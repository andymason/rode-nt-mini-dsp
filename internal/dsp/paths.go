package dsp

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config and presets live in the platform's per-user configuration directory,
// which is what os.UserConfigDir resolves:
//
//	Linux/BSD   $XDG_CONFIG_HOME/rode-dsp/   (default ~/.config/rode-dsp/)
//	Windows     %AppData%\rode-dsp\          (Roaming, so settings follow the user)
//	macOS       ~/Library/Application Support/rode-dsp/
//
// That is the only place files are looked for. A specific file can still be
// named with --config or $RODE_DSP_CONFIG, which is how the systemd unit reads
// the machine-wide config from /etc.
const (
	appDirName     = "rode-dsp"
	configFileName = "config.json"
	presetFileName = "presets.json"

	// ConfigEnvVar names the config file, and is itself overridden by --config.
	ConfigEnvVar = "RODE_DSP_CONFIG"
)

// UserConfigDir is the per-user directory holding this application's files.
func UserConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		// Only reachable with no HOME (or no %AppData% on Windows), so point at
		// the two ways to carry on regardless.
		return "", fmt.Errorf("cannot locate the user config directory, so pass --config or set %s: %w", ConfigEnvVar, err)
	}
	return filepath.Join(base, appDirName), nil
}

// DefaultConfigPath is the config file to use when none was named explicitly.
func DefaultConfigPath() (string, error) {
	if p := os.Getenv(ConfigEnvVar); p != "" {
		return p, nil
	}
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

// PresetPathFor returns the preset file belonging beside the given config file,
// so presets follow the config instead of landing in the working directory. An
// empty configPath means the default config.
func PresetPathFor(configPath string) (string, error) {
	if configPath == "" {
		var err error
		if configPath, err = DefaultConfigPath(); err != nil {
			return "", err
		}
	}
	return filepath.Join(filepath.Dir(configPath), presetFileName), nil
}
