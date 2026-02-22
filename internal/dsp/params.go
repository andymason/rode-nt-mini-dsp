package dsp

import (
	"fmt"
	"rode-dsp/internal/protocol"
)

// ParamDef defines a DSP parameter
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f dB", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f:1", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.2f ms", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f ms", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f dB", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f dB", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.2f ms", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f ms", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f ms", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f dB", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.0f%%", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f%%", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.0f Hz", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.1f%%", v) },
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
				FormatFn: func(v float64) string { return fmt.Sprintf("%.0f Hz", v) },
			},
		},
	},
}
