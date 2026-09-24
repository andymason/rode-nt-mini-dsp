package dsp

import (
	"testing"

	"rode-dsp/internal/protocol"
)

// TestEffectDefKeyMatchesID guards against a copy-paste error in the Effects
// map literal, where an entry is filed under one key but carries another ID.
func TestEffectDefKeyMatchesID(t *testing.T) {
	for key, eff := range Effects {
		if eff.ID != key {
			t.Errorf("effect %q: filed under key %#02x but ID is %#02x", eff.Name, key, eff.ID)
		}
	}
}

func TestParamDefsAreWellFormed(t *testing.T) {
	for _, eff := range Effects {
		seen := make(map[byte]string, len(eff.Params))

		for _, p := range eff.Params {
			if p.EffectID != eff.ID {
				t.Errorf("%s/%s: EffectID is %#02x, want %#02x", eff.Name, p.Name, p.EffectID, eff.ID)
			}

			// Param 0x00 is the enable flag; it is sent via BuildEnablePacket and
			// must never appear in the registry.
			if p.ParamID == 0x00 {
				t.Errorf("%s/%s: ParamID 0x00 is reserved for the enable flag", eff.Name, p.Name)
			}

			if prev, dup := seen[p.ParamID]; dup {
				t.Errorf("%s: ParamID %#02x used by both %q and %q", eff.Name, p.ParamID, prev, p.Name)
			}
			seen[p.ParamID] = p.Name

			if p.UIMin >= p.UIMax {
				t.Errorf("%s/%s: UIMin %v is not below UIMax %v", eff.Name, p.Name, p.UIMin, p.UIMax)
			}
			if p.Default < p.UIMin || p.Default > p.UIMax {
				t.Errorf("%s/%s: Default %v outside [%v, %v]", eff.Name, p.Name, p.Default, p.UIMin, p.UIMax)
			}
			if p.Resolution <= 0 {
				t.Errorf("%s/%s: Resolution %v must be positive", eff.Name, p.Name, p.Resolution)
			}
			if p.EncodeFn == nil {
				t.Errorf("%s/%s: EncodeFn is nil", eff.Name, p.Name)
			}
			if p.FormatFn == nil {
				t.Errorf("%s/%s: FormatFn is nil", eff.Name, p.Name)
			}
		}
	}
}

// TestEncodersAreTotal checks that every encoder returns a non-empty payload
// that fits the packet across its whole declared UI range, including values
// outside it. Encoders clamp rather than error, so an out-of-range input must
// still produce a well-formed payload.
func TestEncodersAreTotal(t *testing.T) {
	const maxPayload = protocol.PacketSize - 4 // report ID, effect, cmd, param

	for _, eff := range Effects {
		for _, p := range eff.Params {
			span := p.UIMax - p.UIMin
			values := []float64{
				p.UIMin - span, // far below range
				p.UIMin,
				p.Default,
				p.UIMin + span/3,
				p.UIMin + 2*span/3,
				p.UIMax,
				p.UIMax + span, // far above range
			}

			for _, v := range values {
				got := p.EncodeFn(v)
				if len(got) == 0 {
					t.Errorf("%s/%s: EncodeFn(%v) returned an empty payload", eff.Name, p.Name, v)
				}
				if len(got) > maxPayload {
					t.Errorf("%s/%s: EncodeFn(%v) returned %d bytes, exceeds %d-byte payload",
						eff.Name, p.Name, v, len(got), maxPayload)
				}
			}
		}
	}
}

// TestIndexedEncodersClampOutOfRange covers the parameters that select a LUT
// index. Their index is a hard 0-255 and a percentage/frequency outside the UI
// range is meaningless, so out-of-range input must pin to the endpoints.
//
// The dB-domain noise gate parameters are deliberately excluded: they are
// formula-based and extrapolate smoothly, and whether RØDE Connect clamps them
// at the UI bounds is still open (see docs/re/open-questions.md).
func TestIndexedEncodersClampOutOfRange(t *testing.T) {
	indexed := map[byte]map[byte]bool{
		protocol.EffComp: {0x02: true},             // Ratio
		protocol.EffAE:   {0x01: true, 0x02: true}, // Harmonics, Tune
		protocol.EffBB:   {0x01: true, 0x02: true}, // Drive, Tune
	}

	for _, eff := range Effects {
		for _, p := range eff.Params {
			if !indexed[eff.ID][p.ParamID] {
				continue
			}
			span := p.UIMax - p.UIMin

			if got, want := p.EncodeFn(p.UIMin-span), p.EncodeFn(p.UIMin); !bytesEqual(got, want) {
				t.Errorf("%s/%s: below-range encodes to % x, want % x (same as UIMin)",
					eff.Name, p.Name, got, want)
			}
			if got, want := p.EncodeFn(p.UIMax+span), p.EncodeFn(p.UIMax); !bytesEqual(got, want) {
				t.Errorf("%s/%s: above-range encodes to % x, want % x (same as UIMax)",
					eff.Name, p.Name, got, want)
			}
		}
	}
}

// TestLogScaledEncodersSurviveNonPositiveInput is a regression test for a NaN
// path. The compressor attack and release encoders take log(ms/min); before the
// input was clamped, a negative ms produced NaN, which passed straight through
// the fraction bounds checks and the LUT interpolation to emit a garbage
// coefficient. This is reachable from a hand-edited config file, because
// DSPState.UnmarshalJSON does not range-check what it loads.
func TestLogScaledEncodersSurviveNonPositiveInput(t *testing.T) {
	cases := []struct {
		name string
		fn   func(float64) []byte
		min  float64
	}{
		{"CompAttack", protocol.EncodeCompAttack, 0.1},
		{"CompRelease", protocol.EncodeCompRelease, 5.0},
	}

	for _, tc := range cases {
		want := tc.fn(tc.min)
		for _, ms := range []float64{0.0, -1.0, -1000.0} {
			if got := tc.fn(ms); !bytesEqual(got, want) {
				t.Errorf("%s(%v) = % x, want % x (same as UIMin %v)", tc.name, ms, got, want, tc.min)
			}
		}
	}
}

// TestParamCountsCoverRegistry checks protocol.ParamCounts against the registry.
// A startup read covers param 0x00 (enable) plus every registered param, and may
// additionally cover parameters the UI never exposes.
//
// The only surplus is Aural Exciter, whose count of 4 covers params 0x00-0x03
// while the UI exposes only 0x01-0x02. Param 0x03 is read at startup but never
// SET, and the device answers a read of Big Bottom's param 0x03 too even though
// RØDE Connect does not ask. See Q6 in docs/re/open-questions.md.
func TestParamCountsCoverRegistry(t *testing.T) {
	hidden := map[byte]int{
		protocol.EffAE: 1, // param 0x03, read-only
	}

	for id, eff := range Effects {
		count, ok := protocol.ParamCounts[id]
		if !ok {
			t.Errorf("%s: no ParamCounts entry for effect %#02x", eff.Name, id)
			continue
		}

		want := len(eff.Params) + 1 + hidden[id]
		if count != want {
			t.Errorf("%s: ParamCounts is %d, want %d (%d registered params + enable + %d hidden)",
				eff.Name, count, want, len(eff.Params), hidden[id])
		}
	}

	for id := range protocol.ParamCounts {
		if _, ok := Effects[id]; !ok {
			t.Errorf("ParamCounts has effect %#02x with no registry entry", id)
		}
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
