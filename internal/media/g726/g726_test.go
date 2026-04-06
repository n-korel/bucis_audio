package g726

import (
	"fmt"
	"math"
	"testing"
)

func float11Eq(a, b float11) bool {
	return a.sign == b.sign && a.exp == b.exp && a.mant == b.mant
}

func coresEqual(enc *G726EncoderState, dec *G726DecoderState) bool {
	a, b := &enc.st, &dec.st
	if a.code != b.code {
		return false
	}
	for i := range a.sr {
		if !float11Eq(a.sr[i], b.sr[i]) {
			return false
		}
	}
	for i := range a.dq {
		if !float11Eq(a.dq[i], b.dq[i]) {
			return false
		}
	}
	for i := range a.a {
		if a.a[i] != b.a[i] {
			return false
		}
	}
	for i := range a.b {
		if a.b[i] != b.b[i] {
			return false
		}
	}
	for i := range a.pk {
		if a.pk[i] != b.pk[i] {
			return false
		}
	}
	return a.ap == b.ap && a.yu == b.yu && a.yl == b.yl &&
		a.dms == b.dms && a.dml == b.dml && a.td == b.td &&
		a.se == b.se && a.sez == b.sez && a.y == b.y
}

func TestG726EncodeFrame160Bytes(t *testing.T) {
	samples := make([]int16, 160)
	var enc G726EncoderState
	payload := G726EncodeFrame(samples, &enc)
	if len(payload) != 80 {
		t.Fatalf("len(payload)=%d, want 80", len(payload))
	}
}

func TestG726EncoderDecoderStateMatchesAfterEachSample(t *testing.T) {
	var enc G726EncoderState
	var dec G726DecoderState
	n := 500
	for i := 0; i < n; i++ {
		x := int16((i*173 + i*i) % 12000)
		if i%2 == 0 {
			x = -x
		}
		code := EncodeLinear(x, &enc)
		DecodeLinear(code, &dec)
		if !coresEqual(&enc, &dec) {
			t.Fatalf("core mismatch after sample %d", i)
		}
	}
}

func TestG726RoundTripSilence(t *testing.T) {
	const n = 160 * 10
	samples := make([]int16, n)
	var enc G726EncoderState
	var dec G726DecoderState
	payload := G726EncodeFrame(samples, &enc)
	got := G726DecodeFrame(payload, &dec)
	if len(got) != n {
		t.Fatalf("len(got)=%d want %d", len(got), n)
	}
	var maxErr int
	for i := range got {
		e := int(got[i] - samples[i])
		if e < 0 {
			e = -e
		}
		if e > maxErr {
			maxErr = e
		}
	}
	if maxErr > 8 {
		t.Fatalf("silence max abs err %d, want <= 8", maxErr)
	}
}

func sineSamples(n int, freqHz float64, sr float64) []int16 {
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		v := math.Sin(2 * math.Pi * freqHz * float64(i) / sr)
		out[i] = int16(v * 3000)
	}
	return out
}

func maxAbsDiff(a, b []int16) int {
	var m int
	for i := range a {
		d := int(a[i] - b[i])
		if d < 0 {
			d = -d
		}
		if d > m {
			m = d
		}
	}
	return m
}

func TestG726RoundTripSines(t *testing.T) {
	const n = 160 * 50
	const sr = 8000.0
	for _, hz := range []float64{300, 1000, 3200} {
		hz := hz
		t.Run(fmt.Sprintf("%gHz", hz), func(t *testing.T) {
			t.Parallel()
			in := sineSamples(n, hz, sr)
			var enc G726EncoderState
			var dec G726DecoderState
			out := G726DecodeFrame(G726EncodeFrame(in, &enc), &dec)
			m := maxAbsDiff(in, out)
			if m > 3500 {
				t.Fatalf("freq=%v Hz max abs err %d, want <= 3500", hz, m)
			}
		})
	}
}

func TestG726OddSampleCountSecondNibbleZero(t *testing.T) {
	samples := []int16{100, 200, 300}
	var enc G726EncoderState
	payload := G726EncodeFrame(samples, &enc)
	if len(payload) != 2 {
		t.Fatalf("len(payload)=%d want 2", len(payload))
	}
	var dec G726DecoderState
	got := G726DecodeFrame(payload, &dec)
	if len(got) != 4 {
		t.Fatalf("len(got)=%d want 4", len(got))
	}
	var enc2 G726EncoderState
	var dec2 G726DecoderState
	full := []int16{100, 200, 300, 0}
	payloadFull := G726EncodeFrame(full, &enc2)
	gotFull := G726DecodeFrame(payloadFull, &dec2)
	for i := 0; i < 3; i++ {
		if got[i] != gotFull[i] {
			t.Fatalf("sample %d: odd=%d full=%d", i, got[i], gotFull[i])
		}
	}
}
