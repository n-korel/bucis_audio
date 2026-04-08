//go:build linux

package audio

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Controller struct {
	mu sync.Mutex

	stopCh chan struct{}
	doneCh chan struct{}

	stdin io.WriteCloser
	cmd   *exec.Cmd
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
	stdin := c.stdin
	cmd := c.cmd
	c.stdin = nil
	c.cmd = nil
	c.mu.Unlock()

	if stopCh == nil {
		return
	}
	close(stopCh)
	<-doneCh

	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

func (c *Controller) StartAt(t0Ms int64, buf *PCMBuffer) error {
	if buf == nil {
		return errors.New("nil pcm buffer")
	}

	c.Stop()

	device := alsaDevice()
	cmd := exec.Command("aplay",
		"-r", "8000",
		"-c", "1",
		"-f", "S16_LE",
		"-D", device,
		"-q",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	c.mu.Lock()
	c.stopCh = stopCh
	c.doneCh = doneCh
	c.stdin = stdin
	c.cmd = cmd
	c.mu.Unlock()

	go func() {
		defer close(doneCh)
		defer func() {
			_ = stdin.Close()
			_ = cmd.Wait()
		}()

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

		silence := make([]byte, ChunkBytes)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				data := buf.Read(ChunkBytes)
				if len(data) == 0 {
					data = silence
				}
				if _, err := stdin.Write(data); err != nil {
					return
				}
			}
		}
	}()

	return nil
}

func alsaDevice() string {
	if d := os.Getenv("ALSA_DEVICE"); d != "" {
		return d
	}
	return "default"
}

