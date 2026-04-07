package audio

import (
	"bytes"
	"testing"
)

func TestPCMBufferWriteReadFIFO(t *testing.T) {
	b := NewPCMBuffer(16)
	b.Write([]byte("abcd"))
	b.Write([]byte("ef"))

	if got, want := b.Len(), 6; got != want {
		t.Fatalf("Len()=%d want %d", got, want)
	}

	out := b.Read(4)
	if !bytes.Equal(out, []byte("abcd")) {
		t.Fatalf("Read(4)=%q want %q", out, "abcd")
	}
	if got, want := b.Len(), 2; got != want {
		t.Fatalf("Len()=%d want %d", got, want)
	}

	out = b.Read(10)
	if !bytes.Equal(out, []byte("ef")) {
		t.Fatalf("Read(10)=%q want %q", out, "ef")
	}
	if got, want := b.Len(), 0; got != want {
		t.Fatalf("Len()=%d want %d", got, want)
	}
}

func TestPCMBufferOverflowDropsOldest(t *testing.T) {
	b := NewPCMBuffer(8)
	b.Write([]byte("abcdef"))
	b.Write([]byte("ghij")) // overflow by 2 -> should keep "cdefghij"

	if got, want := b.Len(), 8; got != want {
		t.Fatalf("Len()=%d want %d", got, want)
	}
	out := b.Read(8)
	if !bytes.Equal(out, []byte("cdefghij")) {
		t.Fatalf("Read(8)=%q want %q", out, "cdefghij")
	}
}

func TestPCMBufferWriteLargerThanCapacityKeepsTail(t *testing.T) {
	b := NewPCMBuffer(5)
	b.Write([]byte("0123456789"))
	if got, want := b.Len(), 5; got != want {
		t.Fatalf("Len()=%d want %d", got, want)
	}
	out := b.Read(10)
	if !bytes.Equal(out, []byte("56789")) {
		t.Fatalf("Read()=%q want %q", out, "56789")
	}
}

func TestPCMBufferWrapAroundPreservesOrder(t *testing.T) {
	b := NewPCMBuffer(8)
	b.Write([]byte("abcdef"))
	_ = b.Read(5) // leaves "f"

	b.Write([]byte("ghijkl")) // n=7, should be "fghijkl"
	out := b.Read(7)
	if !bytes.Equal(out, []byte("fghijkl")) {
		t.Fatalf("Read()=%q want %q", out, "fghijkl")
	}
}

func TestPCMBufferResetClears(t *testing.T) {
	b := NewPCMBuffer(8)
	b.Write([]byte("abcd"))
	b.Reset()
	if got, want := b.Len(), 0; got != want {
		t.Fatalf("Len()=%d want %d", got, want)
	}
	if got := b.Read(1); got != nil {
		t.Fatalf("Read()=%v want nil", got)
	}
}

func TestPCMBufferZeroCapacityBehavesEmpty(t *testing.T) {
	b := NewPCMBuffer(0)
	b.Write([]byte("abcd"))
	if got, want := b.Len(), 0; got != want {
		t.Fatalf("Len()=%d want %d", got, want)
	}
	if got := b.Read(10); got != nil {
		t.Fatalf("Read()=%v want nil", got)
	}
}

