package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"announcer_simulator/internal/control/protocol"
	"announcer_simulator/internal/control/scheduler"
	"announcer_simulator/internal/control/udp"
	"announcer_simulator/internal/infra/config"
	"announcer_simulator/internal/media/receiver"
	"announcer_simulator/pkg/log"
)

type sessionState struct {
	mu sync.Mutex

	sessionID      string
	scheduledT0    int64
	timer          *time.Timer
	playbackActive bool
}

func (s *sessionState) Snapshot() (sessionID string, playbackActive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID, s.playbackActive
}

func (s *sessionState) CurrentID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (s *sessionState) clearSessionLocked() (prevID string, prevT0 int64) {
	prevID = s.sessionID
	prevT0 = s.scheduledT0
	s.sessionID = ""
	s.scheduledT0 = 0
	s.playbackActive = false
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	return prevID, prevT0
}

func (s *sessionState) MarkPlaybackStarted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.playbackActive = true
}

func (s *sessionState) PlaybackActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.playbackActive
}

func (s *sessionState) Stop() (prevID string, prevT0 int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clearSessionLocked()
}

func (s *sessionState) StopIfSession(wantID string) (prevID string, prevT0 int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionID == "" || s.sessionID != wantID {
		return "", 0, false
	}
	prevID, prevT0 = s.clearSessionLocked()
	return prevID, prevT0, true
}

func (s *sessionState) ScheduleStart(sch scheduler.Scheduler, sessionID string, t0 int64, fn func()) {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.sessionID = sessionID
	s.scheduledT0 = t0
	s.timer = sch.Schedule(t0, func() {
		s.mu.Lock()
		if s.sessionID != sessionID {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		fn()
	})
	s.mu.Unlock()
}

func main() {
	var (
		controlAddr string
		controlPort int
		mediaPort   int

		metricsAddr       string
		metricsListenPort int
		metricsSendPort   int
		metricsReplyPort  int
	)

	flag.StringVar(&controlAddr, "control-addr", "", "UDP broadcast адрес (default из CONTROL_ADDR / автоопределение)")
	flag.IntVar(&controlPort, "control-port", 0, "UDP порт (default из CONTROL_PORT)")
	flag.IntVar(&mediaPort, "media-port", 0, "RTP media port (default из MEDIA_PORT)")

	flag.StringVar(&metricsAddr, "metrics-addr", "", "UDP адрес метрик (default из METRICS_ADDR, fallback=control-addr)")
	flag.IntVar(&metricsListenPort, "metrics-listen-port", 0, "UDP порт для приема get_metrics (default из METRICS_LISTEN_PORT)")
	flag.IntVar(&metricsSendPort, "metrics-send-port", 0, "UDP порт назначения для отправки метрик (default из METRICS_SEND_PORT)")
	flag.IntVar(&metricsReplyPort, "metrics-reply-port", 0, "UDP порт ответа на get_metrics (default из METRICS_REPLY_PORT)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.ParseBrs()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "control-addr":
			cfg.ControlAddr = controlAddr
		case "control-port":
			cfg.ControlPort = controlPort
		case "media-port":
			cfg.MediaPort = mediaPort
		case "metrics-addr":
			cfg.MetricsAddr = metricsAddr
		case "metrics-listen-port":
			cfg.MetricsListenPort = metricsListenPort
		case "metrics-send-port":
			cfg.MetricsSendPort = metricsSendPort
		case "metrics-reply-port":
			cfg.MetricsReplyPort = metricsReplyPort
		}
	})

	brsName := os.Getenv("BRS_NAME")
	if brsName == "" {
		brsName = "brs"
	}

	log.Init(os.Getenv("LOG_FORMAT"))
	logger := log.With("node", brsName, "role", "brs")

	recv, err := udp.Join(cfg.ControlAddr, cfg.ControlPort)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var closeControlOnce sync.Once
	closeControl := func() {
		closeControlOnce.Do(func() {
			_ = recv.Close()
		})
	}
	defer closeControl()

	s := scheduler.Scheduler{}
	mediaRecv := receiver.New(cfg.MediaPort)

	var mediaStopMu sync.Mutex
	stopPlayback := func() (stats receiver.SessionStats, err error) {
		mediaStopMu.Lock()
		defer mediaStopMu.Unlock()
		return mediaRecv.Stop()
	}

	var state sessionState
	buf := make([]byte, 2048)

	type lastSessionStats struct {
		sessionID string
		stats     receiver.SessionStats
		at        time.Time
	}
	var lastMu sync.Mutex
	var last lastSessionStats

	metricsSend := func(payload string) {
		if cfg.MetricsAddr == "" || cfg.MetricsSendPort == 0 {
			return
		}
		raddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", cfg.MetricsAddr, cfg.MetricsSendPort))
		if err != nil {
			logger.Warn("metrics resolve failed", "err", err, "addr", cfg.MetricsAddr, "port", cfg.MetricsSendPort)
			return
		}
		conn, err := net.DialUDP("udp4", nil, raddr)
		if err != nil {
			logger.Warn("metrics dial failed", "err", err, "addr", cfg.MetricsAddr, "port", cfg.MetricsSendPort)
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
		n, werr := conn.Write([]byte(payload))
		_ = conn.Close()
		if werr != nil {
			logger.Warn("metrics send failed", "err", werr, "addr", cfg.MetricsAddr, "port", cfg.MetricsSendPort)
			return
		}
		logger.Debug("metrics sent", "addr", cfg.MetricsAddr, "port", cfg.MetricsSendPort, "bytes", n)
	}

	formatRTPMetrics := func(node, sessionID string, stats receiver.SessionStats) string {
		jitter := math.Round(stats.JitterMs()*100) / 100
		return fmt.Sprintf(
			"metrics rtp;%s;%s;%d;%d;%d;%.2f;;;;",
			node,
			sessionID,
			stats.Received,
			stats.Expected(),
			stats.Lost(),
			jitter,
		)
	}

	logSessionStats := func(sessionID string, stats receiver.SessionStats) {
		lastMu.Lock()
		last = lastSessionStats{sessionID: sessionID, stats: stats, at: time.Now()}
		lastMu.Unlock()

		logger.Info("session stats",
			"session_id", sessionID,
			"received", stats.Received,
			"expected", stats.Expected(),
			"lost", stats.Lost(),
			"jitter_ms", math.Round(stats.JitterMs()*100)/100,
		)

		metricsSend(formatRTPMetrics(brsName, sessionID, stats))
	}

	const rtpIdleTimeout = 1 * time.Second

	go func() {
		<-ctx.Done()
		closeControl()
	}()

	go func() {
		if cfg.MetricsListenPort == 0 {
			return
		}
		laddr := &net.UDPAddr{IP: net.IPv4zero, Port: cfg.MetricsListenPort}
		conn, err := net.ListenUDP("udp4", laddr)
		if err != nil {
			logger.Warn("metrics listener failed", "err", err, "port", cfg.MetricsListenPort)
			return
		}
		defer func() { _ = conn.Close() }()

		buf := make([]byte, 2048)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					select {
					case <-ctx.Done():
						return
					default:
						continue
					}
				}
				select {
				case <-ctx.Done():
					return
				default:
				}
				logger.Warn("metrics read failed", "err", err)
				continue
			}

			msg := strings.TrimSpace(string(buf[:n]))
			if msg != "get_metrics" {
				continue
			}
			logger.Debug("get_metrics received", "from_ip", raddr.IP.String(), "from_port", raddr.Port)

			lastMu.Lock()
			snap := last
			lastMu.Unlock()

			payload := "metrics rtp;" + brsName + ";;0;0;0;0.00;;;;"
			if snap.sessionID != "" {
				payload = formatRTPMetrics(brsName, snap.sessionID, snap.stats)
			}

			if cfg.MetricsReplyPort == 0 {
				continue
			}
			dst := &net.UDPAddr{IP: raddr.IP, Port: cfg.MetricsReplyPort}
			wrote, _ := conn.WriteToUDP([]byte(payload), dst)
			logger.Debug("metrics reply sent", "to_ip", dst.IP.String(), "to_port", dst.Port, "bytes", wrote)
		}
	}()

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		type idleCandidate struct {
			sessionID string
			lastPktAt time.Time
		}
		var cand *idleCandidate

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			sessionID, playbackActive := state.Snapshot()
			isPlaying := mediaRecv.IsPlaying()
			lastPktAt := mediaRecv.LastPacketAt()

			if playbackActive && !isPlaying {
				if sessionID == "" {
					cand = nil
					continue
				}
				prevID, _, ok := state.StopIfSession(sessionID)
				if !ok {
					cand = nil
					continue
				}
				stats, recvErr := stopPlayback()
				if recvErr != nil {
					logger.Warn("media receiver stopped with error", "err", recvErr, "session_id", prevID)
				}
				logSessionStats(prevID, stats)
				logger.Warn("session ended (media receiver stopped)", "session_id", prevID)
				cand = nil
				continue
			}

			if !isPlaying {
				cand = nil
				continue
			}

			if !playbackActive || sessionID == "" {
				cand = nil
				continue
			}

			if lastPktAt.IsZero() || time.Since(lastPktAt) <= rtpIdleTimeout {
				cand = nil
				continue
			}

			if cand == nil || cand.sessionID != sessionID {
				cand = &idleCandidate{sessionID: sessionID, lastPktAt: lastPktAt}
				continue
			}

			if lastPktAt.After(cand.lastPktAt) {
				cand = &idleCandidate{sessionID: sessionID, lastPktAt: lastPktAt}
				continue
			}

			prevID, _, ok := state.StopIfSession(sessionID)
			if !ok {
				cand = nil
				continue
			}
			sessionID = prevID
			stats, recvErr := stopPlayback()
			if recvErr != nil {
				logger.Warn("media receiver error on stop", "err", recvErr, "session_id", sessionID)
			}
			logger.Warn("session timeout (no RTP)", "session_id", sessionID, "idle_ms", time.Since(lastPktAt).Milliseconds())
			logSessionStats(sessionID, stats)
			cand = nil
		}
	}()

	for {
		n, _, err := recv.Read(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				prevID, _ := state.Stop()
				stats, recvErr := stopPlayback()
				if prevID != "" {
					logger.Info("playback stopping (shutdown)")
					if recvErr != nil {
						logger.Warn("media receiver error on stop", "err", recvErr, "session_id", prevID)
					}
					logSessionStats(prevID, stats)
				}
				logger.Info("shutdown complete")
				return
			default:
				continue
			}
		}

		start, stop, ok := protocol.Parse(buf[:n])
		if !ok {
			continue
		}

		if stop != nil {
			logFields := []any{}
			currentID := state.CurrentID()
			if currentID != "" {
				logFields = append(logFields, "session_id", currentID)
			}

			if stop.Args != "" {
				logger.Warn("sound_stop arguments ignored", append(logFields, "args", stop.Args)...)
			} else {
				logger.Info("sound_stop received", logFields...)
			}

			prevID, prevT0 := state.Stop()
			stats, recvErr := stopPlayback()
			if prevID != "" {
				if recvErr != nil {
					logger.Warn("media receiver error on stop", "err", recvErr, "session_id", prevID)
				}
				logSessionStats(prevID, stats)
				logger.Info("playback stopped")
			}

			if !mediaRecv.IsPlaying() {
				now := time.Now().UnixMilli()
				if prevT0 != 0 && now < prevT0 {
					logger.Info("CANCELLED (stop before t0)", "session_id", prevID)
				}
			}
			continue
		}

		t0 := start.T0
		now := time.Now().UnixMilli()
		if now > t0 {
			logger.Warn("late start", "behind_ms", now-t0, "session_id", start.SessionID)
		}

		if start.SessionID == state.CurrentID() && start.SessionID != "" {
			logger.Debug("duplicate sound_start ignored", "session_id", start.SessionID)
			continue
		}

		oldSessionID, _ := state.Stop()
		stats, recvErr := stopPlayback()
		if oldSessionID != "" {
			logger.Warn("session replaced", "old_session_id", oldSessionID, "new_session_id", start.SessionID)
			if recvErr != nil {
				logger.Warn("media receiver error on stop", "err", recvErr, "session_id", oldSessionID)
			}
			logSessionStats(oldSessionID, stats)
			logger.Info("playback stopped due to reschedule")
		}

		sessionID := start.SessionID
		logger.Info("sound_start received", "session_id", start.SessionID, "type", start.Type)
		logger.Debug(
			"scheduled playback start",
			"session_id", sessionID,
		)
		state.ScheduleStart(s, sessionID, t0, func() {
			state.mu.Lock()
			if state.sessionID != sessionID {
				state.mu.Unlock()
				return
			}
			mediaStopMu.Lock()
			if state.sessionID != sessionID {
				mediaStopMu.Unlock()
				state.mu.Unlock()
				return
			}
			state.playbackActive = true
			state.mu.Unlock()

			err := mediaRecv.StartBySoundType(start.Type)
			mediaStopMu.Unlock()
			if err != nil {
				_, _, _ = state.StopIfSession(sessionID)
				logger.Error("media receiver start failed", "err", err, "session_id", sessionID)
				return
			}
			startDelta := time.Now().UnixMilli() - t0
			logger.Info("playback started", "session_id", sessionID, "start_delta_ms", startDelta)
		})
	}
}
