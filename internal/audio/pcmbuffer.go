package audio

import "sync"

// PCMBuffer is a fixed-size FIFO ring buffer storing PCM bytes.
// When full, it drops the oldest data (freshness > continuity).
type PCMBuffer struct {
	mu  sync.Mutex
	buf []byte
	r   int
	n   int
}

func NewPCMBuffer(capacityBytes int) *PCMBuffer {
	if capacityBytes < 0 {
		capacityBytes = 0
	}
	return &PCMBuffer{buf: make([]byte, capacityBytes)}
}

func (b *PCMBuffer) Reset() {
	b.mu.Lock()
	b.r = 0
	b.n = 0
	b.mu.Unlock()
}

func (b *PCMBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.n
}

func (b *PCMBuffer) Write(p []byte) {
	if len(p) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) == 0 {
		return
	}

	if len(p) >= len(b.buf) {
		// Keep only the tail that fits.
		p = p[len(p)-len(b.buf):]
		b.r = 0
		copy(b.buf, p)
		b.n = len(b.buf)
		return
	}

	// If overflow, drop oldest bytes first.
	overflow := (b.n + len(p)) - len(b.buf)
	if overflow > 0 {
		b.r = (b.r + overflow) % len(b.buf)
		b.n -= overflow
	}

	w := (b.r + b.n) % len(b.buf)
	first := min(len(p), len(b.buf)-w)
	copy(b.buf[w:w+first], p[:first])
	if first < len(p) {
		copy(b.buf[0:len(p)-first], p[first:])
	}
	b.n += len(p)
}

func (b *PCMBuffer) Read(n int) []byte {
	if n <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.n == 0 || len(b.buf) == 0 {
		return nil
	}
	if n > b.n {
		n = b.n
	}

	out := make([]byte, n)
	first := min(n, len(b.buf)-b.r)
	copy(out, b.buf[b.r:b.r+first])
	if first < n {
		copy(out[first:], b.buf[0:n-first])
	}

	b.r = (b.r + n) % len(b.buf)
	b.n -= n
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
