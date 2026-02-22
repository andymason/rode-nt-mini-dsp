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

	// Load current config (if it exists)
	if err := ctx.LoadConfig(); err != nil {
		fmt.Printf("Warning: Failed to load config: %v\n", err)
	}

	// Reset to defaults
	fmt.Print("Resetting all parameters to defaults... ")
	ctx.State.ResetToDefaults()
	fmt.Println("OK")

	// Save config
	fmt.Printf("Saving configuration to %s... ", ctx.ConfigPath)
	if err := ctx.SaveConfig(); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("failed to save config: %w", err)
	}
	fmt.Println("OK")

	// Optionally send to device
	if *sendToDevice {
		// Check if device is connected
		if err := ctx.EnsureDeviceConnected(); err != nil {
			return fmt.Errorf("cannot send to device: %w", err)
		}

		// Send initialization packets
		fmt.Print("Sending initialization packets... ")
		if err := ctx.Device.SendInit(); err != nil {
			fmt.Println("FAILED")
			return fmt.Errorf("failed to send init packets: %w", err)
		}
		fmt.Println("OK")

		// Send all parameters (including disabled state)
		fmt.Print("Sending default parameters... ")
		if err := ctx.Device.SendAllParams(ctx.State); err != nil {
			fmt.Println("FAILED")
			return fmt.Errorf("failed to send parameters: %w", err)
		}
		fmt.Println("OK")

		fmt.Println("\nDefaults sent to device.")
	}

	// Show new state
	fmt.Println("\nNew default state:")
	fmt.Print(ctx.State.String())

	return nil
}

// init registers the defaults command
func init() {
	RegisterCommand("defaults", "Reset all parameters to default values", defaultsCommand)
}
