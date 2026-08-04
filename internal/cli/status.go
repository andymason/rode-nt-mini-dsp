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
	fromConfig := fs.Bool("config-only", false,
		"Show the local config instead of querying the device")

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

	// Prefer the device's own state over the config's record of it. The two
	// disagree whenever something else has driven the microphone since the last
	// save — RØDE Connect, another rode-dsp instance, or a replug.
	connected := ctx.EnsureDeviceConnected() == nil
	state := ctx.State
	source := "local config (last saved state)"

	if connected && !*fromConfig {
		if live, err := ctx.Device.ReadState(); err == nil {
			state = live
			source = "device"
		} else if !*asJSON {
			ctx.Printf("Note: could not read device state (%v); showing local config.\n", err)
		}
	}

	// JSON output: emit machine-readable state and exit
	if *asJSON {
		out := struct {
			Connected  bool          `json:"connected"`
			ConfigFile string        `json:"config_file"`
			Source     string        `json:"source"`
			State      *dsp.DSPState `json:"state"`
		}{
			Connected:  connected,
			ConfigFile: ctx.ConfigPath,
			Source:     source,
			State:      state,
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
	if !connected {
		ctx.Println("Connection: DISCONNECTED")
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
	ctx.Printf("Values read from: %s\n", source)
	ctx.Println()

	// Print state
	ctx.Printf("%s", state.String())

	return nil
}

// init registers the status command
func init() {
	RegisterCommand("status", "Show connection status and current parameter values", statusCommand)
}
