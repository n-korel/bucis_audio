package audio

import (
	"encoding/binary"
	"testing"
)

func TestPCM16ToBytesLE(t *testing.T) {
	in := []int16{0, 1, -1, 0x1234, -0x1234}
	b := PCM16ToBytesLE(in)
	if got, want := len(b), len(in)*2; got != want {
		t.Fatalf("len=%d want %d", got, want)
	}

	for i, v := range in {
		got := int16(binary.LittleEndian.Uint16(b[i*2:]))
		if got != v {
			t.Fatalf("idx=%d got=%d want=%d", i, got, v)
		}
	}
}

func TestPCM16ToBytesLEEmptyNil(t *testing.T) {
	if got := PCM16ToBytesLE(nil); got != nil {
		t.Fatalf("nil input: got %v want nil", got)
	}
	if got := PCM16ToBytesLE([]int16{}); got != nil {
		t.Fatalf("empty input: got %v want nil", got)
	}
}

