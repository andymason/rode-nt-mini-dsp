package cli

import (
	"encoding/json"
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
	quiet := fs.Bool("quiet", false, "Suppress progress output (errors still go to stderr)")
	fs.BoolVar(quiet, "q", false, "Shorthand for --quiet")
	asJSON := fs.Bool("json", false, "Output DSP state as JSON (machine-readable, useful in scripts)")

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

	// JSON output: emit machine-readable state and exit
	if *asJSON {
		out := struct {
			Connected  bool          `json:"connected"`
			ConfigFile string        `json:"config_file"`
			State      *dsp.DSPState `json:"state"`
		}{
			ConfigFile: ctx.ConfigPath,
			State:      ctx.State,
		}
		if err := ctx.EnsureDeviceConnected(); err == nil {
			out.Connected = true
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal state: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Human-readable output
	ctx.Println("RODE NT-USB Mini DSP Controller")
	ctx.Println("================================")

	// Connection status
	if err := ctx.EnsureDeviceConnected(); err != nil {
		ctx.Printf("Connection: DISCONNECTED (%v)\n", err)
	} else {
		ctx.Println("Connection: CONNECTED")
	}

	// Config file info
	if exists, err := dsp.ConfigExists(ctx.ConfigPath); err == nil && exists {
		ctx.Printf("Config file: %s (loaded)\n", ctx.ConfigPath)
	} else {
		ctx.Printf("Config file: %s (not found, using defaults)\n", ctx.ConfigPath)
	}

	ctx.Println()
	ctx.Println("Note: Values shown are from local config (last saved state).")
	ctx.Println("The device cannot be queried for its current state via USB.")
	ctx.Println()

	// Print state
	ctx.Printf("%s", ctx.State.String())

	return nil
}

// init registers the status command
func init() {
	RegisterCommand("status", "Show connection status and current parameter values", statusCommand)
}
