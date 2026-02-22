package cli

import (
	"flag"
	"fmt"

	"rode-dsp/internal/protocol"
)

// bbCommand implements the "bb" command
func bbCommand(args []string) error {
	fs := flag.NewFlagSet("bb", flag.ExitOnError)
	enable := fs.Bool("enable", false, "Enable big bottom effect")
	disable := fs.Bool("disable", false, "Disable big bottom effect")
	drive := fs.Float64("drive", 62.0, "Drive amount in % (0.0 to 100.0)")
	tune := fs.Float64("tune", 131.0, "Tune frequency in Hz (60.0 to 312.0)")
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
		ctx.State.SetEnabled(0x03, true)
	}
	if *disable {
		ctx.State.SetEnabled(0x03, false)
	}

	if set["drive"] {
		if err := ctx.State.SetParam(0x03, 0x01, *drive); err != nil {
			return fmt.Errorf("failed to set drive: %w", err)
		}
	}
	if set["tune"] {
		if err := ctx.State.SetParam(0x03, 0x02, *tune); err != nil {
			return fmt.Errorf("failed to set tune: %w", err)
		}
	}

	if err := ctx.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := ctx.EnsureDeviceConnected(); err == nil {
		fmt.Print("Sending updates to device... ")

		if *enable || *disable {
			packet := protocol.BuildEnablePacket(0x03, ctx.State.IsEnabled(0x03))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send enable packet: %w", err)
			}
		}
		if set["drive"] {
			packet := protocol.BuildSetPacket(0x03, 0x01, protocol.EncodeBBDrive(*drive))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send drive: %w", err)
			}
		}
		if set["tune"] {
			packet := protocol.BuildSetPacket(0x03, 0x02, protocol.EncodeBBTune(*tune))
			if err := ctx.Device.Send(packet); err != nil {
				return fmt.Errorf("failed to send tune: %w", err)
			}
		}

		ctx.Device.Flush()
		fmt.Println("OK")
	} else {
		fmt.Println("Device not connected. Configuration saved.")
	}

	fmt.Println("\nUpdated big bottom state:")
	if ctx.State.IsEnabled(0x03) {
		fmt.Println("  Big Bottom: ENABLED")
	} else {
		fmt.Println("  Big Bottom: DISABLED")
	}
	if val, ok := ctx.State.GetParam(0x03, 0x01); ok {
		fmt.Printf("    Drive: %.1f%%\n", val)
	}
	if val, ok := ctx.State.GetParam(0x03, 0x02); ok {
		fmt.Printf("    Tune:  %.0f Hz\n", val)
	}

	return nil
}

func init() {
	RegisterCommand("bb", "Configure big bottom effect", bbCommand)
}
