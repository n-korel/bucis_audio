package rtp

import (
	crand "crypto/rand"
	"math/rand"
	"sync"
	"time"

	pionrtp "github.com/pion/rtp"
)

const (
	PayloadTypeG726 = 2
	SampleRate      = 8000
	SamplesPerFrame = 160
	FrameDuration   = 20 * time.Millisecond
)

func NewPacket(seq uint16, ts uint32, ssrc uint32, payload []byte) *pionrtp.Packet {
	return &pionrtp.Packet{
		Header: pionrtp.Header{
			Version:        2,
			PayloadType:    PayloadTypeG726,
			SequenceNumber: seq,
			Timestamp:      ts,
			SSRC:           ssrc,
		},
		Payload: payload,
	}
}

var (
	fallbackOnce sync.Once
	fallbackRand *rand.Rand
)

func fallbackUint32() uint32 {
	fallbackOnce.Do(func() {
		fallbackRand = rand.New(rand.NewSource(time.Now().UnixNano()))
	})
	return fallbackRand.Uint32()
}

func RandomSSRC() uint32 {
	var b [4]byte
	if _, err := crand.Read(b[:]); err == nil {
		return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	}
	return fallbackUint32()
}

func RandomSequence() uint16 {
	return uint16(RandomSSRC())
}
