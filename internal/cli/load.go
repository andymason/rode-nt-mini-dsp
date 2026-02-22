package cli

import (
	"flag"
	"fmt"

	"rode-dsp/internal/dsp"
)

// loadCommand implements the "load" command: reads config file and applies
// all settings to the connected USB microphone.
func loadCommand(args []string) error {
	fs := flag.NewFlagSet("load", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")
	debug := fs.Bool("debug", false, "Enable debug output")

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
	ctx.Device.SetDebug(*debug)

	// Check config exists
	exists, err := dsp.ConfigExists(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to check config: %w", err)
	}
	if !exists {
		return fmt.Errorf("config file not found: %s\n  Run a command like 'comp', 'gate', etc. to create one first", ctx.ConfigPath)
	}

	// Load configuration
	fmt.Printf("Loading config from %s...\n", ctx.ConfigPath)
	if err := ctx.LoadConfig(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Connect to device
	fmt.Print("Connecting to device... ")
	if err := ctx.EnsureDeviceConnected(); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("device not found: %w", err)
	}
	fmt.Println("OK")

	// Send init/reset sequence
	fmt.Print("Sending INIT sequence... ")
	if err := ctx.Device.SendInit(); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("INIT sequence failed: %w", err)
	}
	fmt.Println("OK")

	// Push all parameters and enable states to device
	fmt.Print("Applying config to device... ")
	if err := ctx.Device.SendAllParams(ctx.State); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("failed to send parameters: %w", err)
	}
	fmt.Println("OK")

	fmt.Println("\nApplied settings:")
	fmt.Print(ctx.State.String())

	return nil
}

// init registers the load command
func init() {
	RegisterCommand("load", "Load config file and apply all settings to the device", loadCommand)
}
