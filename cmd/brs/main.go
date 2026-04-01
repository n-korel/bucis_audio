package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
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

	sessionID   string
	scheduledT0 int64
	timer       *time.Timer
}

func (s *sessionState) CurrentID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (s *sessionState) Stop() (prevID string, prevT0 int64) {
	s.mu.Lock()
	prevID = s.sessionID
	prevT0 = s.scheduledT0
	s.sessionID = ""
	s.scheduledT0 = 0
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()
	return prevID, prevT0
}

func (s *sessionState) ScheduleStart(sch scheduler.Scheduler, sessionID string, t0 int64, fn func()) {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.sessionID = sessionID
	s.scheduledT0 = t0
	s.mu.Unlock()

	timer := sch.Schedule(t0, func() {
		s.mu.Lock()
		if s.sessionID != sessionID {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		fn()
	})

	s.mu.Lock()
	if s.sessionID != sessionID {
		s.mu.Unlock()
		timer.Stop()
		return
	}
	s.timer = timer
	s.mu.Unlock()
}

func main() {
	var (
		controlAddr string
		controlPort int
		mediaPort   int
	)

	flag.StringVar(&controlAddr, "control-addr", "", "UDP broadcast адрес (default из CONTROL_ADDR / автоопределение)")
	flag.IntVar(&controlPort, "control-port", 0, "UDP порт (default из CONTROL_PORT)")
	flag.IntVar(&mediaPort, "media-port", 0, "RTP media port (default из MEDIA_PORT)")
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

	var state sessionState
	buf := make([]byte, 2048)

	const rtpIdleTimeout = 1 * time.Second

	go func() {
		<-ctx.Done()
		closeControl()
	}()

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			if !mediaRecv.IsPlaying() {
				continue
			}

			last := mediaRecv.LastPacketAt()
			if last.IsZero() {
				continue
			}
			if time.Since(last) <= rtpIdleTimeout {
				continue
			}

			stats := mediaRecv.Stop()
			sessionID, _ := state.Stop()
			logger.Warn("session timeout (no RTP)", "session_id", sessionID, "idle_ms", time.Since(last).Milliseconds())
			logger.Info("session stats",
				"session_id", sessionID,
				"received", stats.Received,
				"expected", stats.Expected(),
				"lost", stats.Lost(),
				"jitter_ms", math.Round(stats.JitterMs()*100)/100,
			)
		}
	}()

	logSessionStats := func(sessionID string) {
		stats := mediaRecv.Stop()
		logger.Info("session stats",
			"session_id", sessionID,
			"received", stats.Received,
			"expected", stats.Expected(),
			"lost", stats.Lost(),
			"jitter_ms", math.Round(stats.JitterMs()*100)/100,
		)
	}

	for {
		n, _, err := recv.Read(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				state.Stop()
				if mediaRecv.IsPlaying() {
					logger.Info("playback stopping (shutdown)")
					logSessionStats(state.CurrentID())
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
			if mediaRecv.IsPlaying() {
				logSessionStats(prevID)
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
		if mediaRecv.IsPlaying() {
			logger.Warn("session replaced", "old_session_id", oldSessionID, "new_session_id", start.SessionID)
			logSessionStats(oldSessionID)
			logger.Info("playback stopped due to reschedule")
		}

		sessionID := start.SessionID
		logger.Info("sound_start received", "session_id", start.SessionID)
		logger.Debug(
			"scheduled playback start",
			"session_id", sessionID,
		)
		state.ScheduleStart(s, sessionID, t0, func() {
			mediaRecv.Start()
			startDelta := time.Now().UnixMilli() - t0
			logger.Info("playback started", "session_id", sessionID, "start_delta_ms", startDelta)
		})
	}
}
