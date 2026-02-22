package cli

import (
	"flag"
	"fmt"

	"rode-dsp/internal/protocol"
)

// gateCommand implements the "gate" command
func gateCommand(args []string) error {
	fs := flag.NewFlagSet("gate", flag.ExitOnError)
	enable := fs.Bool("enable", false, "Enable noise gate effect")
	disable := fs.Bool("disable", false, "Disable noise gate effect")
	threshold := fs.Float64("threshold", -42.0, "Threshold in dB (-60.0 to 0.0)")
	attack := fs.Float64("attack", 0.8, "Attack time in ms (0.1 to 1000.0)")
	hold := fs.Float64("hold", 80.0, "Hold time in ms (50.0 to 2000.0)")
	release := fs.Float64("release", 210.0, "Release time in ms (50.0 to 2000.0)")
	rangeVal := fs.Float64("range", -9.0, "Range in dB (-100.0 to 0.0)")
	hysteresis := fs.Float64("hysteresis", 50.0, "Hysteresis in % (0.0 to 100.0)")
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")
	debug := fs.Bool("debug", false, "Enable debug output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Track which param flags were explicitly provided
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	// Require at least one non-config/debug flag
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
		ctx.State.SetEnabled(0x01, true)
	}
	if *disable {
		ctx.State.SetEnabled(0x01, false)
	}

	if set["threshold"] {
		if err := ctx.State.SetParam(0x01, 0x01, *threshold); err != nil {
			return fmt.Errorf("failed to set threshold: %w", err)
		}
	}
	if set["attack"] {
		if err := ctx.State.SetParam(0x01, 0x02, *attack); err != nil {
			return fmt.Errorf("failed to set attack: %w", err)
		}
	}
	if set["hold"] {
		if err := ctx.State.SetParam(0x01, 0x03, *hold); err != nil {
			return fmt.Errorf("failed to set hold: %w", err)
		}
	}
	if set["release"] {
		if err := ctx.State.SetParam(0x01, 0x04, *release); err != nil {
			return fmt.Errorf("failed to set release: %w", err)
		}
	}
	if set["range"] {
		if err := ctx.State.SetParam(0x01, 0x05, *rangeVal); err != nil {
			return fmt.Errorf("failed to set range: %w", err)
		}
	}
	if set["hysteresis"] {
		if err := ctx.State.SetParam(0x01, 0x06, *hysteresis); err != nil {
			return fmt.Errorf("failed to set hysteresis: %w", err)
		}
	}

	if err := ctx.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := ctx.EnsureDeviceConnected(); err == nil {
		fmt.Print("Sending updates to device... ")

		if *enable || *disable {
			packet := protocol.BuildEnablePacket(0x01, ctx.State.IsEnabled(0x01))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send enable packet: %w", err)
			}
		}
		if set["threshold"] {
			packet := protocol.BuildSetPacket(0x01, 0x01, protocol.EncodeNGThreshold(*threshold))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send threshold: %w", err)
			}
		}
		if set["attack"] {
			packet := protocol.BuildSetPacket(0x01, 0x02, protocol.EncodeNGAttack(*attack))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send attack: %w", err)
			}
		}
		if set["hold"] {
			packet := protocol.BuildSetPacket(0x01, 0x03, protocol.EncodeNGHold(*hold))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send hold: %w", err)
			}
		}
		if set["release"] {
			packet := protocol.BuildSetPacket(0x01, 0x04, protocol.EncodeNGRelease(*release))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send release: %w", err)
			}
		}
		if set["range"] {
			packet := protocol.BuildSetPacket(0x01, 0x05, protocol.EncodeNGRange(*rangeVal))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send range: %w", err)
			}
		}
		if set["hysteresis"] {
			packet := protocol.BuildSetPacket(0x01, 0x06, protocol.EncodeNGHysteresis(*hysteresis))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send hysteresis: %w", err)
			}
		}

		ctx.Device.Flush()
		fmt.Println("OK")
	} else {
		fmt.Println("Device not connected. Configuration saved.")
	}

	fmt.Println("\nUpdated noise gate state:")
	if ctx.State.IsEnabled(0x01) {
		fmt.Println("  Noise Gate: ENABLED")
	} else {
		fmt.Println("  Noise Gate: DISABLED")
	}
	if val, ok := ctx.State.GetParam(0x01, 0x01); ok {
		fmt.Printf("    Threshold:  %.1f dB\n", val)
	}
	if val, ok := ctx.State.GetParam(0x01, 0x02); ok {
		fmt.Printf("    Attack:     %.2f ms\n", val)
	}
	if val, ok := ctx.State.GetParam(0x01, 0x03); ok {
		fmt.Printf("    Hold:       %.1f ms\n", val)
	}
	if val, ok := ctx.State.GetParam(0x01, 0x04); ok {
		fmt.Printf("    Release:    %.1f ms\n", val)
	}
	if val, ok := ctx.State.GetParam(0x01, 0x05); ok {
		fmt.Printf("    Range:      %.1f dB\n", val)
	}
	if val, ok := ctx.State.GetParam(0x01, 0x06); ok {
		fmt.Printf("    Hysteresis: %.0f%%\n", val)
	}

	return nil
}

func init() {
	RegisterCommand("gate", "Configure noise gate effect", gateCommand)
}
