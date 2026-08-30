package cli

import (
	"flag"
	"fmt"
)

// defaultsCommand implements the "defaults" command
func defaultsCommand(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("defaults", flag.ExitOnError)
	std := addStdFlags(fs)
	sendToDevice := fs.Bool("send", false, "Send defaults to device (requires connection)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	ctx := std.context()
	defer ctx.Close()

	// Load current config (if it exists)
	if err := ctx.LoadConfig(); err != nil {
		ctx.Printf("Warning: Failed to load config: %v\n", err)
	}

	// Reset to defaults
	ctx.Printf("Resetting all parameters to defaults... ")
	ctx.State.ResetToDefaults()
	ctx.Println("OK")

	// Save config
	ctx.Printf("Saving configuration to %s... ", ctx.DisplayConfigPath())
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

		// Apply writes the off switches too, which is what makes this a
		// reset rather than "set every value but leave the effects running".
		ctx.Printf("Sending defaults to device... ")
		if err := ctx.Device.Apply(ctx.State); err != nil {
			ctx.Println("FAILED")
			return fmt.Errorf("failed to send settings: %w", err)
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
	RegisterCommand("defaults", "Reset everything to the standard settings", defaultsCommand)
}
