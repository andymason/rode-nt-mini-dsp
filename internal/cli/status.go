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
	std := addStdFlags(fs)
	asJSON := fs.Bool("json", false, "Output DSP state as JSON (machine-readable, useful in scripts)")
	fromConfig := fs.Bool("config-only", false,
		"Show the local config instead of querying the device")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	ctx := std.context()
	defer ctx.Close()

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
			ConfigFile: ctx.DisplayConfigPath(),
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
		ctx.Printf("Config file: %s (loaded)\n", ctx.DisplayConfigPath())
	} else {
		ctx.Printf("Config file: %s (not found, using defaults)\n", ctx.DisplayConfigPath())
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
	RegisterCommand("status", "Report the microphone's current values", statusCommand)
}
