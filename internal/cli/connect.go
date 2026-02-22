package cli

import (
	"flag"
	"fmt"
	"time"
)

// connectCommand implements the "connect" command
func connectCommand(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	debug := fs.Bool("debug", false, "Enable debug output")
	noInit := fs.Bool("no-init", false, "Skip sending initialization packets")
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")

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

	// Connect to device
	fmt.Print("Connecting to device... ")
	if err := ctx.Device.Connect(); err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("connection failed: %w", err)
	}
	fmt.Println("OK")
	ctx.deviceInitialized = true

	// Send initialization packets (unless disabled)
	if !*noInit {
		fmt.Print("Sending initialization packets... ")
		if err := ctx.Device.SendInit(); err != nil {
			fmt.Println("FAILED")
			ctx.Device.Disconnect()
			return fmt.Errorf("failed to send init packets: %w", err)
		}
		fmt.Println("OK")
	}

	// Send all current parameters
	fmt.Print("Sending current parameters... ")
	if err := ctx.Device.SendAllParams(ctx.State); err != nil {
		fmt.Println("FAILED")
		ctx.Device.Disconnect()
		return fmt.Errorf("failed to send parameters: %w", err)
	}
	fmt.Println("OK")

	// Save configuration (in case defaults were used)
	if err := ctx.SaveConfig(); err != nil {
		fmt.Printf("Warning: Failed to save config: %v\n", err)
	}

	fmt.Println("\nDevice connected and configured.")
	fmt.Println("Current state:")
	fmt.Print(ctx.State.String())

	return nil
}

// disconnectCommand implements the "disconnect" command
func disconnectCommand(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("disconnect", flag.ExitOnError)
	debug := fs.Bool("debug", false, "Enable debug output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Create context
	ctx := NewContext()
	defer ctx.Close()

	// Set debug mode
	ctx.Debug = *debug
	ctx.Device.SetDebug(*debug)

	// Check if device is connected
	if !ctx.Device.Connected() {
		fmt.Println("Device is not connected.")
		return nil
	}

	fmt.Print("Disconnecting... ")
	ctx.Device.Disconnect()

	// Small delay to ensure cleanup
	time.Sleep(50 * time.Millisecond)

	fmt.Println("OK")
	return nil
}

// init registers the connect and disconnect commands
func init() {
	RegisterCommand("connect", "Connect to device and send current parameters", connectCommand)
	RegisterCommand("disconnect", "Disconnect from device", disconnectCommand)
}
