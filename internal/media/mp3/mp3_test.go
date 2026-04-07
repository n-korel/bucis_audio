package mp3

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestResampleNearest_SameRate(t *testing.T) {
	in := []int16{10, 20, 30}
	out := resampleNearest(in, 8000, 8000)
	if len(out) != 3 || out[0] != 10 || out[1] != 20 || out[2] != 30 {
		t.Fatalf("got %v want %v", out, in)
	}
}

func TestResampleNearest_DownSampleLength(t *testing.T) {
	in := make([]int16, 16000)
	for i := range in {
		in[i] = int16(i)
	}
	out := resampleNearest(in, 16000, 8000)
	if len(out) != 8000 {
		t.Fatalf("len(out)=%d want 8000", len(out))
	}
	if out[0] != in[0] {
		t.Fatalf("first sample: got %d want %d", out[0], in[0])
	}
	lastIdx := int(int64(len(out)-1) * int64(16000) / int64(8000))
	if out[len(out)-1] != in[lastIdx] {
		t.Fatalf("last sample: got %d want in[%d]=%d", out[len(out)-1], lastIdx, in[lastIdx])
	}
}

func TestResampleNearest_InvalidRatesOrEmpty(t *testing.T) {
	if got := resampleNearest([]int16{1}, 0, 8000); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
	if got := resampleNearest([]int16{1}, 8000, 0); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
	if got := resampleNearest(nil, 8000, 8000); got != nil {
		t.Fatalf("want nil, got %v", got)
	}
}

func TestLoadPCM8kMono_FileNotFound(t *testing.T) {
	_, err := LoadPCM8kMono("/no/such/file/for/mp3_test.mp3")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadPCM8kMono_RealAsset(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	path := filepath.Join(dir, "..", "..", "..", "audio", "dcwarning.mp3")
	path = filepath.Clean(path)

	pcm, err := LoadPCM8kMono(path)
	if err != nil {
		t.Fatalf("LoadPCM8kMono(%s): %v", path, err)
	}
	if len(pcm) < 8000 {
		t.Fatalf("short pcm: %d samples", len(pcm))
	}
}
