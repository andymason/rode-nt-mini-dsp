package cli

import (
	"flag"
	"fmt"
	"time"

	"rode-dsp/internal/dsp"
)

// loadCommand implements the "load" command: reads config file and applies
// all settings to the connected USB microphone.
func loadCommand(args []string) error {
	fs := flag.NewFlagSet("load", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")
	debug := fs.Bool("debug", false, "Enable debug output")
	quiet := fs.Bool("quiet", false, "Suppress progress output (errors still go to stderr)")
	fs.BoolVar(quiet, "q", false, "Shorthand for --quiet")
	wait := fs.Int("wait", 0, "Seconds to wait for device to appear (useful in boot scripts)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Create context
	ctx := NewContext()
	defer ctx.Close()

	if *configPath != "" {
		ctx.ConfigPath = *configPath
	}
	ctx.Debug = *debug
	ctx.Quiet = *quiet
	ctx.Device.SetDebug(*debug)

	// Check config exists
	exists, err := dsp.ConfigExists(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to check config: %w", err)
	}
	if !exists {
		return fmt.Errorf("config file not found: %s\n  Run a command like 'comp', 'gate', etc. to create one first", ctx.DisplayConfigPath())
	}

	// Load configuration
	ctx.Printf("Loading config from %s...\n", ctx.DisplayConfigPath())
	if err := ctx.LoadConfig(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Connect to device (with optional retry wait for boot scripts)
	ctx.Printf("Connecting to device... ")
	waitDur := time.Duration(*wait) * time.Second
	if err := ctx.EnsureDeviceConnectedWithWait(waitDur); err != nil {
		ctx.Println("FAILED")
		return fmt.Errorf("device not found: %w", err)
	}
	ctx.Println("OK")

	// Send init/reset sequence
	ctx.Printf("Sending startup handshake... ")
	if err := ctx.Device.Handshake(); err != nil {
		ctx.Println("FAILED")
		return fmt.Errorf("startup handshake failed: %w", err)
	}
	ctx.Println("OK")

	// Push all parameters and enable states to device
	ctx.Printf("Applying config to device... ")
	if err := ctx.Device.SendAllParams(ctx.State); err != nil {
		ctx.Println("FAILED")
		return fmt.Errorf("failed to send parameters: %w", err)
	}
	ctx.Println("OK")

	ctx.Println("\nApplied settings:")
	ctx.Printf("%s", ctx.State.String())

	return nil
}

// init registers the load command
func init() {
	RegisterCommand("load", "Load config file and apply all settings to the device", loadCommand)
}
