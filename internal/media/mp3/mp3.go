package mp3

import (
	"errors"
	"io"
	"os"

	mp3decoder "github.com/hajimehoshi/go-mp3"
)

func LoadPCM8kMono(path string) ([]int16, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	decoder, err := mp3decoder.NewDecoder(f)
	if err != nil {
		return nil, err
	}

	raw, err := io.ReadAll(decoder)
	if err != nil {
		return nil, err
	}
	if len(raw)%4 != 0 {
		return nil, errors.New("invalid mp3 decoded pcm length")
	}

	mono := make([]int16, 0, len(raw)/4)
	for i := 0; i+3 < len(raw); i += 4 {
		l := int16(uint16(raw[i]) | uint16(raw[i+1])<<8)
		r := int16(uint16(raw[i+2]) | uint16(raw[i+3])<<8)
		mono = append(mono, int16((int32(l)+int32(r))/2))
	}
	if len(mono) == 0 {
		return nil, errors.New("empty mono pcm")
	}

	return resampleNearest(mono, decoder.SampleRate(), 8000), nil
}

func resampleNearest(in []int16, inRate int, outRate int) []int16 {
	if inRate <= 0 || outRate <= 0 || len(in) == 0 {
		return nil
	}
	if inRate == outRate {
		out := make([]int16, len(in))
		copy(out, in)
		return out
	}

	outLen := int(int64(len(in)) * int64(outRate) / int64(inRate))
	if outLen <= 0 {
		outLen = 1
	}

	out := make([]int16, outLen)
	for i := 0; i < outLen; i++ {
		srcIdx := int(int64(i) * int64(inRate) / int64(outRate))
		if srcIdx >= len(in) {
			srcIdx = len(in) - 1
		}
		out[i] = in[srcIdx]
	}
	return out
}
