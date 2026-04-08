package brs

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"announcer_simulator/internal/control/protocol"
	"announcer_simulator/internal/infra/config"
	"announcer_simulator/internal/media/receiver"
)

type fakeControlReceiver struct {
	ch     chan []byte
	closed atomic.Bool
}

func newFakeControlReceiver() *fakeControlReceiver {
	return &fakeControlReceiver{ch: make(chan []byte, 16)}
}

func (r *fakeControlReceiver) Send(b []byte) {
	r.ch <- b
}

func (r *fakeControlReceiver) Close() error {
	if r.closed.CompareAndSwap(false, true) {
		close(r.ch)
	}
	return nil
}

func (r *fakeControlReceiver) Read(b []byte) (int, *net.UDPAddr, error) {
	msg, ok := <-r.ch
	if !ok {
		return 0, nil, net.ErrClosed
	}
	if r.closed.Load() {
		return 0, nil, net.ErrClosed
	}
	n := copy(b, msg)
	return n, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, nil
}

type fakeMediaReceiver struct {
	mu sync.Mutex

	pcmSink func([]int16)

	playing    bool
	lastPacket time.Time

	// If true, StartBySoundType sets lastPacket far in the past so the idle RTP loop can fire.
	simulateStaleRTP bool

	startCalls int
	stopCalls  int
}

func (m *fakeMediaReceiver) SetPCMSink(fn func(pcm []int16)) {
	m.mu.Lock()
	m.pcmSink = fn
	m.mu.Unlock()
}

func (m *fakeMediaReceiver) StartBySoundType(soundType int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalls++
	m.playing = true
	if m.simulateStaleRTP {
		m.lastPacket = time.Now().Add(-3 * time.Second)
	}
	return nil
}

func (m *fakeMediaReceiver) Stop() (receiver.SessionStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls++
	m.playing = false
	return receiver.SessionStats{}, nil
}

func (m *fakeMediaReceiver) IsPlaying() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.playing
}

func (m *fakeMediaReceiver) LastPacketAt() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastPacket
}

func TestServiceDuplicateSoundStartIgnored(t *testing.T) {
	t.Parallel()

	ctrl := newFakeControlReceiver()
	defer func() { _ = ctrl.Close() }()

	media := &fakeMediaReceiver{}
	media.playing = true
	media.lastPacket = time.Now()

	svc := NewWithDeps(config.Brs{
		ControlAddr:       "127.0.0.1",
		ControlPort:       0,
		MediaPort:         0,
		MetricsListenPort: 0,
		MetricsSendPort:   0,
	}, "node", nil, Deps{
		JoinControl: func(addr string, port int) (controlReceiver, error) { return ctrl, nil },
		NewMedia:    func(mediaPort int) mediaReceiver { return media },
		Now:         time.Now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- svc.Run(ctx) }()

	sessionID := "deadbeef"
	t0 := time.Now().Add(-10 * time.Millisecond).UnixMilli()

	// Use sound type != 1 to avoid real audio output start.
	ctrl.Send([]byte(protocol.FormatSoundStart(2, t0, sessionID)))
	ctrl.Send([]byte(protocol.FormatSoundStart(2, t0, sessionID)))

	time.Sleep(150 * time.Millisecond)

	media.mu.Lock()
	stopAfterDuplicate := media.stopCalls
	media.mu.Unlock()
	// Contract we care about: duplicate sound_start should be ignored and must not stop/restart anything.
	// The first sound_start triggers a "stop previous session" path (even if there was no session), so 1 is expected here.
	if stopAfterDuplicate != 1 {
		t.Fatalf("after duplicate sound_start: expected stopCalls=1, got %d", stopAfterDuplicate)
	}

	ctrl.Send([]byte("sound_stop"))

	time.Sleep(100 * time.Millisecond)
	cancel()
	_ = ctrl.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting service to stop")
	}

	media.mu.Lock()
	stopCalls := media.stopCalls
	startCalls := media.startCalls
	media.mu.Unlock()

	if startCalls != 0 {
		t.Fatalf("expected StartBySoundType not called for soundType=2, got %d", startCalls)
	}
	// Expected calls:
	// - 1x on first sound_start ("stop previous session", even if none)
	// - 1x on sound_stop
	// - 1x on shutdown (ctx.Done)
	if stopCalls != 3 {
		t.Fatalf("expected stopCalls=3, got %d", stopCalls)
	}
}

func TestServiceIdleTimeoutStopsSession(t *testing.T) {
	t.Parallel()

	ctrl := newFakeControlReceiver()
	defer func() { _ = ctrl.Close() }()

	media := &fakeMediaReceiver{}
	media.playing = true
	media.lastPacket = time.Now().Add(-3 * time.Second)

	svc := NewWithDeps(config.Brs{
		ControlAddr:       "127.0.0.1",
		ControlPort:       0,
		MediaPort:         0,
		MetricsListenPort: 0,
		MetricsSendPort:   0,
	}, "node", nil, Deps{
		JoinControl: func(addr string, port int) (controlReceiver, error) { return ctrl, nil },
		NewMedia:    func(mediaPort int) mediaReceiver { return media },
		Now:         time.Now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- svc.Run(ctx) }()

	sessionID := "cafebabe"
	t0 := time.Now().Add(-10 * time.Millisecond).UnixMilli()
	ctrl.Send([]byte(protocol.FormatSoundStart(2, t0, sessionID)))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		media.mu.Lock()
		stopped := media.stopCalls > 0
		media.mu.Unlock()
		if stopped {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	media.mu.Lock()
	stopCalls := media.stopCalls
	media.mu.Unlock()
	if stopCalls == 0 {
		t.Fatalf("expected idle timeout to stop playback")
	}

	cancel()
	_ = ctrl.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting service to stop")
	}
}

func freeUDPPort(t *testing.T, ip net.IP) int {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer func() { _ = c.Close() }()
	return c.LocalAddr().(*net.UDPAddr).Port
}

func TestServiceShutdownOnContextCancel_StopsPlayback(t *testing.T) {
	t.Parallel()

	ctrl := newFakeControlReceiver()
	defer func() { _ = ctrl.Close() }()

	media := &fakeMediaReceiver{}
	svc := NewWithDeps(config.Brs{
		ControlAddr:       "127.0.0.1",
		ControlPort:       0,
		MediaPort:         0,
		MetricsListenPort: 0,
		MetricsSendPort:   0,
	}, "node", nil, Deps{
		JoinControl: func(addr string, port int) (controlReceiver, error) { return ctrl, nil },
		NewMedia:    func(mediaPort int) mediaReceiver { return media },
		Now:         time.Now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- svc.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	_ = ctrl.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for Run to return after cancel (possible deadlock)")
	}

	media.mu.Lock()
	stopCalls := media.stopCalls
	media.mu.Unlock()
	if stopCalls != 1 {
		t.Fatalf("expected exactly one media Stop (shutdown stopPlayback), got %d", stopCalls)
	}
}

func TestServiceIdleTimeoutWhenReceiverPlayingAndStaleLastPacket(t *testing.T) {
	t.Parallel()

	ctrl := newFakeControlReceiver()
	defer func() { _ = ctrl.Close() }()

	media := &fakeMediaReceiver{simulateStaleRTP: true}
	svc := NewWithDeps(config.Brs{
		ControlAddr:       "127.0.0.1",
		ControlPort:       0,
		MediaPort:         0,
		MetricsListenPort: 0,
		MetricsSendPort:   0,
	}, "node", nil, Deps{
		JoinControl: func(addr string, port int) (controlReceiver, error) { return ctrl, nil },
		NewMedia:    func(mediaPort int) mediaReceiver { return media },
		Now:         time.Now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- svc.Run(ctx) }()

	sessionID := "aabbccdd"
	t0 := time.Now().Add(-10 * time.Millisecond).UnixMilli()
	ctrl.Send([]byte(protocol.FormatSoundStart(1, t0, sessionID)))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		media.mu.Lock()
		playing := media.playing
		starts := media.startCalls
		media.mu.Unlock()
		if starts >= 1 && playing {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	media.mu.Lock()
	startCalls := media.startCalls
	playing := media.playing
	stopBaseline := media.stopCalls
	media.mu.Unlock()
	if startCalls < 1 {
		t.Fatalf("expected media StartBySoundType for soundType=1, startCalls=%d", startCalls)
	}
	if stopBaseline < 1 || !playing {
		t.Fatalf("expected playback started with receiver playing, startCalls=%d stopBaseline=%d playing=%v", startCalls, stopBaseline, playing)
	}

	for time.Now().Before(deadline) {
		media.mu.Lock()
		stopCalls := media.stopCalls
		playing := media.playing
		media.mu.Unlock()
		if stopCalls > stopBaseline && !playing {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	media.mu.Lock()
	stopCalls := media.stopCalls
	playing = media.playing
	media.mu.Unlock()
	if stopCalls <= stopBaseline {
		t.Fatalf("expected idle timeout to call media Stop (stopCalls=%d need > baseline %d)", stopCalls, stopBaseline)
	}
	if playing {
		t.Fatalf("expected media not playing after idle stop, got playing=true")
	}

	cancel()
	_ = ctrl.Close()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting service to stop")
	}
}

func TestServiceMetricsGetMetricsUDPReply(t *testing.T) {
	t.Parallel()

	listenPort := freeUDPPort(t, net.IPv4zero)
	replyPort := freeUDPPort(t, net.IPv4(127, 0, 0, 1))
	unusedCfgReplyPort := freeUDPPort(t, net.IPv4(127, 0, 0, 1))

	ctrl := newFakeControlReceiver()
	defer func() { _ = ctrl.Close() }()
	media := &fakeMediaReceiver{}

	svc := NewWithDeps(config.Brs{
		ControlAddr:       "127.0.0.1",
		ControlPort:       0,
		MediaPort:         0,
		MetricsAddr:       "127.0.0.1",
		MetricsListenPort: listenPort,
		MetricsReplyPort:  unusedCfgReplyPort,
		MetricsSendPort:   0,
	}, "metric-node", nil, Deps{
		JoinControl: func(addr string, port int) (controlReceiver, error) { return ctrl, nil },
		NewMedia:    func(mediaPort int) mediaReceiver { return media },
		Now:         time.Now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- svc.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)

	clientConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: replyPort})
	if err != nil {
		cancel()
		_ = ctrl.Close()
		t.Fatalf("ListenUDP client: %v", err)
	}
	defer func() { _ = clientConn.Close() }()

	serverAddr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(listenPort)))
	if err != nil {
		cancel()
		_ = ctrl.Close()
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	_, err = clientConn.WriteToUDP([]byte("get_metrics;"+strconv.Itoa(replyPort)), serverAddr)
	if err != nil {
		cancel()
		_ = ctrl.Close()
		t.Fatalf("WriteToUDP: %v", err)
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, rerr := clientConn.ReadFromUDP(buf)
	cancel()
	_ = ctrl.Close()
	if rerr != nil {
		t.Fatalf("ReadFromUDP: %v", rerr)
	}
	got := strings.TrimSpace(string(buf[:n]))
	wantPrefix := "metrics rtp;metric-node;;0;0;0;0.00"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("unexpected metrics reply: %q (want prefix %q)", got, wantPrefix)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting service to stop")
	}
}

