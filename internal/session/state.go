package session

import (
	"sync"
	"time"

	"announcer_simulator/internal/control/scheduler"
)

type State struct {
	mu sync.Mutex

	sessionID      string
	scheduledT0    int64
	timer          *time.Timer
	playbackActive bool
}

func (s *State) Snapshot() (sessionID string, playbackActive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID, s.playbackActive
}

func (s *State) CurrentID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (s *State) IsSession(wantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID != "" && s.sessionID == wantID
}

func (s *State) clearSessionLocked() (prevID string, prevT0 int64) {
	prevID = s.sessionID
	prevT0 = s.scheduledT0
	s.sessionID = ""
	s.scheduledT0 = 0
	s.playbackActive = false
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	return prevID, prevT0
}

func (s *State) MarkPlaybackStartedIfSession(wantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionID == "" || s.sessionID != wantID {
		return false
	}
	s.playbackActive = true
	return true
}

func (s *State) PlaybackActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.playbackActive
}

func (s *State) Stop() (prevID string, prevT0 int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clearSessionLocked()
}

func (s *State) StopIfSession(wantID string) (prevID string, prevT0 int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionID == "" || s.sessionID != wantID {
		return "", 0, false
	}
	prevID, prevT0 = s.clearSessionLocked()
	return prevID, prevT0, true
}

func (s *State) ScheduleStart(sch scheduler.Scheduler, sessionID string, t0 int64, fn func()) {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.sessionID = sessionID
	s.scheduledT0 = t0
	s.timer = sch.Schedule(t0, func() {
		if !s.IsSession(sessionID) {
			return
		}
		fn()
	})
	s.mu.Unlock()
}
