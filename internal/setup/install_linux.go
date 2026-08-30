package setup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Apply installs everything, or shows what it would install.
//
// The order matters. Everything that can fail is checked before anything is
// written, so a failure here cannot leave the computer half set up.
func Apply(o Options) error {
	self, err := selfPath()
	if err != nil {
		return err
	}

	if o.DryRun {
		return printPlan(os.Stdout, o, self)
	}

	items, err := plan(o)
	if err != nil {
		return err
	}
	if err := checkTools(o.Boot); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return elevate(o)
	}

	binPath := filepath.Join(o.BinDir, binName)
	if err := installProgram(self, binPath); err != nil {
		return err
	}
	for _, f := range items {
		if err := writeFile(f); err != nil {
			return err
		}
	}

	if o.Boot {
		if err := run("systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	// Applies the new rules to a microphone that is already plugged in, which
	// with the startup service also runs it straight away.
	if err := reloadDevices(); err != nil {
		return err
	}

	report(o, binPath)
	return nil
}

// Remove undoes Apply. Settings are deliberately left alone: someone removing
// the program is not asking to lose the sound they spent time on.
func Remove(o Options) error {
	if os.Geteuid() != 0 {
		return elevate(o)
	}

	// Nothing to disable. The service has no [Install] section; the device rule
	// is what starts it.
	if have("systemctl") {
		_ = exec.Command("systemctl", "stop", unitName).Run()
	}

	for _, p := range []string{
		filepath.Join(o.UnitDir, unitName),
		filepath.Join(o.UdevDir, serviceRule),
		filepath.Join(o.UdevDir, accessRule),
		filepath.Join(o.BinDir, binName),
	} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove %s: %w", p, err)
		}
	}

	if have("systemctl") {
		_ = run("systemctl", "daemon-reload")
	}
	if have("udevadm") {
		_ = reloadDevices()
	}

	fmt.Println("Removed.")
	fmt.Println()
	fmt.Println("Your settings were kept. Delete this folder if you want them gone too:")
	if dir := filepath.Dir(o.Config); dir != "" && dir != "." {
		fmt.Printf("  %s\n", dir)
	} else {
		fmt.Println("  ~/.config/rode-dsp")
	}
	return nil
}

// checkTools makes sure the two system commands this needs are present, before
// asking for a password.
func checkTools(boot bool) error {
	if !have("udevadm") {
		return errors.New("this computer has no udevadm, which setup needs to give you access to the microphone")
	}
	if boot && !have("systemctl") {
		return errors.New("this computer does not use systemd, so settings cannot be sent at startup.\n" +
			"  Run \"setup --no-boot\" to install the rest, then \"rode-dsp load\" when you want your settings")
	}
	return nil
}

func have(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// elevate runs this same command again as administrator. The user types one
// command and answers one password prompt.
func elevate(o Options) error {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return errors.New("setup needs administrator rights. Run it again as root")
	}
	self, err := selfPath()
	if err != nil {
		return err
	}

	fmt.Println("Setup needs administrator rights, so it will ask for your password.")
	cmd := exec.Command(sudo, append([]string{self}, o.argv()...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setup did not finish: %w", err)
	}
	return nil
}

// installProgram copies the running program to its permanent home. The copy is
// streamed rather than read into memory: the program is several megabytes, and
// there is no reason to hold two copies of it.
func installProgram(self, dest string) error {
	if same(self, dest) {
		return nil
	}
	src, err := os.Open(self)
	if err != nil {
		return fmt.Errorf("cannot read this program: %w", err)
	}
	defer func() { _ = src.Close() }()

	return writeAtomic(dest, 0o755, func(w io.Writer) error {
		_, err := io.Copy(w, src)
		return err
	})
}

func same(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// writeFile puts one planned file in place.
func writeFile(f file) error {
	return writeAtomic(f.path, f.mode, func(w io.Writer) error {
		_, err := io.WriteString(w, f.content)
		return err
	})
}

// writeAtomic writes to a temporary file in the destination directory and
// renames it into place, so a reader never sees a half-written file. Renaming
// is also what lets a running program overwrite itself: the old file stays
// valid for anything still holding it open.
func writeAtomic(path string, mode os.FileMode, write func(io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".rode-dsp-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	// A no-op once the rename below succeeds.
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := write(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	// Flush before renaming, so a crash cannot leave the new name pointing at
	// contents that never reached the disk.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot flush %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("cannot put %s in place: %w", path, err)
	}
	return nil
}

func reloadDevices() error {
	if err := run("udevadm", "control", "--reload-rules"); err != nil {
		return err
	}
	return run("udevadm", "trigger", "--subsystem-match=hidraw")
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v failed: %w", name, args, err)
	}
	return nil
}

// report tells the user what happened and what to do next, in that order.
func report(o Options, binPath string) {
	fmt.Println()
	fmt.Println("Done. Your microphone is ready.")
	fmt.Println()
	fmt.Printf("  Program   %s\n", binPath)
	fmt.Printf("  Settings  %s\n", o.Config)
	fmt.Println()

	if o.Boot {
		fmt.Println("Your settings will go to the microphone every time you start this")
		fmt.Println("computer or plug the microphone in.")
	} else {
		fmt.Println("Your settings will not be sent automatically. Run \"rode-dsp load\"")
		fmt.Println("when you want them.")
	}

	if _, err := os.Stat(o.Config); err != nil {
		fmt.Println()
		fmt.Println("You have not chosen any settings yet, so the microphone is using its own.")
	}

	fmt.Println()
	fmt.Println("Next, open the settings page:")
	fmt.Println("  rode-dsp gui")
}
