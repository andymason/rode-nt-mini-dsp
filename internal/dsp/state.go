package dsp

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
)

// DSPState holds the current state of all DSP effects
type DSPState struct {
	mu      sync.RWMutex
	enabled map[byte]bool
	params  map[byte]map[byte]float64
}

// NewDSPState creates a new DSPState with default values
func NewDSPState() *DSPState {
	state := &DSPState{
		enabled: make(map[byte]bool),
		params:  make(map[byte]map[byte]float64),
	}

	// Initialize with default values from Effects registry
	for effID, effect := range Effects {
		state.enabled[effID] = false // Default disabled
		state.params[effID] = make(map[byte]float64)
		for _, param := range effect.Params {
			state.params[effID][param.ParamID] = param.Default
		}
	}

	return state
}

// Clone creates a deep copy of the DSPState
func (s *DSPState) Clone() *DSPState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clone := &DSPState{
		enabled: make(map[byte]bool),
		params:  make(map[byte]map[byte]float64),
	}

	// Copy enabled flags
	for k, v := range s.enabled {
		clone.enabled[k] = v
	}

	// Copy params
	for effID, paramMap := range s.params {
		clone.params[effID] = make(map[byte]float64)
		for paramID, value := range paramMap {
			clone.params[effID][paramID] = value
		}
	}

	return clone
}

// IsEnabled returns whether an effect is enabled
func (s *DSPState) IsEnabled(effID byte) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled[effID]
}

// SetEnabled sets the enabled state of an effect
func (s *DSPState) SetEnabled(effID byte, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled[effID] = enabled
}

// GetParam returns the current value of a parameter
func (s *DSPState) GetParam(effID, paramID byte) (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if paramMap, ok := s.params[effID]; ok {
		if value, ok := paramMap[paramID]; ok {
			return value, true
		}
	}
	return 0.0, false
}

// SetParam sets a parameter value with validation
func (s *DSPState) SetParam(effID, paramID byte, value float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find effect definition
	effect, ok := Effects[effID]
	if !ok {
		return fmt.Errorf("unknown effect ID: 0x%02x", effID)
	}

	// Find parameter definition
	var paramDef *ParamDef
	for _, p := range effect.Params {
		if p.ParamID == paramID {
			paramDef = &p
			break
		}
	}
	if paramDef == nil {
		return fmt.Errorf("unknown parameter ID: 0x%02x for effect 0x%02x", paramID, effID)
	}

	// Validate range
	if value < paramDef.UIMin || value > paramDef.UIMax {
		return fmt.Errorf("value %.2f out of range [%.1f, %.1f] for %s %s",
			value, paramDef.UIMin, paramDef.UIMax, effect.Name, paramDef.Name)
	}

	// Apply resolution (snap to nearest resolution step)
	if paramDef.Resolution > 0 {
		steps := (value - paramDef.UIMin) / paramDef.Resolution
		value = paramDef.UIMin + math.Round(steps)*paramDef.Resolution
		// Ensure we stay within bounds after rounding
		if value < paramDef.UIMin {
			value = paramDef.UIMin
		}
		if value > paramDef.UIMax {
			value = paramDef.UIMax
		}
	}

	// Store value
	if s.params[effID] == nil {
		s.params[effID] = make(map[byte]float64)
	}
	s.params[effID][paramID] = value
	return nil
}

// ResetToDefaults resets all parameters to their default values
func (s *DSPState) ResetToDefaults() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for effID, effect := range Effects {
		s.enabled[effID] = false
		if s.params[effID] == nil {
			s.params[effID] = make(map[byte]float64)
		}
		for _, param := range effect.Params {
			s.params[effID][param.ParamID] = param.Default
		}
	}
}

// GetAllParams returns a map of all parameter values for all effects
func (s *DSPState) GetAllParams() map[byte]map[byte]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[byte]map[byte]float64)
	for effID, paramMap := range s.params {
		result[effID] = make(map[byte]float64)
		for paramID, value := range paramMap {
			result[effID][paramID] = value
		}
	}
	return result
}

// GetAllEnabled returns a map of all enabled states
func (s *DSPState) GetAllEnabled() map[byte]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[byte]bool)
	for effID, enabled := range s.enabled {
		result[effID] = enabled
	}
	return result
}

// toConfigKey converts a name like "Noise Gate" to a JSON config key like "noise_gate"
func toConfigKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "_"))
}

// MarshalJSON implements json.Marshaler for DSPState
// Produces human-readable JSON with named effects and parameters, e.g.:
//
//	{ "compressor": { "enabled": true, "threshold": -20.0, ... }, ... }
func (s *DSPState) MarshalJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build ordered output using effect ID order for consistency
	effectIDs := []byte{0x00, 0x01, 0x02, 0x03}
	data := make(map[string]interface{}, len(effectIDs))

	for _, effID := range effectIDs {
		effect, ok := Effects[effID]
		if !ok {
			continue
		}
		effKey := toConfigKey(effect.Name)
		effData := make(map[string]interface{}, len(effect.Params)+1)
		effData["enabled"] = s.enabled[effID]

		if paramMap, ok := s.params[effID]; ok {
			for _, param := range effect.Params {
				if value, ok := paramMap[param.ParamID]; ok {
					effData[toConfigKey(param.Name)] = value
				}
			}
		}
		data[effKey] = effData
	}

	return json.Marshal(data)
}

// UnmarshalJSON implements json.Unmarshaler for DSPState
func (s *DSPState) UnmarshalJSON(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Parse into raw map
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Reset state
	s.enabled = make(map[byte]bool)
	s.params = make(map[byte]map[byte]float64)

	// Build reverse lookup: config key → effect def
	for effID, effect := range Effects {
		effKey := toConfigKey(effect.Name)
		effJSON, ok := raw[effKey]
		if !ok {
			continue
		}

		var effData map[string]json.RawMessage
		if err := json.Unmarshal(effJSON, &effData); err != nil {
			continue
		}

		// Parse enabled
		if enabledJSON, ok := effData["enabled"]; ok {
			var enabled bool
			if err := json.Unmarshal(enabledJSON, &enabled); err == nil {
				s.enabled[effID] = enabled
			}
		}

		// Parse parameters
		if s.params[effID] == nil {
			s.params[effID] = make(map[byte]float64)
		}
		for _, param := range effect.Params {
			paramKey := toConfigKey(param.Name)
			if valJSON, ok := effData[paramKey]; ok {
				var value float64
				if err := json.Unmarshal(valJSON, &value); err == nil {
					// Config files are hand-editable and this path bypasses
					// SetParam's validation, so clamp here. Without it an
					// out-of-range value reaches the encoders directly.
					s.params[effID][param.ParamID] = clampToRange(value, param)
				}
			}
		}
	}

	// Fill any missing effects/params with defaults
	for effID, effect := range Effects {
		if s.params[effID] == nil {
			s.params[effID] = make(map[byte]float64)
		}
		for _, param := range effect.Params {
			if _, exists := s.params[effID][param.ParamID]; !exists {
				s.params[effID][param.ParamID] = param.Default
			}
		}
		if _, exists := s.enabled[effID]; !exists {
			s.enabled[effID] = false
		}
	}

	return nil
}

// String returns a human-readable representation of the state
func (s *DSPState) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("DSP State:\n")

	// Iterate in sorted effect ID order to ensure consistent output
	// (Go map iteration order is randomized)
	effectIDs := []byte{0x00, 0x01, 0x02, 0x03}
	for _, effID := range effectIDs {
		effect, ok := Effects[effID]
		if !ok {
			continue
		}
		enabled := s.enabled[effID]
		sb.WriteString(fmt.Sprintf("  %s: %s\n", effect.Name, map[bool]string{true: "ENABLED", false: "DISABLED"}[enabled]))
		if paramMap, ok := s.params[effID]; ok {
			for _, param := range effect.Params {
				if value, ok := paramMap[param.ParamID]; ok {
					sb.WriteString(fmt.Sprintf("    %s: %s\n", param.Name, param.FormatFn(value)))
				}
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// clampToRange constrains a value to a parameter's declared UI range, mapping
// NaN to the default. Used on the config-load path, which does not go through
// SetParam and so has no other validation.
func clampToRange(value float64, param ParamDef) float64 {
	if math.IsNaN(value) {
		return param.Default
	}
	if value < param.UIMin {
		return param.UIMin
	}
	if value > param.UIMax {
		return param.UIMax
	}
	return value
}
