package protocol

import (
	"math"
	"testing"
)

// TestParseResponse pins the reply layout: report ID, echoed effect ID, ACK,
// then data.
func TestParseResponse(t *testing.T) {
	raw := []byte{0x03, 0x02, 0x41, 0xDE, 0xAD, 0xBE, 0xEF}

	eff, data, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if eff != EffAE {
		t.Errorf("effect = %#02x, want %#02x", eff, EffAE)
	}
	if len(data) != 4 || data[0] != 0xDE || data[3] != 0xEF {
		t.Errorf("data = % 02x, want de ad be ef", data)
	}

	if _, _, err := ParseResponse([]byte{0x03, 0x02}); err != ErrShortResponse {
		t.Errorf("short reply error = %v, want ErrShortResponse", err)
	}
	if _, _, err := ParseResponse([]byte{0x03, 0x02, 0x00, 0x00}); err != ErrNotAcknowledged {
		t.Errorf("unacked reply error = %v, want ErrNotAcknowledged", err)
	}
}

// TestDecodeRoundTrip runs every table-backed and formula parameter through its
// encoder and back, and requires the result to land within one index step.
//
// Exactness is not achievable and not wanted: the device stores 256 quantised
// steps, so several UI values share an index and decoding returns that index's
// representative value. The tolerance below is the width of one step.
func TestDecodeRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		values   []float64
		encode   func(float64) []byte
		decode   func([]byte) (float64, bool)
		tol      float64
		relative bool
	}{
		{
			name:   "comp threshold",
			values: []float64{-60, -45.5, -21.5, -10, 0},
			encode: EncodeCompThreshold,
			decode: DecodeCompThreshold,
			tol:    60.0 / 255.0,
		},
		{
			name:   "comp ratio",
			values: []float64{1.5, 2.0, 3.0, 4.0, 4.5},
			encode: EncodeCompRatio,
			decode: DecodeCompRatio,
			tol:    3.0 / 255.0,
		},
		{
			name:     "comp attack",
			values:   []float64{0.1, 0.3, 1.0, 5.0, 10.0},
			encode:   EncodeCompAttack,
			decode:   DecodeCompAttack,
			tol:      0.02, // one index step is ~1.8% on a log scale
			relative: true,
		},
		{
			name:     "comp release",
			values:   []float64{5.0, 34.5, 100.0, 200.0},
			encode:   EncodeCompRelease,
			decode:   DecodeCompRelease,
			tol:      0.02,
			relative: true,
		},
		{
			name:   "comp gain",
			values: []float64{0, 3, 6, 9},
			encode: EncodeCompGain,
			decode: DecodeCompGain,
			tol:    9.0 / 255.0,
		},
		{
			name:   "AE harmonics",
			values: []float64{0, 25, 49, 92.5, 100},
			encode: EncodeAEHarmonics,
			decode: DecodeAEHarmonics,
			tol:    100.0 / 255.0,
		},
		{
			name:   "AE tune",
			values: []float64{600, 2809, 3516, 5000},
			encode: EncodeAETune,
			decode: DecodeAETune,
			tol:    4400.0 / 255.0,
		},
		{
			name:   "BB drive",
			values: []float64{0, 62, 89.5, 100},
			encode: EncodeBBDrive,
			decode: DecodeBBDrive,
			tol:    100.0 / 255.0,
		},
		{
			name:   "BB tune",
			values: []float64{60, 131, 200, 312},
			encode: EncodeBBTune,
			decode: DecodeBBTune,
			tol:    252.0 / 255.0,
		},
		{
			name:   "NG threshold",
			values: []float64{-60, -31, -12, 0},
			encode: EncodeNGThreshold,
			decode: DecodeNGThreshold,
			tol:    0.01,
		},
		{
			name:     "NG attack",
			values:   []float64{0.5, 30.6, 200, 1000},
			encode:   EncodeNGAttack,
			decode:   DecodeNGAttack,
			tol:      0.01,
			relative: true,
		},
		{
			name:     "NG hold",
			values:   []float64{50, 374, 1000, 2000},
			encode:   EncodeNGHold,
			decode:   DecodeNGHold,
			tol:      0.01,
			relative: true,
		},
		{
			name:     "NG release",
			values:   []float64{50, 209, 2000},
			encode:   EncodeNGRelease,
			decode:   DecodeNGRelease,
			tol:      0.01,
			relative: true,
		},
		{
			name:   "NG range",
			values: []float64{-100, -42, -10, 0},
			encode: EncodeNGRange,
			decode: DecodeNGRange,
			tol:    0.05,
		},
		{
			name:   "NG hysteresis",
			values: []float64{0, 42, 75, 100},
			encode: EncodeNGHysteresis,
			decode: DecodeNGHysteresis,
			tol:    0.05,
		},
	}

	for _, tc := range cases {
		for _, want := range tc.values {
			got, ok := tc.decode(tc.encode(want))
			if !ok {
				t.Errorf("%s: decode of %.2f failed", tc.name, want)
				continue
			}

			diff := math.Abs(got - want)
			limit := tc.tol
			if tc.relative {
				limit = tc.tol * math.Abs(want)
			}
			if diff > limit {
				t.Errorf("%s: encode(%.4f) decoded to %.4f, off by %.4f (limit %.4f)",
					tc.name, want, got, diff, limit)
			}
		}
	}
}

// TestDecodeCaptureValues decodes coefficients taken verbatim from
// rode-nt-mini-usb-official-software-all-dsp-enabled-usb-traffic.pcapng, where
// RØDE Connect read them off the device and then wrote the identical bytes
// back. These are real device replies, not values this package produced.
func TestDecodeCaptureValues(t *testing.T) {
	// Frame 37: compressor threshold reply, data 00 37 07 00.
	if db, ok := DecodeCompThreshold([]byte{0x00, 0x37, 0x07, 0x00}); !ok {
		t.Error("comp threshold decode failed")
	} else if db < -60 || db > 0 {
		t.Errorf("comp threshold = %.2f dB, outside the -60..0 range", db)
	}

	// Frame 41: compressor ratio reply, a bare index of 0x7f.
	ratio, ok := DecodeCompRatio([]byte{0x7f})
	if !ok {
		t.Fatal("comp ratio decode failed")
	}
	if want := 1.5 + 127.0/255.0*3.0; math.Abs(ratio-want) > 1e-6 {
		t.Errorf("comp ratio = %.4f, want %.4f", ratio, want)
	}

	// AE tune reply observed live: LUT1 0x0B000000, LUT2 0x24000000, index 0,
	// which is the 600 Hz endpoint.
	data := []byte{0x00, 0x00, 0x00, 0x0b, 0x00, 0x00, 0x00, 0x24, 0x00}
	hz, ok := DecodeAETune(data)
	if !ok {
		t.Fatal("AE tune decode failed")
	}
	if math.Abs(hz-600.0) > 0.001 {
		t.Errorf("AE tune = %.2f Hz, want 600", hz)
	}
}

// TestDecodeShortData checks that every decoder refuses a truncated reply
// rather than reading past it. A disconnect mid-transfer is the realistic way
// to get one.
func TestDecodeShortData(t *testing.T) {
	decoders := map[string]func([]byte) (float64, bool){
		"comp threshold": DecodeCompThreshold,
		"comp attack":    DecodeCompAttack,
		"comp release":   DecodeCompRelease,
		"comp gain":      DecodeCompGain,
		"AE harmonics":   DecodeAEHarmonics,
		"AE tune":        DecodeAETune,
		"BB drive":       DecodeBBDrive,
		"NG threshold":   DecodeNGThreshold,
		"NG attack":      DecodeNGAttack,
		"NG hold":        DecodeNGHold,
		"NG range":       DecodeNGRange,
		"NG hysteresis":  DecodeNGHysteresis,
	}

	for name, decode := range decoders {
		if _, ok := decode(nil); ok {
			t.Errorf("%s: decoded an empty reply", name)
		}
		if _, ok := decode([]byte{0x01, 0x02}); ok {
			t.Errorf("%s: decoded a 2-byte reply", name)
		}
	}
}
