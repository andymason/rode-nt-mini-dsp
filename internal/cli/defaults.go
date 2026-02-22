package cli

import (
	"flag"
	"fmt"
)

// defaultsCommand implements the "defaults" command
func defaultsCommand(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("defaults", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")
	sendToDevice := fs.Bool("send", false, "Send defaults to device (requires connection)")
	debug := fs.Bool("debug", false, "Enable debug output")
	quiet := fs.Bool("quiet", false, "Suppress progress output (errors still go to stderr)")
	fs.BoolVar(quiet, "q", false, "Shorthand for --quiet")

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

	// Set debug / quiet mode
	ctx.Debug = *debug
	ctx.Quiet = *quiet
	ctx.Device.SetDebug(*debug)

	// Load current config (if it exists)
	if err := ctx.LoadConfig(); err != nil {
		ctx.Printf("Warning: Failed to load config: %v\n", err)
	}

	// Reset to defaults
	ctx.Printf("Resetting all parameters to defaults... ")
	ctx.State.ResetToDefaults()
	ctx.Println("OK")

	// Save config
	ctx.Printf("Saving configuration to %s... ", ctx.ConfigPath)
	if err := ctx.SaveConfig(); err != nil {
		ctx.Println("FAILED")
		return fmt.Errorf("failed to save config: %w", err)
	}
	ctx.Println("OK")

	// Optionally send to device
	if *sendToDevice {
		// Check if device is connected
		if err := ctx.EnsureDeviceConnected(); err != nil {
			return fmt.Errorf("cannot send to device: %w", err)
		}

		// Send initialization packets
		ctx.Printf("Sending initialization packets... ")
		if err := ctx.Device.SendInit(); err != nil {
			ctx.Println("FAILED")
			return fmt.Errorf("failed to send init packets: %w", err)
		}
		ctx.Println("OK")

		// Send all parameters (including disabled state)
		ctx.Printf("Sending default parameters... ")
		if err := ctx.Device.SendAllParams(ctx.State); err != nil {
			ctx.Println("FAILED")
			return fmt.Errorf("failed to send parameters: %w", err)
		}
		ctx.Println("OK")

		ctx.Println("\nDefaults sent to device.")
	}

	// Show new state
	ctx.Println("\nNew default state:")
	ctx.Printf("%s", ctx.State.String())

	return nil
}

// init registers the defaults command
func init() {
	RegisterCommand("defaults", "Reset all parameters to default values", defaultsCommand)
}
