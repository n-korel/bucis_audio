//go:build !windows && !linux && !darwin

package audio

import (
	"errors"
	"sync"
	"time"
)

type Controller struct {
	mu sync.Mutex

	stopCh chan struct{}
	doneCh chan struct{}
}

func (c *Controller) EnsureContext() error {
	return nil
}

func (c *Controller) Stop() {
	c.mu.Lock()
	stopCh := c.stopCh
	doneCh := c.doneCh
	c.stopCh = nil
	c.doneCh = nil
	c.mu.Unlock()

	if stopCh == nil {
		return
	}
	close(stopCh)
	if doneCh != nil {
		<-doneCh
	}
}

func (c *Controller) StartAt(t0Ms int64, buf *PCMBuffer) error {
	if buf == nil {
		return errors.New("nil pcm buffer")
	}

	c.Stop()

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	c.mu.Lock()
	c.stopCh = stopCh
	c.doneCh = doneCh
	c.mu.Unlock()

	go func() {
		defer close(doneCh)

		t0 := time.UnixMilli(t0Ms)
		if d := time.Until(t0); d > 0 {
			timer := time.NewTimer(d)
			select {
			case <-stopCh:
				timer.Stop()
				return
			case <-timer.C:
			}
		}

		// Keep behavior "not broken" on non-Windows: drain buffer at ~realtime pace.
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				_ = buf.Read(ChunkBytes)
			}
		}
	}()

	return nil
}
