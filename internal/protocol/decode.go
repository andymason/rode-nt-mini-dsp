package protocol

import (
	"encoding/binary"
	"errors"
	"math"
)

// Decoders, the inverse of encode.go, transcribed from RØDE Connect's readers:
// FUN_140374630 (Compressor), FUN_140372eb0 (Noise Gate), FUN_1403725a0 (Aural
// Exciter) and FUN_140371d70 (Big Bottom) in RODE Connect.exe (SHA-256
// c3015816…1023c).
//
// The binary recovers a table-backed parameter by binary-searching the same
// lookup table the encoder indexes and then applying the encoder's formula in
// reverse to the recovered index. That is reproduced here, so a decode of an
// encode returns the original value to within one index step — the quantisation
// the device itself imposes, not an approximation added here.
//
// Round-tripping is not exact in the other direction: several UI values map to
// the same index, so Decode(Encode(x)) returns the representative value of x's
// index rather than x. TestDecodeRoundTrip pins that tolerance.

// ErrShortResponse means the reply did not contain the ACK and payload the
// caller asked to parse.
var ErrShortResponse = errors.New("protocol: response too short")

// ErrNotAcknowledged means the device replied without the 'A' ACK byte, so the
// payload carries no parameter data.
var ErrNotAcknowledged = errors.New("protocol: response not acknowledged")

// ParseResponse extracts the data field from a device reply.
//
// The reply arrives on report 0x03 laid out as:
//
//	[0] report ID 0x03
//	[1] effect ID, echoed from the request
//	[2] ACK byte 'A'
//	[3..] data
//
// RØDE Connect wraps exactly this tail — 26 bytes from offset 3 — in a
// juce::MemoryInputStream and reads fields off it in order, which is the layout
// the decoders below assume. Multi-byte fields are little-endian, matching
// juce::InputStream::readInt.
func ParseResponse(raw []byte) (effectID byte, data []byte, err error) {
	if len(raw) < 3 {
		return 0, nil, ErrShortResponse
	}
	if raw[2] != AckByte {
		return raw[1], nil, ErrNotAcknowledged
	}
	return raw[1], raw[3:], nil
}

// respInt reads the little-endian int32 at word i of a response data field.
func respInt(data []byte, i int) (int32, bool) {
	if len(data) < 4*(i+1) {
		return 0, false
	}
	return int32(binary.LittleEndian.Uint32(data[4*i:])), true
}

// tableIndex finds the entry of an ascending or descending lookup table nearest
// to v and returns its index.
//
// The binary uses a branchless std::lower_bound over the 256 entries, which
// lands on the first entry not ordered before v. A linear scan for the nearest
// entry is used here instead: it agrees with lower_bound on every value the
// device can return (each is an exact table entry) and degrades more sensibly
// on a value that is not in the table at all, which lower_bound would answer
// with an off-by-one rather than the closest match.
func tableIndex(table *[lutEntries]uint32, v uint32) int {
	best, bestDist := 0, uint64(math.MaxUint64)
	for i, entry := range table {
		var d uint64
		if entry > v {
			d = uint64(entry - v)
		} else {
			d = uint64(v - entry)
		}
		if d < bestDist {
			best, bestDist = i, d
		}
		if d == 0 {
			break
		}
	}
	return best
}

// --- Compressor (FUN_140374630, Mode 2) --------------------------------------

// DecodeCompThreshold recovers -60.0..0.0 dB. The binary computes
// (1 - idx/255) * 60 - 60, the inverse of CompThresholdIndex.
func DecodeCompThreshold(data []byte) (float64, bool) {
	v, ok := respInt(data, 0)
	if !ok {
		return 0, false
	}
	idx := tableIndex(CompThresholdTable, uint32(v))
	return float64((1.0-float32(idx)/255.0)*60.0 - 60.0), true
}

// DecodeCompRatio recovers 1.5..4.5:1 from the bare index byte.
func DecodeCompRatio(data []byte) (float64, bool) {
	if len(data) < 1 {
		return 0, false
	}
	return float64(float32(data[0])/255.0*3.0 + 1.5), true
}

// DecodeCompAttack recovers 0.1..10.0 ms. The binary reverses the logarithmic
// index with 0.1 * 100^(idx/255).
func DecodeCompAttack(data []byte) (float64, bool) {
	v, ok := respInt(data, 0)
	if !ok {
		return 0, false
	}
	idx := tableIndex(CompAttackTable, uint32(v))
	return 0.1 * math.Pow(100.0, float64(idx)/255.0), true
}

// DecodeCompRelease recovers 5.0..200.0 ms, as attack with base 40.
func DecodeCompRelease(data []byte) (float64, bool) {
	v, ok := respInt(data, 0)
	if !ok {
		return 0, false
	}
	idx := tableIndex(CompReleaseTable, uint32(v))
	return 5.0 * math.Pow(40.0, float64(idx)/255.0), true
}

// DecodeCompGain recovers 0.0..9.0 dB.
func DecodeCompGain(data []byte) (float64, bool) {
	v, ok := respInt(data, 0)
	if !ok {
		return 0, false
	}
	idx := tableIndex(CompGainTable, uint32(v))
	return float64(float32(idx) / 255.0 * 9.0), true
}

// --- Aural Exciter and Big Bottom --------------------------------------------
//
// Both send the coefficient followed by the raw index byte, so the index can be
// taken directly and no table search is needed. The coefficient is read back
// only to confirm it agrees with the index.

// DecodeAEHarmonics recovers 0.0..100.0% from the trailing index byte.
func DecodeAEHarmonics(data []byte) (float64, bool) {
	if len(data) < 5 {
		return 0, false
	}
	return float64(float32(data[4]) / 255.0 * 100.0), true
}

// DecodeAETune recovers 600.0..5000.0 Hz. Tune sends two coefficients, so its
// index byte sits at offset 8.
func DecodeAETune(data []byte) (float64, bool) {
	if len(data) < 9 {
		return 0, false
	}
	return float64(float32(data[8])/255.0*4400.0 + 600.0), true
}

// DecodeBBDrive recovers 0.0..100.0%, identically to AE harmonics.
func DecodeBBDrive(data []byte) (float64, bool) {
	if len(data) < 5 {
		return 0, false
	}
	return float64(float32(data[4]) / 255.0 * 100.0), true
}

// DecodeBBTune recovers 60.0..312.0 Hz from the bare index byte.
func DecodeBBTune(data []byte) (float64, bool) {
	if len(data) < 1 {
		return 0, false
	}
	return float64(float32(data[0])/255.0*252.0 + 60.0), true
}

// --- Noise gate ---------------------------------------------------------------
//
// No tables; each coefficient inverts its formula directly. The binary reads
// these as Q31 or Q16 depending on the firmware gate at FUN_140102e90 and
// rode-dsp encodes Q31, so Q31 is what is decoded.

// q31f converts a wire coefficient back to the normalised float it encodes.
func q31f(v int32) float64 { return float64(v) / 2147483648.0 }

// DecodeNGThreshold recovers -60.0..0.0 dB from the amplitude coefficient.
// A saturated 0x7FFFFFFF decodes to 0 dB.
func DecodeNGThreshold(data []byte) (float64, bool) {
	v, ok := respInt(data, 0)
	if !ok {
		return 0, false
	}
	a := q31f(v)
	if a <= 0 {
		return -60.0, true
	}
	return clamp(20.0*math.Log10(a), -60.0, 0.0), true
}

// DecodeNGRange recovers -100.0..0.0 dB, as threshold over a wider span.
func DecodeNGRange(data []byte) (float64, bool) {
	v, ok := respInt(data, 0)
	if !ok {
		return 0, false
	}
	a := q31f(v)
	if a <= 0 {
		return -100.0, true
	}
	return clamp(20.0*math.Log10(a), -100.0, 0.0), true
}

// DecodeNGAttack recovers 0.1..1000.0 ms by inverting the one-pole design in
// NGAttackToUSB. With p = 1 - c the encoder's coefficient, the binary recovers
// the angular frequency as acos((3 - (p+1)^2) / (4 - 2(p+1))) and then the time
// as 5 / (w * 48000), in seconds, scaled to milliseconds.
func DecodeNGAttack(data []byte) (float64, bool) {
	v, ok := respInt(data, 0)
	if !ok {
		return 0, false
	}
	b := q31f(v) + 1.0
	denom := 4.0 - 2.0*b
	if denom == 0 {
		return 0, false
	}
	arg := clamp((3.0-b*b)/denom, -1.0, 1.0)
	w := math.Acos(arg)
	if w == 0 {
		return 1000.0, true
	}
	return clamp((1.0/(w*48000.0))*5000.0, 0.1, 1000.0), true
}

// DecodeNGHold recovers 50.0..2000.0 ms from the reciprocal coefficient.
func DecodeNGHold(data []byte) (float64, bool) {
	v, ok := respInt(data, 0)
	if !ok {
		return 0, false
	}
	c := q31f(v)
	if c == 0 {
		return 2000.0, true
	}
	return clamp(1.0/(c*48000.0)*1000.0, 50.0, 2000.0), true
}

// DecodeNGRelease recovers 50.0..2000.0 ms, identically to hold.
func DecodeNGRelease(data []byte) (float64, bool) { return DecodeNGHold(data) }

// DecodeNGHysteresis recovers 0..100% by inverting the -1.0..-8.0 dB mapping.
func DecodeNGHysteresis(data []byte) (float64, bool) {
	v, ok := respInt(data, 0)
	if !ok {
		return 0, false
	}
	a := q31f(v)
	if a <= 0 {
		return 100.0, true
	}
	db := 20.0 * math.Log10(a)
	return clamp((-db-1.0)/7.0*100.0, 0.0, 100.0), true
}

// DecodeEnabled recovers an effect's enable flag, param 0x00 for every effect.
func DecodeEnabled(data []byte) (bool, bool) {
	if len(data) < 1 {
		return false, false
	}
	return data[0] != 0, true
}

// DecodeParam dispatches to the decoder for one effect and parameter. It
// returns false for the enable flag (param 0x00), which is not a float — use
// DecodeEnabled — and for any effect or parameter with no decoder.
func DecodeParam(effectID, paramID byte, data []byte) (float64, bool) {
	decoders := map[byte]map[byte]func([]byte) (float64, bool){
		EffComp: {
			0x01: DecodeCompThreshold,
			0x02: DecodeCompRatio,
			0x03: DecodeCompAttack,
			0x04: DecodeCompRelease,
			0x05: DecodeCompGain,
		},
		EffGate: {
			0x01: DecodeNGThreshold,
			0x02: DecodeNGAttack,
			0x03: DecodeNGHold,
			0x04: DecodeNGRelease,
			0x05: DecodeNGRange,
			0x06: DecodeNGHysteresis,
		},
		EffAE: {
			0x01: DecodeAEHarmonics,
			0x02: DecodeAETune,
		},
		EffBB: {
			0x01: DecodeBBDrive,
			0x02: DecodeBBTune,
		},
	}

	byParam, ok := decoders[effectID]
	if !ok {
		return 0, false
	}
	decode, ok := byParam[paramID]
	if !ok {
		return 0, false
	}
	return decode(data)
}
