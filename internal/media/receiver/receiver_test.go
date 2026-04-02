package receiver

import "testing"

func TestLatePacketDoesNotIncreaseCycles(t *testing.T) {
	r := &Receiver{playing: true}

	r.updateStats(40000, 1000)

	r.updateStats(50000, 2000)

	r.updateStats(1000, 1500)

	if r.stats.Cycles != 0 {
		t.Fatalf("expected Cycles=0, got %d", r.stats.Cycles)
	}
	if r.stats.MaxSeq != 50000 {
		t.Fatalf("expected MaxSeq=50000, got %d", r.stats.MaxSeq)
	}
	if got, want := r.stats.Expected(), uint32(10001); got != want {
		t.Fatalf("expected Expected=%d, got %d", want, got)
	}
	if got, want := r.stats.Lost(), uint32(9998); got != want {
		t.Fatalf("expected Lost=%d, got %d", want, got)
	}
}

func TestLatePacketWithNewerTimestampAfterSourceResetDoesNotIncreaseCycles(t *testing.T) {
	r := &Receiver{playing: true}

	r.updateStats(40000, 1000)
	r.updateStats(50000, 2000)
	r.updateStats(1000, 3000)

	if r.stats.Cycles != 0 {
		t.Fatalf("expected Cycles=0, got %d", r.stats.Cycles)
	}
	if r.stats.MaxSeq != 50000 {
		t.Fatalf("expected MaxSeq=50000, got %d", r.stats.MaxSeq)
	}
	if got, want := r.stats.Expected(), uint32(10001); got != want {
		t.Fatalf("expected Expected=%d, got %d", want, got)
	}
}

func TestWrapSequenceIncreasesCycles(t *testing.T) {
	r := &Receiver{playing: true}

	r.updateStats(60000, 1000)

	r.updateStats(65000, 1500)

	r.updateStats(100, 2000)
	
	r.updateStats(60500, 1600)

	if got, want := r.stats.Cycles, uint32(1<<16); got != want {
		t.Fatalf("expected Cycles=%d, got %d", want, got)
	}
	if got, want := r.stats.MaxSeq, uint16(100); got != want {
		t.Fatalf("expected MaxSeq=%d, got %d", want, got)
	}
	if got, want := r.stats.Expected(), uint32(5637); got != want {
		t.Fatalf("expected Expected=%d, got %d", want, got)
	}
}

