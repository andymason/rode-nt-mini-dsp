package cli

import (
	"flag"
	"fmt"
	"os"
)

// Command represents a CLI command
type Command struct {
	Name        string
	Description string
	Run         func(args []string) error
	Flags       *flag.FlagSet
}

// Commands registry
var commands = make(map[string]*Command)

// RegisterCommand registers a new command
func RegisterCommand(name, description string, run func(args []string) error) *Command {
	cmd := &Command{
		Name:        name,
		Description: description,
		Run:         run,
	}
	commands[name] = cmd
	return cmd
}

// Dispatch executes the appropriate command based on args
func Dispatch(args []string) error {
	if len(args) < 1 {
		Usage()
		return fmt.Errorf("no command specified")
	}

	cmdName := args[0]
	cmd, exists := commands[cmdName]
	if !exists {
		Usage()
		return fmt.Errorf("unknown command: %s", cmdName)
	}

	// Parse flags if the command has a FlagSet
	if cmd.Flags != nil {
		if err := cmd.Flags.Parse(args[1:]); err != nil {
			return err
		}
		return cmd.Run(cmd.Flags.Args())
	}

	return cmd.Run(args[1:])
}

// BriefStatus prints a short connection status without config details
func BriefStatus() {
	fmt.Fprintf(os.Stderr, "RODE NT-USB Mini DSP Controller\n\n")
	ctx := NewContext()
	defer ctx.Close()
	if err := ctx.EnsureDeviceConnected(); err != nil {
		fmt.Fprintf(os.Stderr, "Connection: DISCONNECTED (%v)\n\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "Connection: CONNECTED\n\n")
	}
}

// Usage prints the CLI usage information
func Usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <command> [options]\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Commands:\n")

	// List all registered commands
	for name, cmd := range commands {
		fmt.Fprintf(os.Stderr, "  %-15s %s\n", name, cmd.Description)
	}

	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  %s status\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s gui\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s comp --enable --threshold -20\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\nUse \"%s <command> -help\" for command-specific help\n", os.Args[0])
}

// AddCommand adds a command with its flagset (for commands that need flags)
func AddCommand(name, description string, flags *flag.FlagSet, run func(args []string) error) {
	cmd := &Command{
		Name:        name,
		Description: description,
		Run:         run,
		Flags:       flags,
	}
	commands[name] = cmd
}
