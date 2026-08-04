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
	quiet := fs.Bool("quiet", false, "Suppress progress output (errors still go to stderr)")
	fs.BoolVar(quiet, "q", false, "Shorthand for --quiet")
	wait := fs.Int("wait", 0, "Seconds to wait for device to appear (useful in boot scripts)")

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

	// Load configuration
	if err := ctx.LoadConfig(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Connect to device (with optional retry wait for boot scripts)
	ctx.Printf("Connecting to device... ")
	waitDur := time.Duration(*wait) * time.Second
	if err := ctx.EnsureDeviceConnectedWithWait(waitDur); err != nil {
		ctx.Println("FAILED")
		return fmt.Errorf("connection failed: %w", err)
	}
	ctx.Println("OK")

	// Send initialization packets (unless disabled)
	if !*noInit {
		ctx.Printf("Sending initialization packets... ")
		if err := ctx.Device.Handshake(); err != nil {
			ctx.Println("FAILED")
			ctx.Device.Disconnect()
			return fmt.Errorf("failed to send init packets: %w", err)
		}
		ctx.Println("OK")
	}

	// Send all current parameters
	ctx.Printf("Sending current parameters... ")
	if err := ctx.Device.SendAllParams(ctx.State); err != nil {
		ctx.Println("FAILED")
		ctx.Device.Disconnect()
		return fmt.Errorf("failed to send parameters: %w", err)
	}
	ctx.Println("OK")

	// Save configuration (in case defaults were used)
	if err := ctx.SaveConfig(); err != nil {
		ctx.Printf("Warning: Failed to save config: %v\n", err)
	}

	ctx.Println("\nDevice connected and configured.")
	ctx.Println("Current state:")
	ctx.Printf("%s", ctx.State.String())

	return nil
}

// disconnectCommand implements the "disconnect" command
func disconnectCommand(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("disconnect", flag.ExitOnError)
	debug := fs.Bool("debug", false, "Enable debug output")
	quiet := fs.Bool("quiet", false, "Suppress progress output (errors still go to stderr)")
	fs.BoolVar(quiet, "q", false, "Shorthand for --quiet")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Create context
	ctx := NewContext()
	defer ctx.Close()

	// Set debug / quiet mode
	ctx.Debug = *debug
	ctx.Quiet = *quiet
	ctx.Device.SetDebug(*debug)

	// Check if device is connected
	if !ctx.Device.Connected() {
		ctx.Println("Device is not connected.")
		return nil
	}

	ctx.Printf("Disconnecting... ")
	ctx.Device.Disconnect()

	// Small delay to ensure cleanup
	time.Sleep(50 * time.Millisecond)

	ctx.Println("OK")
	return nil
}

// init registers the connect and disconnect commands
func init() {
	RegisterCommand("connect", "Connect to device and send current parameters", connectCommand)
	RegisterCommand("disconnect", "Disconnect from device", disconnectCommand)
}
