package dsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rode-dsp/internal/protocol"
)

// Preset is a complete DSP state under a name. Built-in presets ship with the
// binary and cannot be overwritten or deleted; everything else lives in the
// preset file and is the user's own.
type Preset struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Builtin     bool      `json:"builtin,omitempty"`
	State       *DSPState `json:"state"`
}

// presetFile is the on-disk document. A wrapper object rather than a bare array
// so the format has somewhere to grow.
type presetFile struct {
	Presets []Preset `json:"presets"`
}

// PresetPath returns the absolute path to the preset file. An empty path means
// the default, which sits beside the config file; see paths.go.
func PresetPath(path string) (string, error) {
	if path == "" {
		var err error
		if path, err = PresetPathFor(""); err != nil {
			return "", err
		}
	}
	return filepath.Abs(path)
}

// state builds a DSPState from a literal description of the effects that differ
// from the defaults. Parameters left out keep their registry default, which is
// what the device would use anyway.
func state(specs map[byte]struct {
	enabled bool
	params  map[byte]float64
}) *DSPState {
	s := NewDSPState()
	for effID, spec := range specs {
		s.SetEnabled(effID, spec.enabled)
		for paramID, v := range spec.params {
			// A built-in that names a parameter outside its range is a bug in
			// this file, not in the user's data, so it is worth being loud.
			if err := s.SetParam(effID, paramID, v); err != nil {
				panic(fmt.Sprintf("built-in preset: %v", err))
			}
		}
	}
	return s
}

type effectSpec = struct {
	enabled bool
	params  map[byte]float64
}

// BuiltinPresets returns the presets that ship with the tool.
//
// A fresh DSPState is built on each call so a caller that loads one and then
// edits it cannot mutate the built-in for everyone else.
func BuiltinPresets() []Preset {
	return []Preset{
		{
			Name: "Radio Voice",
			Description: "Broadcast/podcast voice: gate the room, level the delivery, " +
				"lift presence and add a little chest.",
			Builtin: true,
			State: state(map[byte]effectSpec{
				// Gate the room between phrases. -45 dB sits under normal speech
				// but over a quiet room; the range is a partial duck rather than
				// a full mute, which keeps breaths and tails sounding natural
				// instead of chopping them off.
				protocol.EffGate: {enabled: true, params: map[byte]float64{
					0x01: -45.0, // Threshold dB
					0x02: 1.0,   // Attack ms — fast enough not to clip a word onset
					0x03: 120.0, // Hold ms — rides through short gaps mid-sentence
					0x04: 250.0, // Release ms — closes gradually, no pumping
					0x05: -25.0, // Range dB — duck, don't mute
					0x06: 50.0,  // Hysteresis %
				}},
				// The levelling stage. A moderate ratio over a fairly low
				// threshold is what gives the dense, even "radio" delivery;
				// attack is slow enough to let consonants through, release long
				// enough that the gain does not breathe between syllables.
				protocol.EffComp: {enabled: true, params: map[byte]float64{
					0x01: -18.0, // Threshold dB
					0x02: 3.5,   // Ratio :1
					0x03: 4.0,   // Attack ms
					0x04: 120.0, // Release ms
					0x05: 6.0,   // Gain dB — make up what the compression took
				}},
				// Presence, not sibilance: the exciter's tune sits above the
				// harshness region so it adds air and intelligibility rather
				// than sharpening "s" sounds.
				protocol.EffAE: {enabled: true, params: map[byte]float64{
					0x01: 25.0,   // Harmonics %
					0x02: 3800.0, // Tune Hz
				}},
				// A modest low lift for warmth. Kept low in both amount and
				// corner so it stays out of the 200-300 Hz mud band.
				protocol.EffBB: {enabled: true, params: map[byte]float64{
					0x01: 30.0,  // Drive %
					0x02: 100.0, // Tune Hz
				}},
			}),
		},
	}
}

// LoadUserPresets reads the preset file. A missing file is not an error — it
// simply means no presets have been saved yet.
func LoadUserPresets(path string) ([]Preset, error) {
	presetPath, err := PresetPath(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve preset path: %w", err)
	}

	data, err := os.ReadFile(presetPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read preset file %s: %w", presetPath, err)
	}

	var doc presetFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse preset file %s: %w", presetPath, err)
	}

	// The file is hand-editable, so drop anything nameless or stateless rather
	// than serving it to the GUI as an entry that cannot be loaded.
	out := make([]Preset, 0, len(doc.Presets))
	for _, p := range doc.Presets {
		if strings.TrimSpace(p.Name) == "" || p.State == nil {
			continue
		}
		p.Builtin = false
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// AllPresets returns the built-ins followed by the user's own.
func AllPresets(path string) ([]Preset, error) {
	user, err := LoadUserPresets(path)
	if err != nil {
		return nil, err
	}
	return append(BuiltinPresets(), user...), nil
}

// FindPreset looks a preset up by name, case-insensitively. Built-ins are
// searched first, so a stale user entry that shadows a built-in name cannot
// hide it.
func FindPreset(path, name string) (Preset, error) {
	all, err := AllPresets(path)
	if err != nil {
		return Preset{}, err
	}
	for _, p := range all {
		if strings.EqualFold(p.Name, name) {
			return p, nil
		}
	}
	return Preset{}, fmt.Errorf("no preset named %q", name)
}

// isBuiltinName reports whether a name belongs to a shipped preset.
func isBuiltinName(name string) bool {
	for _, p := range BuiltinPresets() {
		if strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}

// SaveUserPreset stores state under a name, replacing any existing user preset
// with that name. Built-in names are refused: the shipped preset would still
// win at load time, so accepting the save would silently lose the user's work.
func SaveUserPreset(path, name, description string, s *DSPState) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("preset name cannot be empty")
	}
	if len(name) > 64 {
		return errors.New("preset name is too long (max 64 characters)")
	}
	if isBuiltinName(name) {
		return fmt.Errorf("%q is a built-in preset; choose another name", name)
	}

	presets, err := LoadUserPresets(path)
	if err != nil {
		return err
	}

	entry := Preset{Name: name, Description: description, State: s.Clone()}
	replaced := false
	for i := range presets {
		if strings.EqualFold(presets[i].Name, name) {
			presets[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		presets = append(presets, entry)
	}

	return writeUserPresets(path, presets)
}

// DeleteUserPreset removes a user preset by name.
func DeleteUserPreset(path, name string) error {
	if isBuiltinName(name) {
		return fmt.Errorf("%q is a built-in preset and cannot be deleted", name)
	}

	presets, err := LoadUserPresets(path)
	if err != nil {
		return err
	}

	kept := presets[:0]
	found := false
	for _, p := range presets {
		if strings.EqualFold(p.Name, name) {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return fmt.Errorf("no preset named %q", name)
	}

	return writeUserPresets(path, kept)
}

// writeUserPresets replaces the preset file.
func writeUserPresets(path string, presets []Preset) error {
	presetPath, err := PresetPath(path)
	if err != nil {
		return fmt.Errorf("failed to resolve preset path: %w", err)
	}
	if presets == nil {
		presets = []Preset{}
	}
	return writeJSON(presetPath, presetFile{Presets: presets})
}

// CopyInto overwrites dst with the preset's state, in place. The server, the CLI
// context and the WebSocket hub all share one *DSPState, so a preset has to be
// copied into it rather than swapped for it.
func (p Preset) CopyInto(dst *DSPState) error {
	if p.State == nil {
		return fmt.Errorf("preset %q has no state", p.Name)
	}
	for effID, enabled := range p.State.GetAllEnabled() {
		dst.SetEnabled(effID, enabled)
	}
	for effID, params := range p.State.GetAllParams() {
		for paramID, value := range params {
			if err := dst.SetParam(effID, paramID, value); err != nil {
				return fmt.Errorf("preset %q: effect 0x%02x param 0x%02x: %w", p.Name, effID, paramID, err)
			}
		}
	}
	return nil
}
