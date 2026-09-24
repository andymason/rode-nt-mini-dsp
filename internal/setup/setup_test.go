package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"rode-dsp/internal/dsp"
)

// The settings path setup writes into the service has to be the same file the
// tool itself reads and writes. They are worked out by different code — setup
// cannot use os.UserConfigDir, because under sudo that answers with root's
// directory — so this pins the two together.
func TestConfigPathMatchesDSP(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("path layout differs per platform; the service is Linux only")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(dsp.ConfigEnvVar, "")

	want, err := dsp.DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}
	if got := configPathIn(home, ""); got != want {
		t.Errorf("configPathIn(%q, \"\") = %q, want %q", home, got, want)
	}
}

func TestConfigPathHonoursXDG(t *testing.T) {
	got := configPathIn("/home/someone", "/somewhere/else")
	want := filepath.Join("/somewhere/else", "rode-dsp", "config.json")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The shipped unit carries placeholders for the two things it cannot know.
// Both have to be replaced, and nothing else may be.
func TestRenderUnit(t *testing.T) {
	unit, err := files.ReadFile("files/" + unitName)
	if err != nil {
		t.Fatal(err)
	}

	got := renderUnit(string(unit), "/opt/bin/rode-dsp", "/home/ada/.config/rode-dsp/config.json")

	if strings.Contains(got, configPlaceholder) {
		t.Errorf("the settings placeholder survived:\n%s", got)
	}
	if strings.Contains(got, binPlaceholder) {
		t.Errorf("the program placeholder survived:\n%s", got)
	}
	for _, want := range []string{
		"ConditionPathExists=/home/ada/.config/rode-dsp/config.json",
		"ExecStart=/opt/bin/rode-dsp load --quiet --wait 5 --config /home/ada/.config/rode-dsp/config.json",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// Substitution is global, so a placeholder left in a comment would be rewritten
// into nonsense. Comments must not contain one.
func TestPlaceholdersAreNotInComments(t *testing.T) {
	unit, err := files.ReadFile("files/" + unitName)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(unit), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, configPlaceholder) || strings.Contains(line, binPlaceholder) {
			t.Errorf("line %d is a comment holding a placeholder, which substitution would mangle: %s", i+1, line)
		}
	}
}

// Without the startup service there is one file to write, with it three.
//
// The expected paths are joined rather than written out, so that the separator
// is whatever the running platform uses. plan builds its paths the same way,
// and this test runs on every platform the build matrix covers.
func TestPlanContents(t *testing.T) {
	o := Options{
		BinDir:  "/usr/local/bin",
		UdevDir: "/etc/udev/rules.d",
		UnitDir: "/etc/systemd/system",
		Config:  "/home/ada/.config/rode-dsp/config.json",
	}

	noBoot, err := plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(noBoot) != 1 {
		t.Fatalf("without the startup service, got %d files, want 1", len(noBoot))
	}
	if got, want := noBoot[0].path, filepath.Join(o.UdevDir, accessRule); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	o.Boot = true
	boot, err := plan(o)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(o.UdevDir, accessRule),
		filepath.Join(o.UnitDir, unitName),
		filepath.Join(o.UdevDir, serviceRule),
	}
	if len(boot) != len(want) {
		t.Fatalf("got %d files, want %d", len(boot), len(want))
	}
	for i, w := range want {
		if boot[i].path != w {
			t.Errorf("file %d: got %q, want %q", i, boot[i].path, w)
		}
		if boot[i].content == "" {
			t.Errorf("file %d (%s) is empty", i, w)
		}
	}
}

// A preview must not touch the disk.
func TestDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	o := Options{
		BinDir:  filepath.Join(dir, "bin"),
		UdevDir: filepath.Join(dir, "udev"),
		UnitDir: filepath.Join(dir, "unit"),
		Config:  "/home/ada/.config/rode-dsp/config.json",
		Boot:    true,
		DryRun:  true,
	}

	var buf bytes.Buffer
	if err := printPlan(&buf, o, "/tmp/rode-dsp"); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a dry run created %v", entries)
	}
	if !strings.Contains(buf.String(), "Nothing has been changed") {
		t.Errorf("the preview does not say it changed nothing:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), o.Config) {
		t.Errorf("the preview does not name the settings file:\n%s", buf.String())
	}
}

// The step up to administrator rebuilds the command line rather than reusing
// the one that was typed, because sudo clears the environment. Everything the
// second run needs must survive.
func TestArgvCarriesEverything(t *testing.T) {
	o := Options{
		BinDir:  "/opt/bin",
		UdevDir: "/etc/udev/rules.d",
		UnitDir: "/etc/systemd/system",
		Config:  "/home/ada/.config/rode-dsp/config.json",
		Boot:    true,
	}

	got := strings.Join(o.argv(), " ")
	for _, want := range []string{"setup", "--bin-dir /opt/bin", "--udev-dir /etc/udev/rules.d",
		"--unit-dir /etc/systemd/system", "--config /home/ada/.config/rode-dsp/config.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "--no-boot") {
		t.Errorf("--no-boot appeared when the startup service was wanted: %q", got)
	}

	o.Boot = false
	if !strings.Contains(strings.Join(o.argv(), " "), "--no-boot") {
		t.Errorf("--no-boot did not survive")
	}

	o.Remove = true
	removal := strings.Join(o.argv(), " ")
	if !strings.Contains(removal, "--remove") {
		t.Errorf("--remove did not survive: %q", removal)
	}
	if strings.Contains(removal, "--config") {
		t.Errorf("removal does not need a settings file: %q", removal)
	}
}

func TestRunRejectsExtraWords(t *testing.T) {
	err := Run([]string{"--dry-run", "please"})
	if err == nil {
		t.Fatal("expected an error for a stray word")
	}
	if !strings.Contains(err.Error(), "please") {
		t.Errorf("the error does not name the stray word: %v", err)
	}
}
