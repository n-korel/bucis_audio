package receiver

import (
	"net"
	"sync"
	"time"

	imaadpcm "announcer_simulator/internal/media/g726"
	mediarpt "announcer_simulator/internal/media/rtp"

	pionrtp "github.com/pion/rtp"
)

const (
	readTimeout = 200 * time.Millisecond
	rtpTickNs   = int64(125000)
	maxWrapForwardGap = 10000
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
	MaxRTPTs    uint32
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
	runErr  error

	connMu sync.Mutex
	conn   *net.UDPConn
}

func New(mediaPort int) *Receiver {
	return &Receiver{
		mediaPort: mediaPort,
	}
}

func (r *Receiver) Start() error {
	r.mu.Lock()
	if r.playing {
		r.mu.Unlock()
		return nil
	}
	laddr := &net.UDPAddr{IP: net.IPv4zero, Port: r.mediaPort}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	r.playing = true
	r.runErr = nil
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	r.stats = SessionStats{}
	r.connMu.Lock()
	r.conn = conn
	r.connMu.Unlock()
	stopCh := r.stopCh
	doneCh := r.doneCh
	r.mu.Unlock()

	go r.run(conn, stopCh, doneCh)
	return nil
}

func (r *Receiver) Stop() (SessionStats, error) {
	r.mu.Lock()
	if !r.playing {
		stats := r.stats
		err := r.runErr
		r.runErr = nil
		r.mu.Unlock()
		return stats, err
	}
	stopCh := r.stopCh
	doneCh := r.doneCh
	r.mu.Unlock()

	r.connMu.Lock()
	c := r.conn
	r.connMu.Unlock()

	close(stopCh)

	if c != nil {
		_ = c.SetReadDeadline(time.Now())
	}

	<-doneCh

	r.mu.Lock()
	stats := r.stats
	err := r.runErr
	r.runErr = nil
	r.playing = false
	r.stopCh = nil
	r.doneCh = nil
	r.mu.Unlock()
	return stats, err
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

func (r *Receiver) run(conn *net.UDPConn, stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer func() {
		r.mu.Lock()
		r.playing = false
		r.mu.Unlock()
		close(doneCh)
	}()
	defer func() {
		r.connMu.Lock()
		r.conn = nil
		r.connMu.Unlock()
		_ = conn.Close()
	}()
	_ = conn.SetReadBuffer(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))

	buf := make([]byte, 2048)
	decState := &imaadpcm.IMAADPCMDecoderState{}

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
			r.mu.Lock()
			r.runErr = err
			r.mu.Unlock()
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))

		var pkt pionrtp.Packet
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			continue
		}
		if pkt.PayloadType != mediarpt.PayloadTypeIMAADPCM() {
			continue
		}

		_ = imaadpcm.IMAADPCMDecodeFrame(pkt.Payload, decState)
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
		r.stats.MaxRTPTs = rtpTs
		return
	}

	arrivalDelta := nowRTPUnits - r.stats.LastArrival
	tsDelta := int64(int32(rtpTs - r.stats.LastRTPTs))
	d := arrivalDelta - tsDelta
	if d < 0 {
		d = -d
	}
	r.stats.Jitter += (float64(d) - r.stats.Jitter) / 16.0
	r.stats.LastArrival = nowRTPUnits
	r.stats.LastRTPTs = rtpTs

	if isNewerTimestamp(rtpTs, r.stats.MaxRTPTs) {
		extMax := r.stats.Cycles + uint32(r.stats.MaxSeq)
		u0 := r.stats.Cycles + uint32(seq)
		if u0 > extMax {
			r.stats.MaxSeq = seq
			r.stats.MaxRTPTs = rtpTs
		} else {
			u1 := u0 + (1 << 16)
			if u1 > extMax && (u1-extMax) <= maxWrapForwardGap {
				r.stats.Cycles += 1 << 16
				r.stats.MaxSeq = seq
				r.stats.MaxRTPTs = rtpTs
			}
		}
	}
	r.stats.LastSeq = seq
	r.stats.Received++
}

func isNewerTimestamp(a, b uint32) bool {
	return int32(a-b) > 0
}
