package dsp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// isolate points the per-user config directory at a fresh temp tree, so these
// tests never read or write the developer's real config.
func isolate(t *testing.T) (userDir string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	_ = os.Unsetenv(ConfigEnvVar)

	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("USERPROFILE", home)
	case "darwin":
		// os.UserConfigDir uses $HOME/Library/Application Support.
	default:
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	}

	dir, err := UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	return dir
}

// The config is the per-user one, whatever the working directory holds.
func TestDefaultConfigPath(t *testing.T) {
	userDir := isolate(t)

	// A file of the old project-local name must not be picked up.
	if err := os.WriteFile(filepath.Join(t.TempDir(), "rode_dsp_config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(userDir, configFileName)
	got, err := DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

// The env var overrides the default, and --config overrides the env var.
func TestConfigPathPrecedence(t *testing.T) {
	userDir := isolate(t)

	env := filepath.Join(t.TempDir(), "from-env.json")
	t.Setenv(ConfigEnvVar, env)

	got, err := ConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	if got != env {
		t.Errorf("with %s set, ConfigPath(\"\") = %q, want %q", ConfigEnvVar, got, env)
	}

	explicit := filepath.Join(t.TempDir(), "explicit.json")
	if got, err = ConfigPath(explicit); err != nil {
		t.Fatal(err)
	} else if got != explicit {
		t.Errorf("explicit path should win, got %q", got)
	}

	// Unsetting the override falls back to the per-user file.
	_ = os.Unsetenv(ConfigEnvVar)
	if got, err = ConfigPath(""); err != nil {
		t.Fatal(err)
	} else if got != filepath.Join(userDir, configFileName) {
		t.Errorf("without the env var, ConfigPath(\"\") = %q", got)
	}
}

// A relative --config is made absolute, so messages name a findable file.
func TestConfigPathIsAbsolute(t *testing.T) {
	isolate(t)

	got, err := ConfigPath("relative.json")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("ConfigPath(%q) = %q, want an absolute path", "relative.json", got)
	}
}

// Presets sit beside whichever config is in use.
func TestPresetPathFollowsConfig(t *testing.T) {
	userDir := isolate(t)

	got, err := PresetPathFor("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(userDir, presetFileName); got != want {
		t.Errorf("PresetPathFor(\"\") = %q, want %q", got, want)
	}

	if got, err = PresetPathFor("/srv/profiles/studio.json"); err != nil {
		t.Fatal(err)
	} else if want := filepath.Join("/srv/profiles", presetFileName); got != want {
		t.Errorf("PresetPathFor(explicit) = %q, want %q", got, want)
	}
}

// Saving creates the per-user directory, which does not exist on a fresh
// machine, and the file is found again afterwards.
func TestSaveConfigCreatesUserDir(t *testing.T) {
	userDir := isolate(t)

	if err := SaveConfig("", NewDSPState()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	want := filepath.Join(userDir, configFileName)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("config was not created at %s: %v", want, err)
	}

	exists, err := ConfigExists("")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("ConfigExists() = false after a save")
	}
}

// With no home directory there is nowhere to default to, so the user is told
// rather than having a file written somewhere arbitrary.
func TestNoHomeDirectoryIsAnError(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv(ConfigEnvVar)

	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", "")
	default:
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "")
	}

	if _, err := DefaultConfigPath(); err == nil {
		t.Error("DefaultConfigPath() succeeded with no home directory")
	}
	if err := SaveConfig("", NewDSPState()); err == nil {
		t.Error("SaveConfig succeeded with no home directory")
	}
}
