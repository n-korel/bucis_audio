//go:build !windows

package audio

import (
	"testing"
	"time"
)

func TestControllerStartAtNilBufferFails(t *testing.T) {
	var c Controller
	if err := c.StartAt(time.Now().UnixMilli(), nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestControllerStartStopDoesNotHang(t *testing.T) {
	var c Controller
	b := NewPCMBuffer(ChunkBytes * 4)

	t0 := time.Now().Add(20 * time.Millisecond).UnixMilli()
	if err := c.StartAt(t0, b); err != nil {
		t.Fatalf("StartAt: %v", err)
	}

	// Put some data to allow draining after start.
	b.Write(make([]byte, ChunkBytes*2))

	time.Sleep(80 * time.Millisecond)
	c.Stop()

	// Calling Stop again should be a no-op.
	c.Stop()
}

func TestControllerRestartReplacesPrevious(t *testing.T) {
	var c Controller
	b := NewPCMBuffer(ChunkBytes * 4)

	t0 := time.Now().Add(10 * time.Millisecond).UnixMilli()
	if err := c.StartAt(t0, b); err != nil {
		t.Fatalf("StartAt #1: %v", err)
	}
	if err := c.StartAt(t0, b); err != nil {
		t.Fatalf("StartAt #2: %v", err)
	}
	c.Stop()
}

