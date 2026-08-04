package protocol

import (
	"encoding/binary"
	"math"
	"testing"
)

// This file is the regression net for the encoders, built from the February 2026
// USB captures rather than from any table in this package.
//
// It was written before the encoders were known exactly, to survive the LUT
// replacement. That replacement has now happened, along with the transcription
// of the real formulas out of RØDE Connect.exe (docs/re/08-encoders.md), and the
// outcome reorganised this file:
//
//   - Every capture taken at the TOP of a slider is reproduced exactly, as are
//     the three LUT indices that distinguish truncation from rounding. Those are
//     in capturedVectors().
//   - Every capture taken at the BOTTOM of a slider is reproduced exactly at an
//     input slightly inside the labelled one. Those are in
//     TestCaptureLabelsAreApproximate, which asserts the implied input, because
//     asserting the label would be asserting a measurement error.
//
// See docs/re/02-protocol.md for the capture methodology.

func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b[:4]) }

type vector struct {
	name string
	got  func() uint32
	want uint32
}

// capturedVectors are UI endpoints whose encoding is confirmed against USB
// captures of RØDE Connect. These must never change.
func capturedVectors() []vector {
	return []vector{
		// Compressor — the top of each slider, which is where the sweeps
		// actually reached the end of travel.
		{"CompThreshold(0dB)", func() uint32 { return le32(EncodeCompThreshold(0)) }, 0x00000000},
		{"CompAttack(10ms)", func() uint32 { return le32(EncodeCompAttack(10)) }, 0x00011EB8},
		{"CompRelease(200ms)", func() uint32 { return le32(EncodeCompRelease(200)) }, 0x00004EE3},
		{"CompGain(9dB)", func() uint32 { return le32(EncodeCompGain(9)) }, 0x74E2B063},

		// Noise gate. 0 dB saturates because 10^0 x 2^31 overflows int32, which
		// is the clearest single confirmation that this device takes the Q31
		// branch of FUN_140102e90 rather than the Q16 one.
		{"NGThreshold(0dB)", func() uint32 { return le32(EncodeNGThreshold(0)) }, 0x7FFFFFFF},
		{"NGRange(0dB)", func() uint32 { return le32(EncodeNGRange(0)) }, 0x7FFFFFFF},
		{"NGAttack(1000ms)", func() uint32 { return le32(EncodeNGAttack(1000)) }, 0x000369C4},
		{"NGHold(2000ms)", func() uint32 { return le32(EncodeNGHold(2000)) }, 0x00005761},
		{"NGRelease(2000ms)", func() uint32 { return le32(EncodeNGRelease(2000)) }, 0x00005761},

		// Hysteresis at full scale is -8.0 dB exactly, from 10^((-1-7h)/20).
		{"NGHysteresis(100%)", func() uint32 { return le32(EncodeNGHysteresis(100)) }, 0x32F52D00},

		// Aural Exciter and Big Bottom share the harmonics/drive LUT; both must
		// reach the same 0x7FFFFFFF terminus.
		{"AEHarmonics(100%)", func() uint32 { return le32(EncodeAEHarmonics(100)) }, 0x7FFFFFFF},
		{"BBDrive(100%)", func() uint32 { return le32(EncodeBBDrive(100)) }, 0x7FFFFFFF},
	}
}

// TestTruncationMatchesCapturedIndices is the resolution of Q2. These three UI
// defaults come from the Phase 1 INIT capture and are the cases where truncating
// and rounding disagree:
//
//	Parameter        UI default  raw index  round  trunc  captured
//	AE Harmonics          49 %     124.950    125    124       124
//	AE Tune             3516 Hz    168.996    169    168       168
//	BB Tune              131 Hz     71.845     72     71        71
//
// The encoders truncate, because CVTTSS2SI with no preceding ADDSS 0.5 and no
// ROUNDSS is what FUN_140375130 and friends do at all eight index sites. That
// these three then match the capture is independent confirmation: the captures
// played no part in reading the instruction.
func TestTruncationMatchesCapturedIndices(t *testing.T) {
	cases := []struct {
		name string
		got  byte
		want byte
	}{
		{"AEHarmonics(49%)", EncodeAEHarmonics(49)[4], 0x7C},
		{"AETune(3516Hz)", EncodeAETune(3516)[8], 0xA8},
		{"BBTune(131Hz)", EncodeBBTune(131)[0], 0x47},
		// BB Drive is the control: rounding and truncation agree here, so it
		// matched before this change and must keep matching.
		{"BBDrive(62%) index", EncodeBBDrive(62)[4], 0x9E},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s index = %#02x, want %#02x (from USB capture)", c.name, c.got, c.want)
		}
	}

	if got, want := le32(EncodeBBDrive(62)), uint32(0x158C2600); got != want {
		t.Errorf("BBDrive(62%%) LUT = %#08x, want %#08x", got, want)
	}
}

// TestCaptureLabelsAreApproximate covers every captured value that the exact
// formulas do NOT reproduce at its labelled UI value — and shows that each is
// reproduced exactly a little way inside the label.
//
// All ten are at the BOTTOM of a slider; every top-of-slider capture is exact
// (capturedVectors above). The compressor cases land 2 to 5 table indices in
// from entry 0 or 255. CompGain is the one that settles the question: its
// slider cannot go below 0 dB, yet the capture labelled "0 dB" is table entry 5,
// which is 0.19 dB. No encoding rule can produce that from an input of 0, so the
// label is wrong, not the formula.
//
// Three of the implied inputs are suspiciously round — -58.80000 dB where the
// label says -60, -98.00000 dB where it says -100, and 1.00000 % where it says
// 0. The first two are exactly 98 % of full range. That points at sweeps
// parameterised by percentage of travel and then labelled with the nominal
// endpoint, rather than at random imprecision in a mouse drag.
//
// Closing this properly needs a fresh capture with each slider deliberately
// parked at its minimum — Step 4 of docs/re/07-workplan.md, which needs the
// microphone.
func TestCaptureLabelsAreApproximate(t *testing.T) {
	// Tolerances differ by how tightly the implied input is pinned:
	//
	//   - The compressor parameters are table-quantised, so a whole band of inputs
	//     maps to one entry and the reproduction is exact.
	//   - NGRange and NGHysteresis are continuous, but their implied inputs are
	//     round numbers that reproduce the capture bit-for-bit.
	//   - NGThreshold is off by one LSB because the binary evaluates powf in
	//     float32 while Go evaluates math.Pow in float64 and rounds once at the
	//     end. One part in 2.4 million on a 31-bit coefficient.
	//   - NGAttack, NGHold and NGRelease have implied inputs pinned far more
	//     tightly than a five-decimal literal can express — NGAttack's bracket is
	//     narrower than 1e-7 ms — so they get a relative tolerance. Even so they
	//     land within 0.01 % of the capture, against a ~10 % gap at the label.
	cases := []struct {
		name     string
		labelled float64
		implied  float64
		captured uint32
		tol      float64 // absolute; 0 means exact
		fn       func(float64) []byte
	}{
		{"CompThreshold", -60, -59.41176, 0x766B0000, 0, EncodeCompThreshold},
		{"CompAttack", 0.1, 0.10462, 0x06104000, 0, EncodeCompAttack},
		{"CompRelease", 5, 5.18427, 0x001C3954, 0, EncodeCompRelease},
		{"CompGain", 0, 0.19412, 0x02322AF5, 0, EncodeCompGain},
		{"NGRange", -100, -98.0, 0x0000699B, 0, EncodeNGRange},
		{"NGHysteresis", 0, 1.0, 0x712A1880, 0, EncodeNGHysteresis},
		{"NGThreshold", -60, -58.8, 0x00259F68, 1, EncodeNGThreshold},
		{"NGAttack", 0.1, 0.10965, 0x4B329800, 0x4B329800 * 1e-4, EncodeNGAttack},
		{"NGHold", 50, 51.87890, 0x000D28AA, 0x000D28AA * 1e-4, EncodeNGHold},
		{"NGRelease", 50, 53.82835, 0x000CAEAA, 0x000CAEAA * 1e-4, EncodeNGRelease},
	}

	for _, c := range cases {
		got := le32(c.fn(c.implied))
		if delta := math.Abs(float64(got) - float64(c.captured)); delta > c.tol {
			t.Errorf("%s(%v) = %#08x, %.0f away from the captured %#08x (tolerance %.0f) — "+
				"the implied input no longer explains the capture",
				c.name, c.implied, got, delta, c.captured, c.tol)
		}
		if le32(c.fn(c.labelled)) == c.captured {
			t.Errorf("%s(%v) now reproduces the capture at its labelled value; "+
				"move it into capturedVectors() and update docs/re/08-encoders.md",
				c.name, c.labelled)
		}
	}
}

func TestEncodersMatchCapturedDevice(t *testing.T) {
	for _, v := range capturedVectors() {
		if got := v.got(); got != v.want {
			t.Errorf("%s = %#08x, want %#08x (from USB capture)", v.name, got, v.want)
		}
	}
}

func TestSharedHarmonicsDriveLUT(t *testing.T) {
	// The February 2026 captures confirmed Aural Exciter harmonics and Big Bottom
	// drive read the same table at DAT_1407391b0. Equal percentages must encode
	// to equal LUT values and equal indices.
	for pct := 0.0; pct <= 100.0; pct += 0.5 {
		ae, bb := EncodeAEHarmonics(pct), EncodeBBDrive(pct)
		if le32(ae) != le32(bb) || ae[4] != bb[4] {
			t.Errorf("at %.1f%%: AE harmonics = %#08x/idx %#02x, BB drive = %#08x/idx %#02x",
				pct, le32(ae), ae[4], le32(bb), bb[4])
		}
	}
}

func TestIndexEndpoints(t *testing.T) {
	cases := []struct {
		name string
		got  byte
		want byte
	}{
		{"BBTune(60Hz)", EncodeBBTune(60)[0], 0x00},
		{"BBTune(312Hz)", EncodeBBTune(312)[0], 0xFF},
		{"AEHarmonics(100%) index", EncodeAEHarmonics(100)[4], 0xFF},
		{"BBDrive(100%) index", EncodeBBDrive(100)[4], 0xFF},
		{"AETune(600Hz) index", EncodeAETune(600)[8], 0x00},
		{"AETune(5000Hz) index", EncodeAETune(5000)[8], 0xFF},
		{"CompRatio(1.5) index", EncodeCompRatio(1.5)[0], 0x00},
		{"CompRatio(4.5) index", EncodeCompRatio(4.5)[0], 0xFF},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %#02x, want %#02x", tc.name, tc.got, tc.want)
		}
	}
}

func TestAETuneDualLUTEndpoints(t *testing.T) {
	lo := EncodeAETune(600)
	if got, want := le32(lo[0:4]), uint32(0x0B000000); got != want {
		t.Errorf("AETune(600Hz) LUT1 = %#08x, want %#08x", got, want)
	}
	if got, want := le32(lo[4:8]), uint32(0x24000000); got != want {
		t.Errorf("AETune(600Hz) LUT2 = %#08x, want %#08x", got, want)
	}

	hi := EncodeAETune(5000)
	if got, want := le32(hi[0:4]), uint32(0x54000000); got != want {
		t.Errorf("AETune(5000Hz) LUT1 = %#08x, want %#08x", got, want)
	}
	if got, want := le32(hi[4:8]), uint32(0x7FFFFFFF); got != want {
		t.Errorf("AETune(5000Hz) LUT2 = %#08x, want %#08x", got, want)
	}
}

// TestNoEncoderProducesNaNDerivedGarbage sweeps every encoder densely and checks
// the output is a plausible DSP coefficient. int(NaN) and int(±Inf) in Go yield
// implementation-defined values, so a domain error upstream surfaces here as a
// wild coefficient rather than a crash.
func TestNoEncoderProducesNaNDerivedGarbage(t *testing.T) {
	encoders := []struct {
		name     string
		min, max float64
		fn       func(float64) []byte
	}{
		{"CompThreshold", -60, 0, EncodeCompThreshold},
		{"CompAttack", 0.1, 10, EncodeCompAttack},
		{"CompRelease", 5, 200, EncodeCompRelease},
		{"CompGain", 0, 9, EncodeCompGain},
		{"NGThreshold", -60, 0, EncodeNGThreshold},
		{"NGAttack", 0.1, 1000, EncodeNGAttack},
		{"NGHold", 50, 2000, EncodeNGHold},
		{"NGRelease", 50, 2000, EncodeNGRelease},
		{"NGRange", -100, 0, EncodeNGRange},
		{"NGHysteresis", 0, 100, EncodeNGHysteresis},
	}

	for _, e := range encoders {
		const steps = 200
		span := e.max - e.min
		for i := 0; i <= steps; i++ {
			v := e.min + span*float64(i)/steps
			raw := e.fn(v)
			if len(raw) < 4 {
				t.Fatalf("%s(%v) returned %d bytes", e.name, v, len(raw))
			}
			// Every observed coefficient is a non-negative 31-bit quantity: the
			// device treats these as signed 32-bit and the captures top out at
			// 0x7FFFFFFF.
			if q := le32(raw); q > 0x7FFFFFFF {
				t.Errorf("%s(%v) = %#08x, exceeds the 0x7FFFFFFF ceiling seen in captures",
					e.name, v, q)
			}
		}
	}
}

// TestMonotonicity checks each encoder moves in one direction across its range.
// A non-monotonic step means the LUT index mapping has folded back on itself,
// which is the failure mode to watch for when the real 256-entry tables land.
func TestMonotonicity(t *testing.T) {
	encoders := []struct {
		name       string
		min, max   float64
		fn         func(float64) []byte
		increasing bool
	}{
		{"CompThreshold", -60, 0, EncodeCompThreshold, false},
		{"CompAttack", 0.1, 10, EncodeCompAttack, false},
		{"CompRelease", 5, 200, EncodeCompRelease, false},
		{"CompGain", 0, 9, EncodeCompGain, true},
		{"NGThreshold", -60, 0, EncodeNGThreshold, true},
		{"NGAttack", 0.1, 1000, EncodeNGAttack, false},
		{"NGHold", 50, 2000, EncodeNGHold, false},
		{"NGRelease", 50, 2000, EncodeNGRelease, false},
		{"NGRange", -100, 0, EncodeNGRange, true},
		{"NGHysteresis", 0, 100, EncodeNGHysteresis, false},
	}

	for _, e := range encoders {
		const steps = 500
		span := e.max - e.min
		prev := le32(e.fn(e.min))

		for i := 1; i <= steps; i++ {
			v := e.min + span*float64(i)/steps
			cur := le32(e.fn(v))

			var bad bool
			if e.increasing {
				bad = cur < prev
			} else {
				bad = cur > prev
			}
			if bad {
				dir := map[bool]string{true: "increasing", false: "decreasing"}[e.increasing]
				t.Errorf("%s is not %s at %v: %#08x then %#08x", e.name, dir, v, prev, cur)
				break
			}
			prev = cur
		}
	}
}
