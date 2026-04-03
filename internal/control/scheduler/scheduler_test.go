package scheduler

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduleNegativeDelayRunsImmediately(t *testing.T) {
	var sch Scheduler
	var n int32
	done := make(chan struct{})
	past := time.Now().Add(-time.Hour).UnixMilli()
	sch.Schedule(past, func() {
		atomic.AddInt32(&n, 1)
		close(done)
	})
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("callback did not run")
	}
	if atomic.LoadInt32(&n) != 1 {
		t.Fatalf("callback count=%d", n)
	}
}
