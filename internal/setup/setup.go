// Package setup installs rode-dsp on this computer.
//
// Three things are installed: the program itself, a rule that lets you reach
// the microphone without sudo, and a service that sends your settings to the
// microphone every time it powers up. The microphone keeps no settings of its
// own across a power cut, so that last part is the whole point of the tool.
//
// The files are embedded in the binary rather than read from the repository,
// so a downloaded release can set itself up with nothing else to hand.
package setup

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

//go:embed files
var files embed.FS

// Names of the things installed. The two udev rules are numbered so that the
// access rule is read before the service rule.
const (
	binName     = "rode-dsp"
	accessRule  = "70-rode-nt-usb-mini.rules"
	serviceRule = "71-rode-dsp-service.rules"
	unitName    = "rode-dsp.service"

	// configPlaceholder is what the unit ships with in place of a real config
	// path. It cannot be written as %h, which expands to /root in a system unit.
	configPlaceholder = "@CONFIG@"

	// binPlaceholder is the path the unit ships with. Installing somewhere else
	// has to rewrite it, so it is substituted rather than assumed.
	binPlaceholder = "/usr/local/bin/" + binName
)

// Options is a resolved instruction to install or remove. Every field is
// already decided by the time Apply or Remove sees it.
type Options struct {
	BinDir  string // where the program goes
	UdevDir string // where the access and service rules go
	UnitDir string // where the service goes
	Config  string // the settings file the service should read
	Boot    bool   // send settings automatically at startup
	DryRun  bool   // print what would be written, write nothing
	Remove  bool   // undo a previous setup
}

// file is one thing to write, with the mode it needs.
type file struct {
	path    string
	mode    os.FileMode
	content string
}

// Run parses the command line and does what it says.
func Run(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	noBoot := fs.Bool("no-boot", false, "Do not send your settings automatically at startup")
	remove := fs.Bool("remove", false, "Undo setup. Your settings are kept")
	dryRun := fs.Bool("dry-run", false, "Show the files setup would write, and write nothing")
	username := fs.String("user", "", "Whose settings the startup service should send")
	config := fs.String("config", "", "The settings file the startup service should read")

	// Where things go. The defaults follow the usual split: a program you
	// installed yourself in /usr/local/bin, rules and services an administrator
	// installed in /etc. Both the flag and the matching environment variable
	// exist because the flags are what survive the step up to administrator.
	binDir := fs.String("bin-dir", envOr("BIN_DIR", "/usr/local/bin"), "Where to put the program")
	udevDir := fs.String("udev-dir", envOr("UDEV_DIR", "/etc/udev/rules.d"), "Where to put the device rules")
	unitDir := fs.String("unit-dir", envOr("UNIT_DIR", "/etc/systemd/system"), "Where to put the startup service")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s setup [options]

Sets this up on your computer. Run it once. After that your settings go to
the microphone every time you start the computer or plug the microphone in.

Setup asks for your password, because it installs the program for everyone
on this computer.

Options:
`, os.Args[0])
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nTo see the exact files this installs, run \"%s setup --dry-run\".\n", os.Args[0])
	}

	if err := fs.Parse(args); err != nil {
		// Asking for help is not a failure. ContinueOnError is used here so that
		// a bad flag returns rather than killing the process mid-install.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("setup does not take %q", fs.Arg(0))
	}

	opts := Options{
		BinDir:  *binDir,
		UdevDir: *udevDir,
		UnitDir: *unitDir,
		Config:  *config,
		Boot:    !*noBoot,
		DryRun:  *dryRun,
		Remove:  *remove,
	}

	if opts.Config == "" {
		p, err := configPathFor(*username)
		switch {
		case err == nil:
			opts.Config = p
		case !opts.Remove:
			return err
		default:
			// Removal does not need the settings file. It only names the folder
			// at the end, and can fall back to describing it.
		}
	}

	if opts.Remove {
		return Remove(opts)
	}
	return Apply(opts)
}

// envOr reads an override from the environment, keeping the same names the
// shell installer used.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// plan is everything Apply would write, in the order it writes it. Building it
// up front means a missing embedded file is found before anything is changed.
func plan(o Options) ([]file, error) {
	access, err := files.ReadFile("files/" + accessRule)
	if err != nil {
		return nil, err
	}

	out := []file{{
		path:    filepath.Join(o.UdevDir, accessRule),
		mode:    0o644,
		content: string(access),
	}}

	if !o.Boot {
		return out, nil
	}

	unit, err := files.ReadFile("files/" + unitName)
	if err != nil {
		return nil, err
	}
	trigger, err := files.ReadFile("files/" + serviceRule)
	if err != nil {
		return nil, err
	}

	return append(out,
		file{
			path:    filepath.Join(o.UnitDir, unitName),
			mode:    0o644,
			content: renderUnit(string(unit), filepath.Join(o.BinDir, binName), o.Config),
		},
		file{
			path:    filepath.Join(o.UdevDir, serviceRule),
			mode:    0o644,
			content: string(trigger),
		},
	), nil
}

// renderUnit fills in the two things the shipped unit cannot know: where the
// program was installed, and whose settings to read.
func renderUnit(unit, binPath, configPath string) string {
	unit = strings.ReplaceAll(unit, binPlaceholder, binPath)
	return strings.ReplaceAll(unit, configPlaceholder, configPath)
}

// configPathFor works out whose settings the startup service should send.
//
// os.UserConfigDir cannot be used here: under sudo it answers with root's
// directory, and root is not who chose the settings. The name comes from
// --user, or from SUDO_USER when sudo set it, or from whoever is running.
func configPathFor(username string) (string, error) {
	if username == "" {
		username = os.Getenv("SUDO_USER")
	}

	var u *user.User
	var err error
	if username == "" {
		if os.Geteuid() == 0 {
			return "", fmt.Errorf("cannot tell whose settings to send.\n" +
				"  Run this with sudo from your own account, or name the account with --user")
		}
		u, err = user.Current()
	} else {
		u, err = user.Lookup(username)
	}
	if err != nil {
		return "", fmt.Errorf("cannot find the account %q on this computer: %w", username, err)
	}
	if u.HomeDir == "" {
		return "", fmt.Errorf("the account %q has no home folder, so name the settings file with --config", u.Username)
	}

	return configPathIn(u.HomeDir, os.Getenv("XDG_CONFIG_HOME")), nil
}

// configPathIn builds the settings path from a home folder. It has to agree
// with dsp.DefaultConfigPath, which is what writes the file; a test pins the
// two together.
func configPathIn(home, xdgConfigHome string) string {
	base := xdgConfigHome
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "rode-dsp", "config.json")
}

// printPlan shows what would be written without writing it. This is also the
// answer for anyone placing the files by hand.
func printPlan(w io.Writer, o Options, self string) error {
	items, err := plan(o)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "Setup would write these files. Nothing has been changed.\n\n")
	fmt.Fprintf(w, "Copy the program\n  from  %s\n  to    %s\n\n", self, filepath.Join(o.BinDir, binName))

	for _, f := range items {
		fmt.Fprintf(w, "%s\n%s\n", f.path, strings.Repeat("-", len(f.path)))
		fmt.Fprintf(w, "%s\n", strings.TrimRight(f.content, "\n"))
		fmt.Fprintf(w, "\n")
	}

	if o.Boot {
		fmt.Fprintf(w, "It would then reload the system's device and service settings.\n")
	}
	return nil
}

// argv rebuilds the command line from resolved options, for the step up to
// administrator. Everything is passed explicitly because sudo clears the
// environment: the settings path in particular is worked out as the real user
// and would come out as root's if it were resolved again afterwards.
func (o Options) argv() []string {
	args := []string{"setup", "--bin-dir", o.BinDir, "--udev-dir", o.UdevDir, "--unit-dir", o.UnitDir}
	if o.Remove {
		return append(args, "--remove")
	}
	args = append(args, "--config", o.Config)
	if !o.Boot {
		args = append(args, "--no-boot")
	}
	return args
}
