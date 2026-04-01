package g726

var imaIndexTable = [16]int{
	-1, -1, -1, -1, 2, 4, 6, 8,
	-1, -1, -1, -1, 2, 4, 6, 8,
}

var imaStepTable = [89]int{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17,
	19, 21, 23, 25, 28, 31, 34, 37, 41, 45,
	50, 55, 60, 66, 73, 80, 88, 97, 107, 118,
	130, 143, 157, 173, 190, 209, 230, 253, 279, 307,
	337, 371, 408, 449, 494, 544, 598, 658, 724, 796,
	876, 963, 1060, 1166, 1282, 1411, 1552, 1707, 1878, 2066,
	2272, 2499, 2749, 3024, 3327, 3660, 4026, 4428, 4871, 5358,
	5894, 6484, 7132, 7845, 8630, 9493, 10442, 11487, 12635, 13899,
	15289, 16818, 18500, 20350, 22385, 24623, 27086, 29794, 32767,
}

type EncoderState struct {
	predictor int
	index     int
}

type DecoderState struct {
	predictor int
	index     int
}

func EncodeLinear(sample int16, state *EncoderState) byte {
	step := imaStepTable[state.index]
	diff := int(sample) - state.predictor
	code := 0
	if diff < 0 {
		code = 8
		diff = -diff
	}

	delta := 0
	vpdiff := step >> 3
	if diff >= step {
		code |= 4
		diff -= step
		vpdiff += step
	}
	if diff >= step>>1 {
		code |= 2
		diff -= step >> 1
		vpdiff += step >> 1
	}
	if diff >= step>>2 {
		code |= 1
		delta += step >> 2
	}
	vpdiff += delta

	if code&8 != 0 {
		state.predictor -= vpdiff
	} else {
		state.predictor += vpdiff
	}
	if state.predictor > 32767 {
		state.predictor = 32767
	}
	if state.predictor < -32768 {
		state.predictor = -32768
	}

	state.index += imaIndexTable[code]
	if state.index < 0 {
		state.index = 0
	}
	if state.index > 88 {
		state.index = 88
	}

	return byte(code & 0x0F)
}

func DecodeLinear(code byte, state *DecoderState) int16 {
	c := int(code & 0x0F)
	step := imaStepTable[state.index]

	vpdiff := step >> 3
	if c&4 != 0 {
		vpdiff += step
	}
	if c&2 != 0 {
		vpdiff += step >> 1
	}
	if c&1 != 0 {
		vpdiff += step >> 2
	}

	if c&8 != 0 {
		state.predictor -= vpdiff
	} else {
		state.predictor += vpdiff
	}
	if state.predictor > 32767 {
		state.predictor = 32767
	}
	if state.predictor < -32768 {
		state.predictor = -32768
	}

	state.index += imaIndexTable[c]
	if state.index < 0 {
		state.index = 0
	}
	if state.index > 88 {
		state.index = 88
	}
	return int16(state.predictor)
}

func EncodeFrame(samples []int16, state *EncoderState) []byte {
	out := make([]byte, 0, (len(samples)+1)/2)
	for i := 0; i < len(samples); i += 2 {
		lo := EncodeLinear(samples[i], state)
		hi := byte(0)
		if i+1 < len(samples) {
			hi = EncodeLinear(samples[i+1], state)
		}
		out = append(out, lo|(hi<<4))
	}
	return out
}

func DecodeFrame(payload []byte, state *DecoderState) []int16 {
	out := make([]int16, 0, len(payload)*2)
	for _, b := range payload {
		lo := b & 0x0F
		hi := (b >> 4) & 0x0F
		out = append(out, DecodeLinear(lo, state))
		out = append(out, DecodeLinear(hi, state))
	}
	return out
}
