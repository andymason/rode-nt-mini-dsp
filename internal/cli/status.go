package cli

import (
	"flag"
	"fmt"

	"rode-dsp/internal/dsp"
)

// statusCommand implements the "status" command
func statusCommand(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")
	debug := fs.Bool("debug", false, "Enable debug output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Create context
	ctx := NewContext()
	defer ctx.Close()

	// Override config path if specified
	if *configPath != "" {
		ctx.ConfigPath = *configPath
	}

	// Set debug mode
	ctx.Debug = *debug
	ctx.Device.SetDebug(*debug)

	// Load configuration
	if err := ctx.LoadConfig(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Print header
	fmt.Println("RODE NT-USB Mini DSP Controller")
	fmt.Println("================================")

	// Connection status
	if err := ctx.EnsureDeviceConnected(); err != nil {
		fmt.Printf("Connection: DISCONNECTED (%v)\n", err)
	} else {
		fmt.Println("Connection: CONNECTED")
	}

	// Config file info
	if exists, err := dsp.ConfigExists(ctx.ConfigPath); err == nil && exists {
		fmt.Printf("Config file: %s (loaded)\n", ctx.ConfigPath)
	} else {
		fmt.Printf("Config file: %s (not found, using defaults)\n", ctx.ConfigPath)
	}

	fmt.Println()
	fmt.Println("Note: Values shown are from local config (last saved state).")
	fmt.Println("The device cannot be queried for its current state via USB.")
	fmt.Println()

	// Print state
	fmt.Print(ctx.State.String())

	return nil
}

// init registers the status command
func init() {
	RegisterCommand("status", "Show connection status and current parameter values", statusCommand)
}
