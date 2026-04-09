package brs

import (
	"context"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"announcer_simulator/internal/audio"
	"announcer_simulator/internal/control/protocol"
	"announcer_simulator/internal/control/scheduler"
	"announcer_simulator/internal/control/udp"
	"announcer_simulator/internal/infra/config"
	"announcer_simulator/internal/media/receiver"
	"announcer_simulator/internal/session"

	"log/slog"
)

type Service interface {
	Run(ctx context.Context) error
}

type controlReceiver interface {
	Read(b []byte) (int, *net.UDPAddr, error)
	Close() error
}

type mediaReceiver interface {
	SetPCMSink(fn func(pcm []int16))
	StartBySoundType(soundType int) error
	Stop() (receiver.SessionStats, error)
	IsPlaying() bool
	LastPacketAt() time.Time
}

type audioController interface {
	EnsureContext() error
	StartAt(t0Ms int64, buf *audio.PCMBuffer) error
	Stop()
}

type Deps struct {
	JoinControl func(addr string, port int) (controlReceiver, error)
	NewMedia    func(mediaPort int) mediaReceiver
	NewPCMBuf   func(size int) *audio.PCMBuffer
	Now         func() time.Time
}

type brsService struct {
	cfg    config.Brs
	name   string
	logger *slog.Logger

	deps Deps
}

func New(cfg config.Brs, name string, logger *slog.Logger) Service {
	return NewWithDeps(cfg, name, logger, Deps{})
}

func NewWithDeps(cfg config.Brs, name string, logger *slog.Logger, deps Deps) Service {
	if deps.JoinControl == nil {
		deps.JoinControl = func(addr string, port int) (controlReceiver, error) { return udp.Join(addr, port) }
	}
	if deps.NewMedia == nil {
		deps.NewMedia = func(mediaPort int) mediaReceiver { return receiver.New(mediaPort) }
	}
	if deps.NewPCMBuf == nil {
		deps.NewPCMBuf = audio.NewPCMBuffer
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &brsService{cfg: cfg, name: name, logger: logger, deps: deps}
}

func (s *brsService) Run(ctx context.Context) error {
	if s.logger == nil {
		s.logger = slog.Default()
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	recv, err := s.deps.JoinControl(s.cfg.ControlAddr, s.cfg.ControlPort)
	if err != nil {
		return err
	}

	var closeControlOnce sync.Once
	closeControl := func() {
		closeControlOnce.Do(func() {
			_ = recv.Close()
		})
	}
	defer closeControl()

	go func() {
		<-ctx.Done()
		closeControl()
	}()

	sch := scheduler.Scheduler{}
	mediaRecv := s.deps.NewMedia(s.cfg.MediaPort)
	pcmBuf := s.deps.NewPCMBuf(4800) // ~300ms @ 8kHz mono S16LE
	var audioCtl audio.Controller
	var audioOut audioController = &audioCtl

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := audioOut.EnsureContext(); err != nil {
			s.logger.Warn("audio output init failed", "err", err)
		}
	}()

	mediaRecv.SetPCMSink(func(pcm []int16) {
		pcmBuf.Write(audio.PCM16ToBytesLE(pcm))
	})

	var mediaStopMu sync.Mutex
	stopPlaybackLocked := func() (stats receiver.SessionStats, err error) {
		audioOut.Stop()
		pcmBuf.Reset()
		return mediaRecv.Stop()
	}

	var state session.State
	buf := make([]byte, 2048)

	type lastSessionStats struct {
		sessionID string
		stats     receiver.SessionStats
		at        time.Time
	}
	var lastMu sync.Mutex
	var last lastSessionStats

	var metricsMu sync.Mutex
	var metricsConn *net.UDPConn
	var metricsResolveOnce sync.Once
	var metricsRaddr *net.UDPAddr
	var metricsResolveErr error
	resolveMetricsAddr := func() (*net.UDPAddr, error) {
		metricsResolveOnce.Do(func() {
			if s.cfg.MetricsAddr == "" || s.cfg.MetricsSendPort == 0 {
				metricsResolveErr = fmt.Errorf("metrics disabled")
				return
			}
			metricsRaddr, metricsResolveErr = net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", s.cfg.MetricsAddr, s.cfg.MetricsSendPort))
		})
		return metricsRaddr, metricsResolveErr
	}
	closeMetricsConn := func() {
		metricsMu.Lock()
		defer metricsMu.Unlock()
		if metricsConn == nil {
			return
		}
		_ = metricsConn.Close()
		metricsConn = nil
	}
	defer closeMetricsConn()

	metricsSend := func(payload string) {
		raddr, err := resolveMetricsAddr()
		if err != nil {
			return
		}

		getConn := func() (*net.UDPConn, error) {
			metricsMu.Lock()
			defer metricsMu.Unlock()
			if metricsConn != nil {
				return metricsConn, nil
			}
			conn, derr := net.DialUDP("udp4", nil, raddr)
			if derr != nil {
				return nil, derr
			}
			metricsConn = conn
			return metricsConn, nil
		}

		conn, err := getConn()
		if err != nil {
			s.logger.Warn("metrics dial failed", "err", err, "addr", s.cfg.MetricsAddr, "port", s.cfg.MetricsSendPort)
			return
		}

		_ = conn.SetWriteDeadline(s.deps.Now().Add(200 * time.Millisecond))
		n, werr := conn.Write([]byte(payload))
		if werr == nil {
			s.logger.Debug("metrics sent", "addr", s.cfg.MetricsAddr, "port", s.cfg.MetricsSendPort, "bytes", n)
			return
		}

		s.logger.Warn("metrics send failed", "err", werr, "addr", s.cfg.MetricsAddr, "port", s.cfg.MetricsSendPort)
		closeMetricsConn()

		// One reconnect retry on transient socket errors.
		conn, err = getConn()
		if err != nil {
			s.logger.Warn("metrics dial failed", "err", err, "addr", s.cfg.MetricsAddr, "port", s.cfg.MetricsSendPort)
			return
		}
		_ = conn.SetWriteDeadline(s.deps.Now().Add(200 * time.Millisecond))
		n, werr = conn.Write([]byte(payload))
		if werr != nil {
			s.logger.Warn("metrics send failed", "err", werr, "addr", s.cfg.MetricsAddr, "port", s.cfg.MetricsSendPort)
			closeMetricsConn()
			return
		}
		s.logger.Debug("metrics sent", "addr", s.cfg.MetricsAddr, "port", s.cfg.MetricsSendPort, "bytes", n)
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
		last = lastSessionStats{sessionID: sessionID, stats: stats, at: s.deps.Now()}
		lastMu.Unlock()

		s.logger.Info("session stats",
			"session_id", sessionID,
			"received", stats.Received,
			"expected", stats.Expected(),
			"lost", stats.Lost(),
			"jitter_ms", math.Round(stats.JitterMs()*100)/100,
		)

		metricsSend(formatRTPMetrics(s.name, sessionID, stats))
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if s.cfg.MetricsListenPort == 0 {
			return
		}
		laddr := &net.UDPAddr{IP: net.IPv4zero, Port: s.cfg.MetricsListenPort}
		conn, err := net.ListenUDP("udp4", laddr)
		if err != nil {
			s.logger.Warn("metrics listener failed", "err", err, "port", s.cfg.MetricsListenPort)
			return
		}
		defer func() { _ = conn.Close() }()

		buf := make([]byte, 2048)
		for {
			_ = conn.SetReadDeadline(s.deps.Now().Add(500 * time.Millisecond))
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
				s.logger.Warn("metrics read failed", "err", err)
				continue
			}

			msg := strings.TrimSpace(string(buf[:n]))
			if msg == "" {
				continue
			}

			// Expected request: "get_metrics" optionally with reply port, e.g. "get_metrics;8881" or "get_metrics 8881".
			fields := strings.FieldsFunc(msg, func(r rune) bool {
				return r == ';' || r == ' ' || r == '\t'
			})
			if len(fields) == 0 || fields[0] != "get_metrics" {
				continue
			}
			s.logger.Debug("get_metrics received", "from_ip", raddr.IP.String(), "from_port", raddr.Port)

			lastMu.Lock()
			snap := last
			lastMu.Unlock()

			payload := "metrics rtp;" + s.name + ";;0;0;0;0.00;;;;"
			if snap.sessionID != "" {
				payload = formatRTPMetrics(s.name, snap.sessionID, snap.stats)
			}

			replyPort := 0
			if len(fields) >= 2 && fields[1] != "" {
				if p, perr := strconv.Atoi(fields[1]); perr == nil && p > 0 && p <= 65535 {
					replyPort = p
				}
			}
			if replyPort == 0 && raddr.Port > 0 {
				replyPort = raddr.Port
			}
			if replyPort == 0 && s.cfg.MetricsReplyPort > 0 {
				replyPort = s.cfg.MetricsReplyPort
			}
			if replyPort == 0 {
				continue
			}

			dst := &net.UDPAddr{IP: raddr.IP, Port: replyPort}
			wrote, _ := conn.WriteToUDP([]byte(payload), dst)
			s.logger.Debug("metrics reply sent", "to_ip", dst.IP.String(), "to_port", dst.Port, "bytes", wrote)
		}
	}()

	const rtpIdleTimeout = 1 * time.Second
	wg.Add(1)
	go func() {
		defer wg.Done()
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

			if ctx.Err() != nil {
				return
			}

			mediaStopMu.Lock()
			sessionID, playbackActive := state.Snapshot()
			isPlaying := mediaRecv.IsPlaying()
			lastPktAt := mediaRecv.LastPacketAt()

			if playbackActive && !isPlaying {
				if ctx.Err() != nil {
					mediaStopMu.Unlock()
					return
				}
				if sessionID == "" {
					mediaStopMu.Unlock()
					cand = nil
					continue
				}
				if ctx.Err() != nil {
					mediaStopMu.Unlock()
					return
				}

				prevID, _, ok := state.StopIfSession(sessionID)
				if !ok {
					mediaStopMu.Unlock()
					cand = nil
					continue
				}
				stats, recvErr := stopPlaybackLocked()
				mediaStopMu.Unlock()
				if recvErr != nil {
					s.logger.Warn("media receiver stopped with error", "err", recvErr, "session_id", prevID)
				}
				logSessionStats(prevID, stats)
				s.logger.Warn("session ended (media receiver stopped)", "session_id", prevID)
				cand = nil
				continue
			}

			if !isPlaying {
				mediaStopMu.Unlock()
				cand = nil
				continue
			}

			if !playbackActive || sessionID == "" {
				mediaStopMu.Unlock()
				cand = nil
				continue
			}

			if lastPktAt.IsZero() || s.deps.Now().Sub(lastPktAt) <= rtpIdleTimeout {
				mediaStopMu.Unlock()
				cand = nil
				continue
			}

			if cand == nil || cand.sessionID != sessionID {
				mediaStopMu.Unlock()
				cand = &idleCandidate{sessionID: sessionID, lastPktAt: lastPktAt}
				continue
			}

			if lastPktAt.After(cand.lastPktAt) {
				mediaStopMu.Unlock()
				cand = &idleCandidate{sessionID: sessionID, lastPktAt: lastPktAt}
				continue
			}

			if ctx.Err() != nil {
				mediaStopMu.Unlock()
				return
			}

			prevID, _, ok := state.StopIfSession(sessionID)
			if !ok {
				mediaStopMu.Unlock()
				cand = nil
				continue
			}
			sessionID = prevID
			stats, recvErr := stopPlaybackLocked()
			mediaStopMu.Unlock()
			if recvErr != nil {
				s.logger.Warn("media receiver error on stop", "err", recvErr, "session_id", sessionID)
			}
			s.logger.Warn("session timeout (no RTP)", "session_id", sessionID, "idle_ms", s.deps.Now().Sub(lastPktAt).Milliseconds())
			logSessionStats(sessionID, stats)
			cand = nil
		}
	}()

	for {
		n, _, err := recv.Read(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				mediaStopMu.Lock()
				prevID, _ := state.Stop()
				stats, recvErr := stopPlaybackLocked()
				mediaStopMu.Unlock()
				if prevID != "" {
					s.logger.Info("playback stopping (shutdown)")
					if recvErr != nil {
						s.logger.Warn("media receiver error on stop", "err", recvErr, "session_id", prevID)
					}
					logSessionStats(prevID, stats)
				}
				s.logger.Info("shutdown complete")
				return nil
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
				s.logger.Warn("sound_stop arguments ignored", append(logFields, "args", stop.Args)...)
			} else {
				s.logger.Info("sound_stop received", logFields...)
			}

			mediaStopMu.Lock()
			prevID, prevT0 := state.Stop()
			stats, recvErr := stopPlaybackLocked()
			mediaStopMu.Unlock()
			if prevID != "" {
				if recvErr != nil {
					s.logger.Warn("media receiver error on stop", "err", recvErr, "session_id", prevID)
				}
				logSessionStats(prevID, stats)
				s.logger.Info("playback stopped")
			}

			if !mediaRecv.IsPlaying() {
				now := s.deps.Now().UnixMilli()
				if prevT0 != 0 && now < prevT0 {
					s.logger.Info("CANCELLED (stop before t0)", "session_id", prevID)
				}
			}
			continue
		}

		sessionID := start.SessionID
		if sessionID == "" {
			sessionID = fmt.Sprintf("%08x", s.deps.Now().UnixMilli()&0xffffffff)
			s.logger.Warn("sound_start without session_id, generated synthetic", "session_id", sessionID)
		}

		t0 := start.T0
		now := s.deps.Now().UnixMilli()
		if now > t0 {
			s.logger.Warn("late start", "behind_ms", now-t0, "session_id", sessionID)
		}

		if sessionID == state.CurrentID() {
			s.logger.Debug("duplicate sound_start ignored", "session_id", sessionID)
			continue
		}

		mediaStopMu.Lock()
		oldSessionID, _ := state.Stop()
		stats, recvErr := stopPlaybackLocked()
		mediaStopMu.Unlock()
		if oldSessionID != "" {
			s.logger.Warn("session replaced", "old_session_id", oldSessionID, "new_session_id", sessionID)
			if recvErr != nil {
				s.logger.Warn("media receiver error on stop", "err", recvErr, "session_id", oldSessionID)
			}
			logSessionStats(oldSessionID, stats)
			s.logger.Info("playback stopped due to reschedule")
		}

		soundType := start.Type
		s.logger.Info("sound_start received", "session_id", sessionID, "type", start.Type)

		pcmBuf.Reset()

		if soundType == 1 {
			mediaStopMu.Lock()
			if err := mediaRecv.StartBySoundType(soundType); err != nil {
				mediaStopMu.Unlock()
				s.logger.Error("media receiver start failed", "err", err, "session_id", sessionID)
				continue
			}
			if err := audioOut.EnsureContext(); err != nil {
				stats, recvErr := stopPlaybackLocked()
				mediaStopMu.Unlock()
				s.logger.Error("audio output init failed", "err", err, "session_id", sessionID)
				if recvErr != nil {
					s.logger.Warn("media receiver error on stop", "err", recvErr, "session_id", sessionID)
				}
				logSessionStats(sessionID, stats)
				continue
			}
			mediaStopMu.Unlock()
		}

		s.logger.Debug("scheduled playback start", "session_id", sessionID)
		state.ScheduleStart(sch, sessionID, t0, func() {
			if !state.MarkPlaybackStartedIfSession(sessionID) {
				return
			}

			mediaStopMu.Lock()
			defer mediaStopMu.Unlock()

			if soundType == 1 {
				if s.deps.Now().UnixMilli() > t0 {
					pcmBuf.Reset()
				}
				if err := audioOut.StartAt(t0, pcmBuf); err != nil {
					prevID, _, ok := state.StopIfSession(sessionID)
					if ok {
						sessionID = prevID
					}
					stats, recvErr := stopPlaybackLocked()
					s.logger.Error("audio output start failed", "err", err, "session_id", sessionID)
					if recvErr != nil {
						s.logger.Warn("media receiver error on stop", "err", recvErr, "session_id", sessionID)
					}
					logSessionStats(sessionID, stats)
					return
				}
			}
			startDelta := s.deps.Now().UnixMilli() - t0
			s.logger.Info("playback started", "session_id", sessionID, "start_delta_ms", startDelta)
		})
	}
}

