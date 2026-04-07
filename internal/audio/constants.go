package audio

import "encoding/binary"

const (
	SampleRate   = 8000
	Channels     = 1
	ChunkBytes   = 320 // 20ms @ 8kHz mono S16LE
	PrefillBytes = 320 // 20ms prefill target
	MaxStartSlip = 50  // ms
)

func PCM16ToBytesLE(pcm []int16) []byte {
	if len(pcm) == 0 {
		return nil
	}
	out := make([]byte, len(pcm)*2)
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out
}
