package dsp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// fileMu serialises writes from this process, so two goroutines saving at once
// cannot interleave. writeJSON explains the rest.
var fileMu sync.Mutex

// ConfigPath returns the absolute path to the configuration file.
// An empty path means the platform default; see paths.go.
func ConfigPath(path string) (string, error) {
	if path == "" {
		var err error
		if path, err = DefaultConfigPath(); err != nil {
			return "", err
		}
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

// SaveConfig saves DSP state to a JSON configuration file.
// If path is empty, uses the default config file.
func SaveConfig(path string, state *DSPState) error {
	configPath, err := ConfigPath(path)
	if err != nil {
		return fmt.Errorf("failed to resolve config path: %w", err)
	}
	return writeJSON(configPath, state)
}

// writeJSON writes a document to a file without ever leaving a half-written one
// behind: it goes to a temporary file in the same directory, is flushed to
// disk, and is then renamed over the target, which is atomic.
//
// The temporary file has a unique name, so the CLI and a running GUI writing at
// the same moment cannot land on each other. The mutex covers the same-process
// case, where they would otherwise interleave.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	fileMu.Lock()
	defer fileMu.Unlock()

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("failed to write to %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name()) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	// Flush before renaming: otherwise a crash can leave the rename durable and
	// the contents not, which is the corruption this is meant to prevent.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to flush %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("failed to finalize %s: %w", path, err)
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
