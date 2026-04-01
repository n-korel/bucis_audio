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

	src := make([]int16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		v := int16(uint16(raw[i]) | uint16(raw[i+1])<<8)
		src = append(src, v)
	}
	if len(src) == 0 {
		return nil, errors.New("empty decoded pcm")
	}

	stereo := make([]int16, 0, len(src)/2)
	for i := 0; i+1 < len(src); i += 2 {
		l := int32(src[i])
		r := int32(src[i+1])
		stereo = append(stereo, int16((l+r)/2))
	}
	if len(stereo) == 0 {
		return nil, errors.New("empty mono pcm")
	}

	return resampleNearest(stereo, decoder.SampleRate(), 8000), nil
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
