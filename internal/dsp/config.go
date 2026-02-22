package dsp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DefaultConfigFile is the default configuration filename
const DefaultConfigFile = "rode_dsp_config.json"

// fileMu serializes config file writes to prevent corruption from rapid/concurrent saves
var fileMu sync.Mutex

// ConfigPath returns the absolute path to the configuration file.
// If an empty string is provided, returns the default config file path.
func ConfigPath(path string) (string, error) {
	if path == "" {
		return filepath.Abs(DefaultConfigFile)
	}
	return filepath.Abs(path)
}

// LoadConfig loads DSP state from a JSON configuration file.
// If the file doesn't exist, returns a new DSPState with default values.
// If path is empty, uses the default config file.
func LoadConfig(path string) (*DSPState, error) {
	configPath, err := ConfigPath(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return NewDSPState(), nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	state := NewDSPState()
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	return state, nil
}

// SaveConfig saves DSP state to a JSON configuration file using an atomic
// write (temp file + rename) to prevent corruption from concurrent or rapid saves.
// If path is empty, uses the default config file.
func SaveConfig(path string, state *DSPState) error {
	configPath, err := ConfigPath(path)
	if err != nil {
		return fmt.Errorf("failed to resolve config path: %w", err)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Marshal to indented JSON for human readability
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal DSP state: %w", err)
	}

	// Serialize file writes to prevent interleaved writes
	fileMu.Lock()
	defer fileMu.Unlock()

	// Write to a temp file first, then rename atomically
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}

	if err := os.Rename(tmpPath, configPath); err != nil {
		os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("failed to finalize config file %s: %w", configPath, err)
	}

	return nil
}

// ConfigExists checks if a configuration file exists.
// If path is empty, checks the default config file.
func ConfigExists(path string) (bool, error) {
	configPath, err := ConfigPath(path)
	if err != nil {
		return false, fmt.Errorf("failed to resolve config path: %w", err)
	}

	_, err = os.Stat(configPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check config file %s: %w", configPath, err)
	}
	return true, nil
}
