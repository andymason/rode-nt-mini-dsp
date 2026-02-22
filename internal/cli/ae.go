package cli

import (
	"flag"
	"fmt"

	"rode-dsp/internal/protocol"
)

// aeCommand implements the "ae" command
func aeCommand(args []string) error {
	fs := flag.NewFlagSet("ae", flag.ExitOnError)
	enable := fs.Bool("enable", false, "Enable aural exciter effect")
	disable := fs.Bool("disable", false, "Disable aural exciter effect")
	harmonics := fs.Float64("harmonics", 49.0, "Harmonics amount in % (0.0 to 100.0)")
	tune := fs.Float64("tune", 3516.0, "Tune frequency in Hz (600.0 to 5000.0)")
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")
	debug := fs.Bool("debug", false, "Enable debug output")

	if err := fs.Parse(args); err != nil {
		return err
	}

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
		ctx.State.SetEnabled(0x02, true)
	}
	if *disable {
		ctx.State.SetEnabled(0x02, false)
	}

	if set["harmonics"] {
		if err := ctx.State.SetParam(0x02, 0x01, *harmonics); err != nil {
			return fmt.Errorf("failed to set harmonics: %w", err)
		}
	}
	if set["tune"] {
		if err := ctx.State.SetParam(0x02, 0x02, *tune); err != nil {
			return fmt.Errorf("failed to set tune: %w", err)
		}
	}

	if err := ctx.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := ctx.EnsureDeviceConnected(); err == nil {
		fmt.Print("Sending updates to device... ")

		if *enable || *disable {
			packet := protocol.BuildEnablePacket(0x02, ctx.State.IsEnabled(0x02))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send enable packet: %w", err)
			}
		}
		if set["harmonics"] {
			packet := protocol.BuildSetPacket(0x02, 0x01, protocol.EncodeAEHarmonics(*harmonics))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send harmonics: %w", err)
			}
		}
		if set["tune"] {
			packet := protocol.BuildSetPacket(0x02, 0x02, protocol.EncodeAETune(*tune))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send tune: %w", err)
			}
		}

		ctx.Device.Flush()
		fmt.Println("OK")
	} else {
		fmt.Println("Device not connected. Configuration saved.")
	}

	fmt.Println("\nUpdated aural exciter state:")
	if ctx.State.IsEnabled(0x02) {
		fmt.Println("  Aural Exciter: ENABLED")
	} else {
		fmt.Println("  Aural Exciter: DISABLED")
	}
	if val, ok := ctx.State.GetParam(0x02, 0x01); ok {
		fmt.Printf("    Harmonics: %.1f%%\n", val)
	}
	if val, ok := ctx.State.GetParam(0x02, 0x02); ok {
		fmt.Printf("    Tune:      %.0f Hz\n", val)
	}

	return nil
}

func init() {
	RegisterCommand("ae", "Configure aural exciter effect", aeCommand)
}
