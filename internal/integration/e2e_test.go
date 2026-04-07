package integration

import (
	"context"
	"net"
	"testing"
	"time"

	"announcer_simulator/internal/control/protocol"
	"announcer_simulator/internal/control/scheduler"
	"announcer_simulator/internal/control/udp"
	"announcer_simulator/internal/media/receiver"
	"announcer_simulator/internal/media/sender"
	"announcer_simulator/internal/session"
)

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatalf("listen udp :0: %v", err)
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	_ = c.Close()
	return port
}

func TestE2ESoundStartStop(t *testing.T) {
	t.Parallel()

	const loopback = "127.0.0.1"

	controlPort := freeUDPPort(t)
	mediaPort := freeUDPPort(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type result struct {
		stats receiver.SessionStats
		err   error
	}
	resCh := make(chan result, 1)

	go func() {
		recv, err := udp.Join(loopback, controlPort)
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer func() { _ = recv.Close() }()

		var (
			s     scheduler.Scheduler
			state session.State
			rx    = receiver.New(mediaPort)
		)

		buf := make([]byte, 2048)

		for {
			select {
			case <-ctx.Done():
				resCh <- result{err: ctx.Err()}
				return
			default:
			}

			n, _, err := recv.Read(buf)
			if err != nil {
				resCh <- result{err: err}
				return
			}

			start, stop, ok := protocol.Parse(buf[:n])
			if !ok {
				continue
			}

			if start != nil {
				_, _ = state.Stop()
				_, _ = rx.Stop()

				state.ScheduleStart(s, start.SessionID, start.T0, func() {
					if !state.MarkPlaybackStartedIfSession(start.SessionID) {
						return
					}
					_ = rx.StartBySoundType(start.Type)
				})
				continue
			}

			if stop != nil {
				sessionID, _ := state.Stop()
				stats, err := rx.Stop()
				if sessionID == "" {
					resCh <- result{err: err}
					return
				}
				resCh <- result{stats: stats, err: err}
				return
			}
		}
	}()

	controlSender, err := udp.NewSender(loopback, controlPort)
	if err != nil {
		t.Fatalf("control sender: %v", err)
	}
	defer func() { _ = controlSender.Close() }()

	mediaSender, err := sender.New(loopback, mediaPort)
	if err != nil {
		t.Fatalf("media sender: %v", err)
	}
	defer func() { _ = mediaSender.Close() }()

	pcm := make([]int16, 8000)
	for i := range pcm {
		pcm[i] = int16((i%200 - 100) * 200)
	}

	t0 := time.Now().Add(300 * time.Millisecond).UnixMilli()
	sessionID := "deadbeef"

	if _, err := controlSender.Send([]byte(protocol.FormatSoundStart(1, t0, sessionID))); err != nil {
		t.Fatalf("send sound_start: %v", err)
	}

	if err := mediaSender.StreamAt(ctx, t0, pcm); err != nil {
		t.Fatalf("stream media: %v", err)
	}

	if _, err := controlSender.Send([]byte("sound_stop")); err != nil {
		t.Fatalf("send sound_stop: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Fatalf("timeout waiting for stats: %v", ctx.Err())
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("brs error: %v", res.err)
		}
		if got := res.stats.Lost(); got != 0 {
			t.Fatalf("expected Lost()==0, got %d (received=%d expected=%d)", got, res.stats.Received, res.stats.Expected())
		}
		if got := res.stats.JitterMs(); got >= 5 {
			t.Fatalf("expected JitterMs()<5, got %.3f", got)
		}
	}
}

