package dsp

import (
	"path/filepath"
	"testing"

	"rode-dsp/internal/protocol"
)

// Built-in presets are constructed with SetParam, which panics via state() if a
// value is out of the parameter's range. This test is what catches an edit to
// the numbers that the compiler cannot.
func TestBuiltinPresetsAreValid(t *testing.T) {
	presets := BuiltinPresets()
	if len(presets) == 0 {
		t.Fatal("no built-in presets")
	}

	for _, p := range presets {
		if p.Name == "" || !p.Builtin || p.State == nil {
			t.Fatalf("preset %+v is not a usable built-in", p)
		}
		for effID, params := range p.State.GetAllParams() {
			def, ok := Effects[effID]
			if !ok {
				t.Fatalf("%s: unknown effect 0x%02x", p.Name, effID)
			}
			for _, pd := range def.Params {
				v := params[pd.ParamID]
				if v < pd.UIMin || v > pd.UIMax {
					t.Errorf("%s: %s %s = %v, outside [%v, %v]",
						p.Name, def.Name, pd.Name, v, pd.UIMin, pd.UIMax)
				}
			}
		}
	}
}

func TestRadioVoiceEnablesEverything(t *testing.T) {
	p, err := FindPreset(filepath.Join(t.TempDir(), "presets.json"), "radio voice")
	if err != nil {
		t.Fatalf("FindPreset: %v", err)
	}
	for _, effID := range []byte{protocol.EffComp, protocol.EffGate, protocol.EffAE, protocol.EffBB} {
		if !p.State.IsEnabled(effID) {
			t.Errorf("effect 0x%02x is not enabled in %q", effID, p.Name)
		}
	}
}

func TestUserPresetRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.json")

	// A missing file is an empty list, not an error.
	if got, err := LoadUserPresets(path); err != nil || len(got) != 0 {
		t.Fatalf("LoadUserPresets on missing file = %v, %v", got, err)
	}

	s := NewDSPState()
	s.SetEnabled(protocol.EffComp, true)
	if err := s.SetParam(protocol.EffComp, 0x01, -33.0); err != nil {
		t.Fatal(err)
	}

	if err := SaveUserPreset(path, "Booth", "", s); err != nil {
		t.Fatalf("SaveUserPreset: %v", err)
	}

	got, err := FindPreset(path, "booth")
	if err != nil {
		t.Fatalf("FindPreset: %v", err)
	}
	if got.Builtin {
		t.Error("a saved preset should not be marked built-in")
	}
	if v, _ := got.State.GetParam(protocol.EffComp, 0x01); v != -33.0 {
		t.Errorf("threshold = %v, want -33", v)
	}

	// Saving the same name again replaces rather than duplicates.
	if err := SaveUserPreset(path, "Booth", "", NewDSPState()); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	list, err := LoadUserPresets(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("after re-save there are %d presets, want 1", len(list))
	}

	if err := DeleteUserPreset(path, "Booth"); err != nil {
		t.Fatalf("DeleteUserPreset: %v", err)
	}
	if list, _ := LoadUserPresets(path); len(list) != 0 {
		t.Fatalf("after delete there are %d presets, want 0", len(list))
	}
	if err := DeleteUserPreset(path, "Booth"); err == nil {
		t.Error("deleting a missing preset should report an error")
	}
}

// A user preset must not be able to take a built-in's name: the built-in wins
// at load time, so the save would be silently lost.
func TestBuiltinNamesAreProtected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.json")
	if err := SaveUserPreset(path, "radio VOICE", "", NewDSPState()); err == nil {
		t.Error("saving over a built-in name should be refused")
	}
	if err := DeleteUserPreset(path, "Radio Voice"); err == nil {
		t.Error("deleting a built-in should be refused")
	}
	if err := SaveUserPreset(path, "  ", "", NewDSPState()); err == nil {
		t.Error("an empty name should be refused")
	}
}

func TestCopyIntoReplacesEveryEffect(t *testing.T) {
	dst := NewDSPState()
	dst.SetEnabled(protocol.EffBB, true)

	// A preset with Big Bottom off has to turn it off, not merely leave it.
	src := NewDSPState()
	src.SetEnabled(protocol.EffComp, true)
	p := Preset{Name: "x", State: src}

	if err := p.CopyInto(dst); err != nil {
		t.Fatalf("CopyInto: %v", err)
	}
	if dst.IsEnabled(protocol.EffBB) {
		t.Error("Big Bottom should have been switched off")
	}
	if !dst.IsEnabled(protocol.EffComp) {
		t.Error("Compressor should have been switched on")
	}
}
