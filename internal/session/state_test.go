package session

import (
	"sync/atomic"
	"testing"
	"time"

	"announcer_simulator/internal/control/scheduler"
)

func TestStateStopIfSession(t *testing.T) {
	var st State
	var sch scheduler.Scheduler

	t0 := time.Now().Add(time.Second).UnixMilli()
	st.ScheduleStart(sch, "a", t0, func() {})

	if _, _, ok := st.StopIfSession("b"); ok {
		t.Fatalf("expected StopIfSession for different id to fail")
	}
	prevID, prevT0, ok := st.StopIfSession("a")
	if !ok {
		t.Fatalf("expected StopIfSession for current id to succeed")
	}
	if prevID != "a" || prevT0 != t0 {
		t.Fatalf("got (%q,%d) want (%q,%d)", prevID, prevT0, "a", t0)
	}
	if got := st.CurrentID(); got != "" {
		t.Fatalf("expected empty CurrentID after stop, got %q", got)
	}
	if st.PlaybackActive() {
		t.Fatalf("expected PlaybackActive=false after stop")
	}
}

func TestStateMarkPlaybackStartedIfSession(t *testing.T) {
	var st State
	var sch scheduler.Scheduler

	t0 := time.Now().Add(time.Second).UnixMilli()
	st.ScheduleStart(sch, "a", t0, func() {})

	if st.MarkPlaybackStartedIfSession("b") {
		t.Fatalf("expected MarkPlaybackStartedIfSession for different id to return false")
	}
	if !st.MarkPlaybackStartedIfSession("a") {
		t.Fatalf("expected MarkPlaybackStartedIfSession for current id to return true")
	}
	if !st.PlaybackActive() {
		t.Fatalf("expected PlaybackActive=true after MarkPlaybackStartedIfSession")
	}
}

func TestStateScheduleStartReplacesPreviousTimer(t *testing.T) {
	var st State
	var sch scheduler.Scheduler

	var called1 int32
	done2 := make(chan struct{})

	st.ScheduleStart(sch, "s1", time.Now().Add(250*time.Millisecond).UnixMilli(), func() {
		atomic.AddInt32(&called1, 1)
	})

	st.ScheduleStart(sch, "s2", time.Now().Add(-time.Millisecond).UnixMilli(), func() {
		close(done2)
	})

	select {
	case <-done2:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected second callback to run")
	}

	time.Sleep(300 * time.Millisecond)
	if got := atomic.LoadInt32(&called1); got != 0 {
		t.Fatalf("expected first callback not to run after reschedule, got %d", got)
	}
}

