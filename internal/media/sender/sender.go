package sender

import (
	"context"
	"fmt"
	"net"
	"time"

	"announcer_simulator/internal/media/g726"
	mediarpt "announcer_simulator/internal/media/rtp"
)

type Sender struct {
	conn *net.UDPConn
	addr *net.UDPAddr
}

func New(broadcastAddr string, mediaPort int) (*Sender, error) {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", broadcastAddr, mediaPort))
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return nil, err
	}
	return &Sender{conn: conn, addr: addr}, nil
}

func (s *Sender) Close() error {
	return s.conn.Close()
}

func (s *Sender) StreamAt(ctx context.Context, t0 int64, pcm []int16) error {
	if len(pcm) == 0 {
		return nil
	}
	start := time.UnixMilli(t0)
	wait := time.Until(start)
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	encState := &g726.EncoderState{}
	seq := mediarpt.RandomSequence()
	ssrc := mediarpt.RandomSSRC()
	rtpTS := uint32(0)

	ticker := time.NewTicker(mediarpt.FrameDuration)
	defer ticker.Stop()

	for i := 0; i < len(pcm); i += mediarpt.SamplesPerFrame {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := i + mediarpt.SamplesPerFrame
		frame := make([]int16, mediarpt.SamplesPerFrame)
		if end <= len(pcm) {
			copy(frame, pcm[i:end])
		} else {
			copy(frame, pcm[i:])
		}

		payload := g726.EncodeFrame(frame, encState)
		pkt := mediarpt.NewPacket(seq, rtpTS, ssrc, payload)
		raw, err := pkt.Marshal()
		if err != nil {
			return err
		}
		if _, err := s.conn.Write(raw); err != nil {
			return err
		}

		seq++
		rtpTS += mediarpt.SamplesPerFrame

		if end < len(pcm) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
	return nil
}
