package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"

	"rode-dsp/internal/dsp"
)

// Command is one subcommand. Each one parses its own flags, so there is no
// shared FlagSet to keep in step.
type Command struct {
	Name        string
	Description string
	Run         func(args []string) error

	// Advanced keeps a command out of the main help listing. The
	// reverse-engineering commands are useful about once a year and would
	// otherwise be the first thing a new user reads.
	Advanced bool
}

// Commands registry
var commands = make(map[string]*Command)

// RegisterCommand registers a new command
func RegisterCommand(name, description string, run func(args []string) error) *Command {
	cmd := &Command{Name: name, Description: description, Run: run}
	commands[name] = cmd
	return cmd
}

// RegisterAdvancedCommand registers a command shown only under "Advanced".
func RegisterAdvancedCommand(name, description string, run func(args []string) error) *Command {
	cmd := RegisterCommand(name, description, run)
	cmd.Advanced = true
	return cmd
}

// Dispatch executes the appropriate command based on args
func Dispatch(args []string) error {
	if len(args) < 1 {
		Usage()
		return errors.New("no command specified")
	}

	cmdName := args[0]
	cmd, exists := commands[cmdName]
	if !exists {
		Usage()
		return fmt.Errorf("unknown command: %s", cmdName)
	}

	return cmd.Run(args[1:])
}

// noExtraArgs rejects leftover positional arguments. Go's flag package stops
// parsing at the first non-flag word and stashes the rest in fs.Args(), so
// "rode-dsp status gui" would otherwise run status and silently drop "gui".
func noExtraArgs(fs *flag.FlagSet) error {
	if fs.NArg() == 0 {
		return nil
	}
	extra := fs.Args()
	if _, isCommand := commands[extra[0]]; isCommand {
		return fmt.Errorf("unexpected argument %q: run one command at a time, e.g. %q",
			extra[0], os.Args[0]+" "+extra[0])
	}
	return fmt.Errorf("unexpected argument(s): %v", extra)
}

// BriefStatus prints a short connection status without config details.
//
// This is what someone sees when they run a freshly downloaded program with no
// arguments, so it is also where the first instruction belongs.
func BriefStatus() {
	fmt.Fprintf(os.Stderr, "RODE NT-USB Mini sound settings\n\n")
	ctx := NewContext()
	defer ctx.Close()
	if err := ctx.EnsureDeviceConnected(); err != nil {
		fmt.Fprintf(os.Stderr, "Microphone: not found (%v)\n\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "Microphone: connected\n\n")
	}
	if hint := setupHint(); hint != "" {
		fmt.Fprintf(os.Stderr, "%s\n\n", hint)
	}
}

// setupHint tells a first-time user what to do, and says nothing once setup has
// run. The access rule is the marker because it is the file that has to exist
// before the microphone can be reached without sudo.
func setupHint() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if _, err := os.Stat("/etc/udev/rules.d/70-rode-nt-usb-mini.rules"); err == nil {
		return ""
	}
	return "Not set up yet. Run:\n  sudo " + os.Args[0] + " setup"
}

// Usage prints the CLI usage information.
func Usage() {
	prog := os.Args[0]

	fmt.Fprintf(os.Stderr, "Usage: %s <command> [options]\n\n", prog)

	listCommands("Commands:", false)
	listCommands("Advanced (for working on the protocol; you will not need these):", true)

	fmt.Fprintf(os.Stderr, `Getting started:
  sudo %[1]s setup    set this up once
  %[1]s gui           choose how you want to sound

Examples:
  %[1]s status                          what the microphone is set to now
  %[1]s comp --enable --threshold -20   turn the compressor on
  %[1]s load                            send your saved settings again

Your settings are saved as you make them. After setup they go to the
microphone every time you start the computer or plug the microphone in.

Environment:
  %[2]s   the settings file to use (--config wins)

Run "%[1]s <command> -help" for that command's own options.
`, prog, dsp.ConfigEnvVar)
}

// listCommands prints one section of the help, in name order.
func listCommands(heading string, advanced bool) {
	names := make([]string, 0, len(commands))
	for name, cmd := range commands {
		if cmd.Advanced == advanced {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)

	fmt.Fprintf(os.Stderr, "%s\n", heading)
	for _, name := range names {
		fmt.Fprintf(os.Stderr, "  %-14s %s\n", name, commands[name].Description)
	}
	fmt.Fprintln(os.Stderr)
}
