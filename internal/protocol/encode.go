package protocol

import (
	"encoding/binary"
	"math"
)

// clamp01 constrains a fraction to [0, 1]. NaN maps to 0: callers derive
// fractions from logarithms, and a non-positive input would otherwise carry NaN
// through the LUT interpolation and emit a garbage coefficient.
func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0.0 {
		return 0.0
	}
	if v > 1.0 {
		return 1.0
	}
	return v
}

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

// clampIndex constrains a LUT index to [0, 255], mapping NaN to 0.
func clampIndex(v float64) int {
	if math.IsNaN(v) || v < 0.0 {
		return 0
	}
	if v > 255.0 {
		return 255
	}
	return int(math.Round(v))
}

// DBToQ16Raw converts dB to Q16 format (reference=32768.0)
func DBToQ16Raw(db, reference float64) uint32 {
	if db >= 0.0 {
		return 0x7FFFFFFF
	}
	linear := reference * math.Pow(10.0, db/20.0)
	q16 := linear * 65536.0
	result := int64(q16)
	if result < 0 {
		return 0
	}
	if result > 0x7FFFFFFF {
		return 0x7FFFFFFF
	}
	return uint32(result)
}

// NGThresholdToUSB converts noise gate threshold dB to USB value
func NGThresholdToUSB(db float64) uint32 {
	return DBToQ16Raw(db, 32768.0)
}

// NGAttackToUSB converts noise gate attack time to USB value
func NGAttackToUSB(ms float64) uint32 {
	// Approximation — real device uses bilinear transform (FUN_1401031b0)
	// Endpoints are exact; intermediate values may differ from RØDE Connect
	if ms <= 0.1 {
		return 0x4B329800
	}
	if ms >= 1000.0 {
		return 0x000369C4
	}
	invMin := 1.0 / 0.1
	invMax := 1.0 / 1000.0
	invMs := 1.0 / ms
	t := (invMs - invMin) / (invMax - invMin)
	valMin, valMax := float64(0x4B329800), float64(0x000369C4)
	result := valMin + (valMax-valMin)*t
	if result < 0 {
		return 0
	}
	return uint32(result)
}

// NGHoldToUSB converts noise gate hold time to USB value
func NGHoldToUSB(ms float64) uint32 {
	if ms <= 50.0 {
		return 0x000D28AA
	}
	if ms >= 2000.0 {
		return 0x00005761
	}
	invMin := 1.0 / 50.0
	invMax := 1.0 / 2000.0
	invMs := 1.0 / ms
	t := (invMs - invMin) / (invMax - invMin)
	valMin, valMax := float64(0x000D28AA), float64(0x00005761)
	result := valMin + (valMax-valMin)*t
	if result < 0 {
		return 0
	}
	return uint32(result)
}

// NGReleaseToUSB converts noise gate release time to USB value
func NGReleaseToUSB(ms float64) uint32 {
	if ms <= 50.0 {
		return 0x000CAEAA
	}
	if ms >= 2000.0 {
		return 0x00005761
	}
	invMin := 1.0 / 50.0
	invMax := 1.0 / 2000.0
	invMs := 1.0 / ms
	t := (invMs - invMin) / (invMax - invMin)
	valMin, valMax := float64(0x000CAEAA), float64(0x00005761)
	result := valMin + (valMax-valMin)*t
	if result < 0 {
		return 0
	}
	return uint32(result)
}

// NGRangeToUSB converts noise gate range dB to USB value
func NGRangeToUSB(db float64) uint32 {
	return NGThresholdToUSB(db)
}

// NGHysteresisToUSB converts noise gate hysteresis percentage to USB value
func NGHysteresisToUSB(pct float64) uint32 {
	maxQ16 := 28970.10
	minQ16 := 13045.18
	// Percentage is a hard 0-100 range; extrapolating past it would emit
	// coefficients outside the span observed on the device.
	q16 := maxQ16 - (clamp(pct, 0.0, 100.0)/100.0)*(maxQ16-minQ16)
	result := q16 * 65536.0
	if result < 0 {
		return 0
	}
	if result > 0x7FFFFFFF {
		return 0x7FFFFFFF
	}
	return uint32(result)
}

// EncodeCompThreshold encodes compressor threshold (-60.0 to 0.0 dB)
func EncodeCompThreshold(db float64) []byte {
	frac := clamp01((db - (-60.0)) / 60.0)
	val := InterpolateSequentialLUT(CompThresholdLUT, frac)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}

// EncodeCompRatio encodes compressor ratio (1.5 to 4.5:1)
func EncodeCompRatio(ratio float64) []byte {
	return []byte{byte(clampIndex((ratio - 1.5) / 3.0 * 255.0))}
}

// EncodeCompAttack encodes compressor attack time (0.1 to 10.0 ms)
func EncodeCompAttack(ms float64) []byte {
	// Log scaling: ms = 0.1 * 100^t (Ghidra: min=0.1, log_base=100.0)
	// Clamp before the log: ms <= 0 is outside its domain.
	frac := clamp01(math.Log(clamp(ms, 0.1, 10.0)/0.1) / math.Log(100.0))
	val := InterpolateSequentialLUT(CompAttackLUT, frac)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}

// EncodeCompRelease encodes compressor release time (5.0 to 200.0 ms)
func EncodeCompRelease(ms float64) []byte {
	// Log scaling: ms = 5.0 * 40^t (Ghidra: min=5.0, log_base=40.0)
	// Clamp before the log: ms <= 0 is outside its domain.
	frac := clamp01(math.Log(clamp(ms, 5.0, 200.0)/5.0) / math.Log(40.0))
	val := InterpolateSequentialLUT(CompReleaseLUT, frac)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}

// EncodeCompGain encodes compressor gain (0.0 to 9.0 dB)
func EncodeCompGain(db float64) []byte {
	// TODO(docs/re/04-open-questions.md): the 9.0 divisor is not among the float
	// constants recovered from the binary. Confirm the real gain range.
	frac := clamp01(db / 9.0)
	val := InterpolateSequentialLUT(CompGainLUT, frac)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}

// EncodeAEHarmonics encodes Aural Exciter harmonics (0.0 to 100.0%)
func EncodeAEHarmonics(pct float64) []byte {
	idx := clampIndex(pct / 100.0 * 255.0)
	lutVal := InterpolateIndexedLUT(HarmonicsDriveLUT, idx)
	buf := make([]byte, 5)
	binary.LittleEndian.PutUint32(buf[:4], lutVal)
	buf[4] = byte(idx)
	return buf
}

// EncodeAETune encodes Aural Exciter tune (600.0 to 5000.0 Hz)
func EncodeAETune(hz float64) []byte {
	idx := clampIndex((hz - 600.0) / 4400.0 * 255.0)
	lut1, lut2 := InterpolateAETune(idx)
	buf := make([]byte, 9)
	binary.LittleEndian.PutUint32(buf[:4], lut1)
	binary.LittleEndian.PutUint32(buf[4:8], lut2)
	buf[8] = byte(idx)
	return buf
}

// EncodeBBDrive encodes Big Bottom drive (0.0 to 100.0%)
func EncodeBBDrive(pct float64) []byte {
	idx := clampIndex(pct / 100.0 * 255.0)
	lutVal := InterpolateIndexedLUT(HarmonicsDriveLUT, idx)
	buf := make([]byte, 5)
	binary.LittleEndian.PutUint32(buf[:4], lutVal)
	buf[4] = byte(idx)
	return buf
}

// EncodeBBTune encodes Big Bottom tune (60.0 to 312.0 Hz)
func EncodeBBTune(hz float64) []byte {
	return []byte{byte(clampIndex((hz - 60.0) / 252.0 * 255.0))}
}

// EncodeNGThreshold encodes noise gate threshold
func EncodeNGThreshold(db float64) []byte {
	val := NGThresholdToUSB(db)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}

// EncodeNGAttack encodes noise gate attack
func EncodeNGAttack(ms float64) []byte {
	val := NGAttackToUSB(ms)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}

// EncodeNGHold encodes noise gate hold
func EncodeNGHold(ms float64) []byte {
	val := NGHoldToUSB(ms)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}

// EncodeNGRelease encodes noise gate release
func EncodeNGRelease(ms float64) []byte {
	val := NGReleaseToUSB(ms)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}

// EncodeNGRange encodes noise gate range
func EncodeNGRange(db float64) []byte {
	val := NGRangeToUSB(db)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}

// EncodeNGHysteresis encodes noise gate hysteresis
func EncodeNGHysteresis(pct float64) []byte {
	val := NGHysteresisToUSB(pct)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return buf
}
