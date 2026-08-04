package dsp

import (
	"encoding/json"
	"math"
	"testing"

	"rode-dsp/internal/protocol"
)

func TestNewDSPStateUsesDefaults(t *testing.T) {
	s := NewDSPState()

	for effID, eff := range Effects {
		if s.IsEnabled(effID) {
			t.Errorf("%s: enabled by default, want disabled", eff.Name)
		}
		for _, p := range eff.Params {
			got, ok := s.GetParam(effID, p.ParamID)
			if !ok {
				t.Errorf("%s/%s: missing from a fresh state", eff.Name, p.Name)
				continue
			}
			if got != p.Default {
				t.Errorf("%s/%s: default is %v, want %v", eff.Name, p.Name, got, p.Default)
			}
		}
	}
}

func TestSetParamRejectsOutOfRange(t *testing.T) {
	s := NewDSPState()

	for effID, eff := range Effects {
		for _, p := range eff.Params {
			span := p.UIMax - p.UIMin
			for _, v := range []float64{p.UIMin - span, p.UIMax + span} {
				if err := s.SetParam(effID, p.ParamID, v); err == nil {
					t.Errorf("%s/%s: SetParam(%v) succeeded, want out-of-range error",
						eff.Name, p.Name, v)
				}
			}
		}
	}
}

func TestSetParamRejectsUnknownIDs(t *testing.T) {
	s := NewDSPState()

	if err := s.SetParam(0xFF, 0x01, 0); err == nil {
		t.Error("SetParam with unknown effect ID succeeded, want error")
	}
	if err := s.SetParam(protocol.EffComp, 0xFF, 0); err == nil {
		t.Error("SetParam with unknown param ID succeeded, want error")
	}
}

func TestSetParamSnapsToResolution(t *testing.T) {
	s := NewDSPState()

	// Compressor threshold: -60..0 dB at 0.5 dB resolution.
	if err := s.SetParam(protocol.EffComp, 0x01, -20.3); err != nil {
		t.Fatalf("SetParam: %v", err)
	}
	got, _ := s.GetParam(protocol.EffComp, 0x01)
	if got != -20.5 {
		t.Errorf("threshold snapped to %v, want -20.5", got)
	}

	// Snapping must never push a value outside the declared range.
	for effID, eff := range Effects {
		for _, p := range eff.Params {
			for _, v := range []float64{p.UIMin, p.UIMax} {
				if err := s.SetParam(effID, p.ParamID, v); err != nil {
					t.Errorf("%s/%s: SetParam(%v): %v", eff.Name, p.Name, v, err)
					continue
				}
				got, _ := s.GetParam(effID, p.ParamID)
				if got < p.UIMin || got > p.UIMax {
					t.Errorf("%s/%s: SetParam(%v) snapped to %v, outside [%v, %v]",
						eff.Name, p.Name, v, got, p.UIMin, p.UIMax)
				}
			}
		}
	}
}

func TestStateJSONRoundTrip(t *testing.T) {
	orig := NewDSPState()
	orig.SetEnabled(protocol.EffComp, true)
	orig.SetEnabled(protocol.EffGate, true)
	if err := orig.SetParam(protocol.EffComp, 0x01, -25.0); err != nil {
		t.Fatalf("SetParam: %v", err)
	}
	if err := orig.SetParam(protocol.EffAE, 0x02, 1600.0); err != nil {
		t.Fatalf("SetParam: %v", err)
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var round DSPState
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	for effID, eff := range Effects {
		if got, want := round.IsEnabled(effID), orig.IsEnabled(effID); got != want {
			t.Errorf("%s: enabled round-tripped to %v, want %v", eff.Name, got, want)
		}
		for _, p := range eff.Params {
			want, _ := orig.GetParam(effID, p.ParamID)
			got, ok := round.GetParam(effID, p.ParamID)
			if !ok {
				t.Errorf("%s/%s: missing after round trip", eff.Name, p.Name)
				continue
			}
			if got != want {
				t.Errorf("%s/%s: round-tripped to %v, want %v", eff.Name, p.Name, got, want)
			}
		}
	}
}

// TestUnmarshalClampsOutOfRange covers the config-load path, which does not go
// through SetParam. A hand-edited config previously delivered its values
// straight to the encoders; a negative compressor attack reached math.Log and
// produced a NaN-derived coefficient.
func TestUnmarshalClampsOutOfRange(t *testing.T) {
	const cfg = `{
	  "compressor": {"enabled": true, "attack": -5.0, "threshold": 999.0},
	  "noise_gate": {"enabled": false, "hysteresis": -300.0}
	}`

	var s DSPState
	if err := json.Unmarshal([]byte(cfg), &s); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	for effID, eff := range Effects {
		for _, p := range eff.Params {
			got, ok := s.GetParam(effID, p.ParamID)
			if !ok {
				t.Errorf("%s/%s: missing after load", eff.Name, p.Name)
				continue
			}
			if math.IsNaN(got) || got < p.UIMin || got > p.UIMax {
				t.Errorf("%s/%s: loaded %v, outside [%v, %v]",
					eff.Name, p.Name, got, p.UIMin, p.UIMax)
			}
		}
	}

	// Absent effects fall back to defaults rather than zero values.
	drive, _ := s.GetParam(protocol.EffBB, 0x01)
	if drive != 62.0 {
		t.Errorf("Big Bottom drive loaded as %v, want the 62.0 default", drive)
	}
}

func TestUnmarshalUnknownFieldsAreIgnored(t *testing.T) {
	const cfg = `{"compressor": {"enabled": true, "nonexistent": 1.0}, "not_an_effect": {}}`

	var s DSPState
	if err := json.Unmarshal([]byte(cfg), &s); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !s.IsEnabled(protocol.EffComp) {
		t.Error("compressor should still be enabled alongside unknown fields")
	}
}

func TestCloneIsDeep(t *testing.T) {
	orig := NewDSPState()
	orig.SetEnabled(protocol.EffComp, true)
	if err := orig.SetParam(protocol.EffComp, 0x01, -30.0); err != nil {
		t.Fatalf("SetParam: %v", err)
	}

	clone := orig.Clone()
	if err := clone.SetParam(protocol.EffComp, 0x01, -10.0); err != nil {
		t.Fatalf("SetParam: %v", err)
	}
	clone.SetEnabled(protocol.EffComp, false)

	if got, _ := orig.GetParam(protocol.EffComp, 0x01); got != -30.0 {
		t.Errorf("mutating the clone changed the original param to %v", got)
	}
	if !orig.IsEnabled(protocol.EffComp) {
		t.Error("mutating the clone changed the original enable flag")
	}
}
