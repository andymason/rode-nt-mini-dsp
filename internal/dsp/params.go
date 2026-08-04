package dsp

import (
	"fmt"
	"rode-dsp/internal/protocol"
)

// ParamDef defines a DSP parameter.
//
// The last four fields exist for the GUI's advanced mode: they describe how a
// UI value becomes wire bytes, so the interface can explain the encoding
// rather than restate it. IndexFn is the same function the encoder calls, so
// the two cannot drift apart. See docs/re/03-encoders.md.
type ParamDef struct {
	EffectID   byte
	ParamID    byte
	Name       string
	Unit       string
	UIMin      float64
	UIMax      float64
	Default    float64
	Resolution float64
	EncodeFn   func(float64) []byte
	FormatFn   func(float64) string

	// Scale is how the value is distributed across the control, "linear" or
	// "log". It mirrors the encoder, not a display preference.
	Scale string
	// Table names the embedded lookup table, empty when none is needed.
	Table string
	// Formula is the index or coefficient expression, as read from the binary.
	Formula string
	// IndexFn returns the 0-255 table index, or nil when the parameter is not
	// table-indexed (the noise gate computes coefficients directly).
	IndexFn func(float64) int
}

// EffectDef defines a DSP effect
type EffectDef struct {
	ID     byte
	Name   string
	Params []ParamDef
}

// Effects registry
var Effects = map[byte]EffectDef{
	protocol.EffComp: {
		ID:   protocol.EffComp,
		Name: "Compressor",
		Params: []ParamDef{
			{
				EffectID:   protocol.EffComp,
				ParamID:    0x01,
				Name:       "Threshold",
				Unit:       "dB",
				UIMin:      -60.0,
				UIMax:      0.0,
				Default:    -20.0,
				Resolution: 0.5,
				EncodeFn:   protocol.EncodeCompThreshold,
				Scale:      "linear",
				Table:      "comp_threshold",
				Formula:    "idx = trunc((1 - (dB + 60) / 60) * 255)",
				IndexFn:    protocol.CompThresholdIndex,
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f dB", v) },
			},
			{
				EffectID:   protocol.EffComp,
				ParamID:    0x02,
				Name:       "Ratio",
				Unit:       ":1",
				UIMin:      1.5,
				UIMax:      4.5,
				Default:    3.0,
				Resolution: 0.1,
				EncodeFn:   protocol.EncodeCompRatio,
				Scale:      "linear",
				Formula:    "payload = trunc((ratio - 1.5) / 3 * 255)",
				IndexFn:    protocol.CompRatioIndex,
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f:1", v) },
			},
			{
				EffectID:   protocol.EffComp,
				ParamID:    0x03,
				Name:       "Attack",
				Unit:       "ms",
				UIMin:      0.1,
				UIMax:      10.0,
				Default:    0.7,
				Resolution: 0.1,
				EncodeFn:   protocol.EncodeCompAttack,
				Scale:      "log",
				Table:      "comp_attack",
				Formula:    "idx = trunc(log(ms / 0.1) / log(100) * 255)",
				IndexFn:    protocol.CompAttackIndex,
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.2f ms", v) },
			},
			{
				EffectID:   protocol.EffComp,
				ParamID:    0x04,
				Name:       "Release",
				Unit:       "ms",
				UIMin:      5.0,
				UIMax:      200.0,
				Default:    21.0,
				Resolution: 0.5,
				EncodeFn:   protocol.EncodeCompRelease,
				Scale:      "log",
				Table:      "comp_release",
				Formula:    "idx = trunc(log(ms / 5) / log(40) * 255)",
				IndexFn:    protocol.CompReleaseIndex,
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f ms", v) },
			},
			{
				EffectID:   protocol.EffComp,
				ParamID:    0x05,
				Name:       "Gain",
				Unit:       "dB",
				UIMin:      0.0,
				UIMax:      9.0,
				Default:    2.0,
				Resolution: 0.1,
				EncodeFn:   protocol.EncodeCompGain,
				Scale:      "linear",
				Table:      "comp_gain",
				Formula:    "idx = trunc(dB / 9 * 255)",
				IndexFn:    protocol.CompGainIndex,
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f dB", v) },
			},
		},
	},
	protocol.EffGate: {
		ID:   protocol.EffGate,
		Name: "Noise Gate",
		Params: []ParamDef{
			{
				EffectID:   protocol.EffGate,
				ParamID:    0x01,
				Name:       "Threshold",
				Unit:       "dB",
				UIMin:      -60.0,
				UIMax:      0.0,
				Default:    -42.0,
				Resolution: 0.5,
				EncodeFn:   protocol.EncodeNGThreshold,
				Scale:      "linear",
				Formula:    "Q31 = sat(trunc(10^(dB/20) x 2^31))",
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f dB", v) },
			},
			{
				EffectID:   protocol.EffGate,
				ParamID:    0x02,
				Name:       "Attack",
				Unit:       "ms",
				UIMin:      0.1,
				UIMax:      1000.0,
				Default:    0.8,
				Resolution: 0.1,
				EncodeFn:   protocol.EncodeNGAttack,
				Scale:      "log",
				Formula:    "w = 5/(s x 48000); Q31 = sat(sqrt(c^2 - 4c + 3) + c - 1), c = cos w",
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.2f ms", v) },
			},
			{
				EffectID:   protocol.EffGate,
				ParamID:    0x03,
				Name:       "Hold",
				Unit:       "ms",
				UIMin:      50.0,
				UIMax:      2000.0,
				Default:    80.0,
				Resolution: 1.0,
				EncodeFn:   protocol.EncodeNGHold,
				Scale:      "log",
				Formula:    "Q31 = sat(trunc(1 / (s x 48000) x 2^31))",
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f ms", v) },
			},
			{
				EffectID:   protocol.EffGate,
				ParamID:    0x04,
				Name:       "Release",
				Unit:       "ms",
				UIMin:      50.0,
				UIMax:      2000.0,
				Default:    210.0,
				Resolution: 1.0,
				EncodeFn:   protocol.EncodeNGRelease,
				Scale:      "log",
				Formula:    "Q31 = sat(trunc(1 / (s x 48000) x 2^31))",
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f ms", v) },
			},
			{
				EffectID:   protocol.EffGate,
				ParamID:    0x05,
				Name:       "Range",
				Unit:       "dB",
				UIMin:      -100.0,
				UIMax:      0.0,
				Default:    -9.0,
				Resolution: 0.5,
				EncodeFn:   protocol.EncodeNGRange,
				Scale:      "linear",
				Formula:    "Q31 = sat(trunc(10^(dB/20) x 2^31))",
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f dB", v) },
			},
			{
				EffectID:   protocol.EffGate,
				ParamID:    0x06,
				Name:       "Hysteresis",
				Unit:       "%",
				UIMin:      0.0,
				UIMax:      100.0,
				Default:    50.0,
				Resolution: 1.0,
				EncodeFn:   protocol.EncodeNGHysteresis,
				Scale:      "linear",
				Formula:    "h = pct/100; Q31 = sat(trunc(10^((-1 - 7h)/20) x 2^31))",
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.0f%%", v) },
			},
		},
	},
	protocol.EffAE: {
		ID:   protocol.EffAE,
		Name: "Aural Exciter",
		Params: []ParamDef{
			{
				EffectID:   protocol.EffAE,
				ParamID:    0x01,
				Name:       "Harmonics",
				Unit:       "%",
				UIMin:      0.0,
				UIMax:      100.0,
				Default:    49.0,
				Resolution: 0.5,
				EncodeFn:   protocol.EncodeAEHarmonics,
				Scale:      "linear",
				Table:      "harmonics_drive",
				Formula:    "idx = trunc(pct / 100 * 255)",
				IndexFn:    protocol.HarmonicsDriveIndex,
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f%%", v) },
			},
			{
				EffectID:   protocol.EffAE,
				ParamID:    0x02,
				Name:       "Tune",
				Unit:       "Hz",
				UIMin:      600.0,
				UIMax:      5000.0,
				Default:    3516.0,
				Resolution: 1.0,
				EncodeFn:   protocol.EncodeAETune,
				Scale:      "linear",
				Table:      "ae_tune_1 + ae_tune_2",
				Formula:    "idx = trunc((Hz - 600) / 4400 * 255)",
				IndexFn:    protocol.AETuneIndex,
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.0f Hz", v) },
			},
		},
	},
	protocol.EffBB: {
		ID:   protocol.EffBB,
		Name: "Big Bottom",
		Params: []ParamDef{
			{
				EffectID:   protocol.EffBB,
				ParamID:    0x01,
				Name:       "Drive",
				Unit:       "%",
				UIMin:      0.0,
				UIMax:      100.0,
				Default:    62.0,
				Resolution: 0.5,
				EncodeFn:   protocol.EncodeBBDrive,
				Scale:      "linear",
				Table:      "harmonics_drive",
				Formula:    "idx = trunc(pct / 100 * 255)",
				IndexFn:    protocol.HarmonicsDriveIndex,
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.1f%%", v) },
			},
			{
				EffectID:   protocol.EffBB,
				ParamID:    0x02,
				Name:       "Tune",
				Unit:       "Hz",
				UIMin:      60.0,
				UIMax:      312.0,
				Default:    131.0,
				Resolution: 1.0,
				EncodeFn:   protocol.EncodeBBTune,
				Scale:      "linear",
				Formula:    "payload = trunc((Hz - 60) / 252 * 255)",
				IndexFn:    protocol.BBTuneIndex,
				FormatFn:   func(v float64) string { return fmt.Sprintf("%.0f Hz", v) },
			},
		},
	},
}
