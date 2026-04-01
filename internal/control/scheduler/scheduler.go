package scheduler

import (
	"time"
)

type Scheduler struct {
}

func (s Scheduler) Schedule(unixMilli int64, fn func()) *time.Timer {
	now := time.Now()
	targetTime := time.UnixMilli(unixMilli)
	delay := targetTime.Sub(now)
	if delay < 0 {
		delay = 0
	}
	return time.AfterFunc(delay, fn)
}

