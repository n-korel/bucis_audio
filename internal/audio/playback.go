//go:build windows

package audio

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/hajimehoshi/oto/v2"
)

type Controller struct {
	mu sync.Mutex

	ctx       *oto.Context
	readyCh   chan struct{}
	ctxErr    error
	ctxInited bool

	stopCh chan struct{}
	doneCh chan struct{}
}

func (c *Controller) EnsureContext() error {
	c.mu.Lock()
	if c.ctxInited {
		err := c.ctxErr
		ready := c.readyCh
		c.mu.Unlock()
		if err != nil {
			return err
		}
		if ready != nil {
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case <-ready:
			case <-timer.C:
				return errors.New("oto context init timeout")
			}
		}
		return nil
	}
	c.ctxInited = true
	c.mu.Unlock()

	type res struct {
		ctx   *oto.Context
		ready chan struct{}
		err   error
	}
	ch := make(chan res, 1)
	go func() {
		ctx, ready, err := oto.NewContext(SampleRate, Channels, oto.FormatSignedInt16LE)
		ch <- res{ctx: ctx, ready: ready, err: err}
	}()

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	var (
		ctx   *oto.Context
		ready chan struct{}
		err   error
	)
	select {
	case r := <-ch:
		ctx, ready, err = r.ctx, r.ready, r.err
	case <-timer.C:
		err = errors.New("oto NewContext timeout")
	}

	c.mu.Lock()
	c.ctx = ctx
	c.readyCh = ready
	c.ctxErr = err
	c.mu.Unlock()

	if err != nil {
		return err
	}
	timer = time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-ready:
	case <-timer.C:
		return errors.New("oto context init timeout")
	}
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
	<-doneCh
}

func (c *Controller) StartAt(t0Ms int64, buf *PCMBuffer) error {
	if buf == nil {
		return errors.New("nil pcm buffer")
	}
	if err := c.EnsureContext(); err != nil {
		return err
	}

	c.Stop()

	pr, pw := io.Pipe()
	player := c.ctx.NewPlayer(pr)
	go func() {
		player.Play()
	}()

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	c.mu.Lock()
	c.stopCh = stopCh
	c.doneCh = doneCh
	c.mu.Unlock()

	go func() {
		defer close(doneCh)
		defer func() {
			_ = pw.Close()
			_ = player.Close()
		}()

		t0 := time.UnixMilli(t0Ms)
		deadline := t0.Add(time.Duration(MaxStartSlip) * time.Millisecond)

		// Start a ticker immediately. Before t0, feed silence to avoid blocking the player.
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		silence := make([]byte, ChunkBytes)
		chunk := make([]byte, ChunkBytes)
		startedAudio := false

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				now := time.Now()

				// Keep the player's reader unblocked before t0.
				if now.Before(t0) {
					if _, err := pw.Write(silence); err != nil {
						return
					}
					continue
				}

				// Optional prefill right after t0, but never wait longer than t0+MaxStartSlip.
				if !startedAudio && now.Before(deadline) && buf.Len() < PrefillBytes {
					if _, err := pw.Write(silence); err != nil {
						return
					}
					continue
				}
				startedAudio = true

				data := buf.Read(ChunkBytes)
				if len(data) == 0 {
					if _, err := pw.Write(silence); err != nil {
						return
					}
					continue
				}
				copy(chunk, silence)
				copy(chunk, data)
				if _, err := pw.Write(chunk); err != nil {
					return
				}
			}
		}
	}()

	return nil
}
