package imaadpcm

import "testing"

func TestIMAADPCMEncodeDecodeRoundTripLowAmplitude(t *testing.T) {
	samples := make([]int16, 256)
	for i := range samples {
		samples[i] = int16(i%3 - 1)
	}
	enc := &IMAADPCMEncoderState{}
	payload := IMAADPCMEncodeFrame(samples, enc)
	if len(payload) != len(samples)/2 {
		t.Fatalf("payload len: got %d want %d", len(payload), len(samples)/2)
	}
	dec := &IMAADPCMDecoderState{}
	got := IMAADPCMDecodeFrame(payload, dec)
	if len(got) != len(samples) {
		t.Fatalf("decoded len: got %d want %d", len(got), len(samples))
	}
	for i := range samples {
		d := int(got[i]) - int(samples[i])
		if d < 0 {
			d = -d
		}
		if d > 2 {
			t.Fatalf("i=%d got=%d want=%d (delta %d)", i, got[i], samples[i], d)
		}
	}
}

func TestIMAADPCMEncodeDecodeKeepsPredictorAndIndexMatched(t *testing.T) {
	enc := &IMAADPCMEncoderState{}
	dec := &IMAADPCMDecoderState{}
	samples := []int16{-3500, -3000, 100, -200, 0, 32767, -300, 1200}
	for i, sample := range samples {
		code := EncodeLinear(sample, enc)
		_ = DecodeLinear(code, dec)
		if enc.predictor != dec.predictor || enc.index != dec.index {
			t.Fatalf("step %d sample %d: enc pred=%d idx=%d dec pred=%d idx=%d",
				i, sample, enc.predictor, enc.index, dec.predictor, dec.index)
		}
	}
}

func TestIMAADPCMEncodeFrameOddSampleCountExtraDecodedNibble(t *testing.T) {
	samples := []int16{-1, 0, 1}
	enc := &IMAADPCMEncoderState{}
	payload := IMAADPCMEncodeFrame(samples, enc)
	if len(payload) != 2 {
		t.Fatalf("len(payload)=%d", len(payload))
	}
	dec := &IMAADPCMDecoderState{}
	got := IMAADPCMDecodeFrame(payload, dec)
	if len(got) != 4 {
		t.Fatalf("decode len=%d", len(got))
	}
	for i := range samples {
		d := int(got[i]) - int(samples[i])
		if d < 0 {
			d = -d
		}
		if d > 2 {
			t.Fatalf("i=%d got=%d want=%d", i, got[i], samples[i])
		}
	}
}
