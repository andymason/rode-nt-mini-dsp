package protocol

import (
	"encoding/binary"
	"math"
)

// Encoders transcribed from RØDE Connect.exe (SHA-256 c3015816…1023c).
//
// The formulas are read off the instruction stream, not fitted to captures; see
// docs/re/03-encoders.md for the decompilation and docs/re/01-addresses.md for
// the addresses. Three properties of the original are reproduced deliberately:
//
//   - Indices truncate (CVTTSS2SI), they do not round.
//   - The final scaling happens in float32 (MULSS), so it is done here in
//     float32 too; float64 would disagree by an LSB near index boundaries.
//   - Noise gate coefficients saturate rather than wrap.
//
// One known fidelity limit: the binary evaluates powf, logf and cos in float32,
// whereas Go evaluates math.Pow, math.Log and math.Cos in float64 and rounds
// once at the end. That can differ by a single LSB — noise gate threshold at
// -58.8 dB is one such case, one part in 2.4 million on a 31-bit coefficient.
// Matching bit-for-bit would mean reimplementing the MSVC float32 runtime, which
// is not worth it for a difference far below the device's resolution.
//
// SCALE AND FIRMWARE. The binary picks Q31 (2^31, saturating) or Q16 (65536) at
// FUN_140102e90 based on the *firmware version*, not the device model: an
// NT-USB Mini reports Q31 only above firmware 2.1.2. Every capture in this
// repository shows Q31, and rode-dsp does not read the firmware version, so Q31
// is what is implemented. A microphone on 2.1.2 or older would need the Q16
// path, which is a one-line change here but needs the version plumbed through
// from the device.

// clamp constrains v to [lo, hi], mapping NaN to lo.
func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) || v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// lutIndex reproduces the binary's float-to-index conversion: CVTTSS2SI
// (truncate toward zero) followed by MOVZX reg, AL (keep the low 8 bits).
//
// The binary has no clamping at any of the eight index sites — an out-of-range
// value wraps, so -70 dB of compressor threshold would read entry 41 rather
// than 255. RØDE Connect's sliders make that unreachable, so there is no
// behaviour worth imitating; callers here clamp to the documented UI range
// first, which makes the mask a no-op. It is applied anyway so that the result
// is always a valid index into a 256-entry table.
func lutIndex(scaled float32) int {
	// CVTTSS2SI produces 0x80000000 for NaN and for anything outside int32,
	// and 0x80000000&0xFF is zero.
	if math.IsNaN(float64(scaled)) || scaled >= 2147483648.0 || scaled < -2147483648.0 {
		return 0
	}
	return int(int32(scaled)) & 0xFF
}

// q31 converts a normalised coefficient to the wire format: multiply by 2^31 in
// float32, truncate, then saturate if the integer's sign disagrees with the
// float's. That sign check is the binary's overflow test, and it is why a 0 dB
// noise gate threshold transmits 0x7FFFFFFF — 10^0 × 2^31 overflows int32.
func q31(v float32) uint32 {
	scaled := v * 2147483648.0

	n := int32(math.MinInt32) // CVTTSS2SI's "integer indefinite"
	if !math.IsNaN(float64(scaled)) && scaled < 2147483648.0 && scaled >= -2147483648.0 {
		n = int32(scaled)
	}

	if (v > 0) != (n > 0) {
		if v > 0 {
			return 0x7FFFFFFF
		}
		return 0x80000000
	}
	return uint32(n)
}

// u32le packs a coefficient into the 4-byte little-endian payload the device
// expects.
func u32le(v uint32) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, v)
	return buf
}

// --- Compressor (FUN_140375130, Mode 2) -------------------------------------
//
// Each parameter's index is exposed alongside its encoder. The GUI shows these
// to explain what a slider position actually becomes, and deriving them from
// the same function the encoder uses keeps the two from drifting apart.

// CompThresholdIndex maps -60.0..0.0 dB onto the table index. It runs
// backwards: 0 dB is entry 0 and -60 dB is entry 255.
func CompThresholdIndex(db float64) int {
	return lutIndex(float32((1.0 - (clamp(db, -60.0, 0.0) - -60.0)/60.0) * 255.0))
}

// CompRatioIndex maps 1.5..4.5:1 onto the index that is itself the payload.
func CompRatioIndex(ratio float64) int {
	return lutIndex(float32((clamp(ratio, 1.5, 4.5) - 1.5) / 3.0 * 255.0))
}

// CompAttackIndex maps 0.1..10.0 ms logarithmically onto the table index.
func CompAttackIndex(ms float64) int {
	return lutIndex(float32(math.Log(clamp(ms, 0.1, 10.0)/0.1) / math.Log(100.0) * 255.0))
}

// CompReleaseIndex maps 5.0..200.0 ms logarithmically onto the table index.
func CompReleaseIndex(ms float64) int {
	return lutIndex(float32(math.Log(clamp(ms, 5.0, 200.0)/5.0) / math.Log(40.0) * 255.0))
}

// CompGainIndex maps 0.0..9.0 dB onto the table index. The 9.0 is 0x142279fc0
// in the binary; see Q3 in docs/re/04-open-questions.md.
func CompGainIndex(db float64) int {
	return lutIndex(float32(clamp(db, 0.0, 9.0) / 9.0 * 255.0))
}

// EncodeCompThreshold encodes compressor threshold, -60.0 to 0.0 dB.
func EncodeCompThreshold(db float64) []byte {
	return u32le(CompThresholdTable[CompThresholdIndex(db)])
}

// EncodeCompRatio encodes compressor ratio, 1.5 to 4.5:1. Alone among the
// compressor parameters this has no lookup table — the index is the payload.
func EncodeCompRatio(ratio float64) []byte {
	return []byte{byte(CompRatioIndex(ratio))}
}

// EncodeCompAttack encodes compressor attack time, 0.1 to 10.0 ms, logarithmic.
func EncodeCompAttack(ms float64) []byte {
	return u32le(CompAttackTable[CompAttackIndex(ms)])
}

// EncodeCompRelease encodes compressor release time, 5.0 to 200.0 ms, logarithmic.
func EncodeCompRelease(ms float64) []byte {
	return u32le(CompReleaseTable[CompReleaseIndex(ms)])
}

// EncodeCompGain encodes compressor make-up gain, 0.0 to 9.0 dB.
func EncodeCompGain(db float64) []byte {
	return u32le(CompGainTable[CompGainIndex(db)])
}

// --- Aural Exciter (FUN_140372910) and Big Bottom (FUN_140372080) ------------
//
// Both send the looked-up coefficient followed by the raw index byte. A second
// firmware gate (FUN_1401030b0) switches newer firmware to an index-only form
// in which the device performs the lookup itself; the captures show the
// coefficient form, which is what is implemented.

// HarmonicsDriveIndex maps 0.0..100.0% onto the shared harmonics/drive table
// index. Aural Exciter harmonics and Big Bottom drive use it identically.
func HarmonicsDriveIndex(pct float64) int {
	return lutIndex(float32(clamp(pct, 0.0, 100.0) / 100.0 * 255.0))
}

// AETuneIndex maps 600.0..5000.0 Hz onto the index shared by both tune tables.
func AETuneIndex(hz float64) int {
	return lutIndex(float32((clamp(hz, 600.0, 5000.0) - 600.0) / 4400.0 * 255.0))
}

// BBTuneIndex maps 60.0..312.0 Hz onto the index that is itself the payload.
func BBTuneIndex(hz float64) int {
	return lutIndex(float32((clamp(hz, 60.0, 312.0) - 60.0) / 252.0 * 255.0))
}

// EncodeAEHarmonics encodes Aural Exciter harmonics, 0.0 to 100.0%.
func EncodeAEHarmonics(pct float64) []byte {
	idx := HarmonicsDriveIndex(pct)
	buf := make([]byte, 5)
	binary.LittleEndian.PutUint32(buf[:4], HarmonicsDriveTable[idx])
	buf[4] = byte(idx)
	return buf
}

// EncodeAETune encodes Aural Exciter tune, 600.0 to 5000.0 Hz. Tune indexes two
// tables with the same index and sends both coefficients.
func EncodeAETune(hz float64) []byte {
	idx := AETuneIndex(hz)
	buf := make([]byte, 9)
	binary.LittleEndian.PutUint32(buf[:4], AETune1Table[idx])
	binary.LittleEndian.PutUint32(buf[4:8], AETune2Table[idx])
	buf[8] = byte(idx)
	return buf
}

// EncodeBBDrive encodes Big Bottom drive, 0.0 to 100.0%. It shares the
// harmonics table with the Aural Exciter and indexes it identically.
func EncodeBBDrive(pct float64) []byte {
	idx := HarmonicsDriveIndex(pct)
	buf := make([]byte, 5)
	binary.LittleEndian.PutUint32(buf[:4], HarmonicsDriveTable[idx])
	buf[4] = byte(idx)
	return buf
}

// EncodeBBTune encodes Big Bottom tune, 60.0 to 312.0 Hz. Like compressor
// ratio, this is a bare index with no table.
func EncodeBBTune(hz float64) []byte {
	return []byte{byte(BBTuneIndex(hz))}
}

// --- Noise gate (FUN_1403738a0) ---------------------------------------------
//
// No lookup tables. Note the unit inconsistency, which is in the original:
// attack arrives in milliseconds and is divided by 1000 inside the transform,
// while hold and release arrive already in seconds. The Go API takes
// milliseconds throughout and converts.

// NGThresholdToUSB converts a dB level to the device's amplitude coefficient.
// At 0 dB this saturates to 0x7FFFFFFF.
func NGThresholdToUSB(db float64) uint32 {
	return q31(float32(math.Pow(10.0, clamp(db, -60.0, 0.0)/20.0)))
}

// NGRangeToUSB converts noise gate range dB. Same mapping as threshold over a
// wider span.
func NGRangeToUSB(db float64) uint32 {
	return q31(float32(math.Pow(10.0, clamp(db, -100.0, 0.0)/20.0)))
}

// NGAttackToUSB converts noise gate attack time to a one-pole filter
// coefficient (FUN_1401031b0).
//
// With b = 2 - cos(w) this is 1 - (b - sqrt(b²-1)): the textbook one-pole
// time-constant design, where b - sqrt(b²-1) is the pole. Writing it in the
// binary's own algebraic form keeps the correspondence obvious.
func NGAttackToUSB(ms float64) uint32 {
	ms = clamp(ms, 0.1, 1000.0)
	w := 5.0 / ((ms / 1000.0) * 48000.0)
	c := math.Cos(w)
	return q31(float32(math.Sqrt(c*c-4.0*c+3.0) + c - 1.0))
}

// NGHoldToUSB converts noise gate hold time. Unlike attack this is the bare
// reciprocal, with no filter transform.
func NGHoldToUSB(ms float64) uint32 {
	ms = clamp(ms, 50.0, 2000.0)
	return q31(float32(1.0 / ((ms / 1000.0) * 48000.0)))
}

// NGReleaseToUSB converts noise gate release time, identically to hold.
func NGReleaseToUSB(ms float64) uint32 {
	ms = clamp(ms, 50.0, 2000.0)
	return q31(float32(1.0 / ((ms / 1000.0) * 48000.0)))
}

// NGHysteresisToUSB converts noise gate hysteresis. The binary takes a 0..1
// fraction and maps it onto -1.0 to -8.0 dB; this API takes a percentage.
func NGHysteresisToUSB(pct float64) uint32 {
	h := clamp(pct, 0.0, 100.0) / 100.0
	return q31(float32(math.Pow(10.0, (-1.0-7.0*h)/20.0)))
}

func EncodeNGThreshold(db float64) []byte   { return u32le(NGThresholdToUSB(db)) }
func EncodeNGAttack(ms float64) []byte      { return u32le(NGAttackToUSB(ms)) }
func EncodeNGHold(ms float64) []byte        { return u32le(NGHoldToUSB(ms)) }
func EncodeNGRelease(ms float64) []byte     { return u32le(NGReleaseToUSB(ms)) }
func EncodeNGRange(db float64) []byte       { return u32le(NGRangeToUSB(db)) }
func EncodeNGHysteresis(pct float64) []byte { return u32le(NGHysteresisToUSB(pct)) }
