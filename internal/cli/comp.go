package cli

import (
	"flag"
	"fmt"

	"rode-dsp/internal/protocol"
)

// compCommand implements the "comp" command
func compCommand(args []string) error {
	fs := flag.NewFlagSet("comp", flag.ExitOnError)
	enable := fs.Bool("enable", false, "Enable compressor effect")
	disable := fs.Bool("disable", false, "Disable compressor effect")
	threshold := fs.Float64("threshold", -20.0, "Threshold in dB (-60.0 to 0.0)")
	ratio := fs.Float64("ratio", 3.0, "Compression ratio (1.5 to 4.5:1)")
	attack := fs.Float64("attack", 0.7, "Attack time in ms (0.1 to 10.0)")
	release := fs.Float64("release", 21.0, "Release time in ms (5.0 to 200.0)")
	gain := fs.Float64("gain", 2.0, "Makeup gain in dB (0.0 to 9.0)")
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")
	debug := fs.Bool("debug", false, "Enable debug output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Track which param flags were explicitly provided
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	actionSet := false
	for name := range set {
		if name != "config" && name != "debug" {
			actionSet = true
			break
		}
	}
	if !actionSet {
		return fmt.Errorf("no parameters specified. Use --help for usage")
	}

	ctx := NewContext()
	defer ctx.Close()

	if *configPath != "" {
		ctx.ConfigPath = *configPath
	}
	ctx.Debug = *debug
	ctx.Device.SetDebug(*debug)

	if err := ctx.LoadConfig(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if *enable && *disable {
		return fmt.Errorf("cannot specify both --enable and --disable")
	}
	if *enable {
		ctx.State.SetEnabled(0x00, true)
	}
	if *disable {
		ctx.State.SetEnabled(0x00, false)
	}

	if set["threshold"] {
		if err := ctx.State.SetParam(0x00, 0x01, *threshold); err != nil {
			return fmt.Errorf("failed to set threshold: %w", err)
		}
	}
	if set["ratio"] {
		if err := ctx.State.SetParam(0x00, 0x02, *ratio); err != nil {
			return fmt.Errorf("failed to set ratio: %w", err)
		}
	}
	if set["attack"] {
		if err := ctx.State.SetParam(0x00, 0x03, *attack); err != nil {
			return fmt.Errorf("failed to set attack: %w", err)
		}
	}
	if set["release"] {
		if err := ctx.State.SetParam(0x00, 0x04, *release); err != nil {
			return fmt.Errorf("failed to set release: %w", err)
		}
	}
	if set["gain"] {
		if err := ctx.State.SetParam(0x00, 0x05, *gain); err != nil {
			return fmt.Errorf("failed to set gain: %w", err)
		}
	}

	if err := ctx.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := ctx.EnsureDeviceConnected(); err == nil {
		fmt.Print("Sending updates to device... ")

		if *enable || *disable {
			packet := protocol.BuildEnablePacket(0x00, ctx.State.IsEnabled(0x00))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send enable packet: %w", err)
			}
		}
		if set["threshold"] {
			packet := protocol.BuildSetPacket(0x00, 0x01, protocol.EncodeCompThreshold(*threshold))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send threshold: %w", err)
			}
		}
		if set["ratio"] {
			packet := protocol.BuildSetPacket(0x00, 0x02, protocol.EncodeCompRatio(*ratio))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send ratio: %w", err)
			}
		}
		if set["attack"] {
			packet := protocol.BuildSetPacket(0x00, 0x03, protocol.EncodeCompAttack(*attack))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send attack: %w", err)
			}
		}
		if set["release"] {
			packet := protocol.BuildSetPacket(0x00, 0x04, protocol.EncodeCompRelease(*release))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send release: %w", err)
			}
		}
		if set["gain"] {
			packet := protocol.BuildSetPacket(0x00, 0x05, protocol.EncodeCompGain(*gain))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send gain: %w", err)
			}
		}

		ctx.Device.Flush()
		fmt.Println("OK")
	} else {
		fmt.Println("Device not connected. Configuration saved.")
	}

	fmt.Println("\nUpdated compressor state:")
	if ctx.State.IsEnabled(0x00) {
		fmt.Println("  Compressor: ENABLED")
	} else {
		fmt.Println("  Compressor: DISABLED")
	}
	if val, ok := ctx.State.GetParam(0x00, 0x01); ok {
		fmt.Printf("    Threshold: %.1f dB\n", val)
	}
	if val, ok := ctx.State.GetParam(0x00, 0x02); ok {
		fmt.Printf("    Ratio:     %.1f:1\n", val)
	}
	if val, ok := ctx.State.GetParam(0x00, 0x03); ok {
		fmt.Printf("    Attack:    %.2f ms\n", val)
	}
	if val, ok := ctx.State.GetParam(0x00, 0x04); ok {
		fmt.Printf("    Release:   %.1f ms\n", val)
	}
	if val, ok := ctx.State.GetParam(0x00, 0x05); ok {
		fmt.Printf("    Gain:      %.1f dB\n", val)
	}

	return nil
}

func init() {
	RegisterCommand("comp", "Configure compressor effect", compCommand)
}
