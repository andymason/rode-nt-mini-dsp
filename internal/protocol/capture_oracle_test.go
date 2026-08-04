package protocol

import (
	"encoding/binary"
	"testing"
)

// This file is the regression net for the LUT replacement planned in
// docs/re/03-luts.md.
//
// The tables in lut.go are capture-derived: their values are real bytes observed
// on the wire, but their *indexing* is wrong. The compressor tables are indexed
// 0..N-1 by the count of distinct values seen in a USB sweep, not by the real
// 0..255 table index, so InterpolateSequentialLUT emits intermediate values that
// appear nowhere in the device's table. When those tables are replaced with the
// true 256-entry ones, the vectors below must still hold — they are independent
// evidence, taken from the February 2026 USB captures rather than from lut.go.
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
		// Compressor — LUT endpoints, all four tables.
		{"CompThreshold(-60dB)", func() uint32 { return le32(EncodeCompThreshold(-60)) }, 0x766B0000},
		{"CompThreshold(0dB)", func() uint32 { return le32(EncodeCompThreshold(0)) }, 0x00000000},
		{"CompAttack(0.1ms)", func() uint32 { return le32(EncodeCompAttack(0.1)) }, 0x06104000},
		{"CompAttack(10ms)", func() uint32 { return le32(EncodeCompAttack(10)) }, 0x00011EB8},
		{"CompRelease(5ms)", func() uint32 { return le32(EncodeCompRelease(5)) }, 0x001C3954},
		{"CompRelease(200ms)", func() uint32 { return le32(EncodeCompRelease(200)) }, 0x00004EE3},
		{"CompGain(0dB)", func() uint32 { return le32(EncodeCompGain(0)) }, 0x02322AF5},
		{"CompGain(9dB)", func() uint32 { return le32(EncodeCompGain(9)) }, 0x74E2B063},

		// Noise gate — the parameters whose formulas are confirmed exact.
		{"NGThreshold(0dB)", func() uint32 { return le32(EncodeNGThreshold(0)) }, 0x7FFFFFFF},
		{"NGRange(0dB)", func() uint32 { return le32(EncodeNGRange(0)) }, 0x7FFFFFFF},
		{"NGAttack(0.1ms)", func() uint32 { return le32(EncodeNGAttack(0.1)) }, 0x4B329800},
		{"NGAttack(1000ms)", func() uint32 { return le32(EncodeNGAttack(1000)) }, 0x000369C4},
		{"NGHold(50ms)", func() uint32 { return le32(EncodeNGHold(50)) }, 0x000D28AA},
		{"NGHold(2000ms)", func() uint32 { return le32(EncodeNGHold(2000)) }, 0x00005761},
		{"NGRelease(50ms)", func() uint32 { return le32(EncodeNGRelease(50)) }, 0x000CAEAA},
		{"NGRelease(2000ms)", func() uint32 { return le32(EncodeNGRelease(2000)) }, 0x00005761},

		// Aural Exciter and Big Bottom share the harmonics/drive LUT; both must
		// reach the same 0x7FFFFFFF terminus.
		{"AEHarmonics(100%)", func() uint32 { return le32(EncodeAEHarmonics(100)) }, 0x7FFFFFFF},
		{"BBDrive(100%)", func() uint32 { return le32(EncodeBBDrive(100)) }, 0x7FFFFFFF},
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

// TestKnownDeviationsFromCapture pins four endpoints where the current Go
// encoders do NOT reproduce the captured device values. It asserts today's
// output, so the test fails the moment someone changes these — which is the
// point: fixing them is a deliberate act that should update this table and the
// corresponding entry in docs/re/04-open-questions.md.
//
//	Parameter          ours            device          error
//	NG Threshold -60dB 0x0020C49B      0x00259F68      -60.00 vs -58.80 dB
//	NG Range    -100dB 0x000053E2      0x0000699B     -100.00 vs -98.00 dB
//	NG Hysteresis   0% 0x712A1999      0x712A1880      constant precision
//	NG Hysteresis 100% 0x32F52E14      0x32F52D00      constant precision
//
// The two dB parameters are the substantive ones. The device compresses the
// nominal 60 dB and 100 dB spans into roughly 58.8 dB and 98.0 dB, so
// `reference * 10^(dB/20)` is not the mapping RØDE Connect actually applies; the
// error grows toward the bottom of each range. The two hysteresis entries differ
// only because 28970.10 and 13045.18 are rounded transcriptions of the real
// constants (ratio 1.000000) — cosmetic, but they should come from the binary.
func TestKnownDeviationsFromCapture(t *testing.T) {
	deviations := []struct {
		name     string
		got      func() uint32
		current  uint32
		captured uint32
	}{
		{"NGThreshold(-60dB)", func() uint32 { return le32(EncodeNGThreshold(-60)) }, 0x0020C49B, 0x00259F68},
		{"NGRange(-100dB)", func() uint32 { return le32(EncodeNGRange(-100)) }, 0x000053E2, 0x0000699B},
		{"NGHysteresis(0%)", func() uint32 { return le32(EncodeNGHysteresis(0)) }, 0x712A1999, 0x712A1880},
		{"NGHysteresis(100%)", func() uint32 { return le32(EncodeNGHysteresis(100)) }, 0x32F52E14, 0x32F52D00},
	}

	for _, d := range deviations {
		got := d.got()
		switch got {
		case d.current:
			// Still deviating as documented.
		case d.captured:
			t.Errorf("%s now matches the capture (%#08x) — good, but move it into "+
				"capturedVectors() and update docs/re/04-open-questions.md", d.name, got)
		default:
			t.Errorf("%s = %#08x, expected either the documented current value %#08x "+
				"or the captured device value %#08x", d.name, got, d.current, d.captured)
		}
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

// TestIndexRoundingDeviatesFromCapture is the evidence that the LUT index
// formula rounds where RØDE Connect truncates.
//
// encode.go computes indices with math.Round. Comparing against the defaults
// observed in the Phase 1 INIT capture:
//
//	Parameter        UI default  raw index  Round  trunc  captured
//	AE Harmonics          49 %     124.950    125    124       124
//	AE Tune             3516 Hz    168.996    169    168       168
//	BB Tune              131 Hz     71.845     72     71        71
//	BB Drive              62 %     158.100    158    158       158
//
// Truncation matches all four; rounding matches only BB Drive, where the two
// agree anyway. That is what an ordinary C float-to-int cast (CVTTSS2SI) does,
// so the fix is to replace math.Round in clampIndex with truncation — affecting
// EncodeCompRatio, EncodeAEHarmonics, EncodeAETune, EncodeBBDrive and
// EncodeBBTune.
//
// It is not applied yet. Two of the four UI defaults above (AE Tune, BB Tune)
// were themselves back-derived from the captured index in the original
// analysis, so they cannot independently confirm the hypothesis; only AE
// Harmonics, whose 49 % is a value the UI displays, is fully independent.
// Phase 3.3 settles it by reading the cast out of the disassembly. This test
// asserts today's behaviour so that change is deliberate.
func TestIndexRoundingDeviatesFromCapture(t *testing.T) {
	deviations := []struct {
		name     string
		got      byte
		current  byte
		captured byte
	}{
		{"AEHarmonics(49%)", EncodeAEHarmonics(49)[4], 0x7D, 0x7C},
		{"AETune(3516Hz)", EncodeAETune(3516)[8], 0xA9, 0xA8},
		{"BBTune(131Hz)", EncodeBBTune(131)[0], 0x48, 0x47},
	}

	for _, d := range deviations {
		switch d.got {
		case d.current:
			// Still rounding, as documented.
		case d.captured:
			t.Errorf("%s index is now %#02x, matching the capture — move this into "+
				"capturedVectors() and update docs/re/04-open-questions.md", d.name, d.got)
		default:
			t.Errorf("%s index = %#02x, expected the documented current value %#02x "+
				"or the captured value %#02x", d.name, d.got, d.current, d.captured)
		}
	}

	// BB Drive is where rounding and truncation agree, so it already matches the
	// capture and must keep doing so under either rule.
	if got, want := EncodeBBDrive(62)[4], byte(0x9E); got != want {
		t.Errorf("BBDrive(62%%) index = %#02x, want %#02x", got, want)
	}
	if got, want := le32(EncodeBBDrive(62)), uint32(0x158C2600); got != want {
		t.Errorf("BBDrive(62%%) LUT = %#08x, want %#08x", got, want)
	}
}
