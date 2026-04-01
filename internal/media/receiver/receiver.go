package receiver

import (
	"net"
	"sync"
	"time"

	"announcer_simulator/internal/media/g726"
	mediarpt "announcer_simulator/internal/media/rtp"

	pionrtp "github.com/pion/rtp"
)

const (
	readTimeout = 200 * time.Millisecond
	rtpTickNs = int64(125000)
)

type SessionStats struct {
	FirstSeq    uint16
	LastSeq     uint16
	MaxSeq      uint16
	Cycles      uint32
	Received    uint32
	Jitter      float64
	LastArrival int64
	LastRTPTs   uint32
	LastPacket  time.Time
}

func (s SessionStats) Expected() uint32 {
	if s.Received == 0 {
		return 0
	}
	extMax := s.Cycles + uint32(s.MaxSeq)
	return extMax - uint32(s.FirstSeq) + 1
}

func (s SessionStats) Lost() uint32 {
	expected := s.Expected()
	if expected <= s.Received {
		return 0
	}
	return expected - s.Received
}

func (s SessionStats) JitterMs() float64 {
	return s.Jitter / 8.0
}

type Receiver struct {
	mediaPort int

	mu      sync.Mutex
	playing bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	stats   SessionStats
}

func New(mediaPort int) *Receiver {
	return &Receiver{
		mediaPort: mediaPort,
	}
}

func (r *Receiver) Start() {
	r.mu.Lock()
	if r.playing {
		r.mu.Unlock()
		return
	}
	r.playing = true
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	r.stats = SessionStats{}
	stopCh := r.stopCh
	doneCh := r.doneCh
	r.mu.Unlock()

	go r.run(stopCh, doneCh)
}

func (r *Receiver) Stop() SessionStats {
	r.mu.Lock()
	if !r.playing {
		stats := r.stats
		r.mu.Unlock()
		return stats
	}
	stopCh := r.stopCh
	doneCh := r.doneCh
	r.mu.Unlock()

	close(stopCh)
	<-doneCh

	r.mu.Lock()
	stats := r.stats
	r.playing = false
	r.stopCh = nil
	r.doneCh = nil
	r.mu.Unlock()
	return stats
}

func (r *Receiver) IsPlaying() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.playing
}

func (r *Receiver) LastPacketAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats.LastPacket
}

func (r *Receiver) run(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer close(doneCh)

	laddr := &net.UDPAddr{IP: net.IPv4zero, Port: r.mediaPort}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return
	}
	defer func() {
		_ = conn.Close()
	}()
	_ = conn.SetReadBuffer(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))

	buf := make([]byte, 2048)
	decState := &g726.DecoderState{}

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
				continue
			}
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))

		var pkt pionrtp.Packet
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			continue
		}
		if pkt.PayloadType != mediarpt.PayloadTypeG726 {
			continue
		}

		_ = g726.DecodeFrame(pkt.Payload, decState)
		r.updateStats(pkt.SequenceNumber, pkt.Timestamp)
	}
}

func (r *Receiver) updateStats(seq uint16, rtpTs uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.playing {
		return
	}

	now := time.Now()
	r.stats.LastPacket = now
	nowRTPUnits := now.UnixNano() / rtpTickNs
	if r.stats.Received == 0 {
		r.stats.FirstSeq = seq
		r.stats.LastSeq = seq
		r.stats.MaxSeq = seq
		r.stats.Cycles = 0
		r.stats.Received = 1
		r.stats.LastArrival = nowRTPUnits
		r.stats.LastRTPTs = rtpTs
		return
	}

	transitPrev := float64(r.stats.LastArrival - int64(r.stats.LastRTPTs))
	transitNow := float64(nowRTPUnits - int64(rtpTs))
	d := transitNow - transitPrev
	if d < 0 {
		d = -d
	}
	r.stats.Jitter += (d - r.stats.Jitter) / 16.0
	r.stats.LastArrival = nowRTPUnits
	r.stats.LastRTPTs = rtpTs

	if seq < r.stats.MaxSeq && (r.stats.MaxSeq-seq) > 0x8000 {
		r.stats.Cycles += 1 << 16
		r.stats.MaxSeq = seq
	} else if seq > r.stats.MaxSeq {
		r.stats.MaxSeq = seq
	}
	r.stats.LastSeq = seq
	r.stats.Received++
}
