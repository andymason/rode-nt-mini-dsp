package cli

// effects.go registers all four DSP effect commands (comp, gate, ae, bb) using a
// single generic handler driven by the dsp.Effects registry. Adding a new effect
// only requires an entry in dsp.Effects — no new command file is needed.

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"rode-dsp/internal/dsp"
	"rode-dsp/internal/protocol"
)

// makeEffectCommand returns a CLI handler for the given effect ID. The handler
// derives all flag names, defaults, and ranges from the dsp.Effects registry,
// so it stays in sync automatically when effect definitions change.
func makeEffectCommand(cmdName string, effID byte) func([]string) error {
	return func(args []string) error {
		effDef, ok := dsp.Effects[effID]
		if !ok {
			return fmt.Errorf("unknown effect 0x%02x", effID)
		}

		fs := flag.NewFlagSet(cmdName, flag.ExitOnError)
		enable := fs.Bool("enable", false, "Enable "+effDef.Name+" effect")
		disable := fs.Bool("disable", false, "Disable "+effDef.Name+" effect")
		std := addStdFlags(fs)

		// Register a float64 flag for every parameter, keyed by lowercase name.
		paramPtrs := make(map[string]*float64, len(effDef.Params))
		for _, p := range effDef.Params {
			key := strings.ToLower(p.Name)
			help := fmt.Sprintf("%s in %s (%.4g to %.4g)", p.Name, p.Unit, p.UIMin, p.UIMax)
			paramPtrs[key] = fs.Float64(key, p.Default, help)
		}

		if err := fs.Parse(args); err != nil {
			return err
		}
		if err := noExtraArgs(fs); err != nil {
			return err
		}

		// Collect the flags that were explicitly provided by the user.
		set := make(map[string]bool)
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

		// Require at least one meaningful flag (not just --config/--debug/--quiet).
		actionSet := false
		for name := range set {
			if name != "config" && name != "debug" && name != "quiet" && name != "q" {
				actionSet = true
				break
			}
		}
		if !actionSet {
			return errors.New("no parameters specified; use --help for usage")
		}

		ctx := std.context()
		defer ctx.Close()

		if err := ctx.LoadConfig(); err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Apply enable / disable.
		if *enable && *disable {
			return errors.New("cannot specify both --enable and --disable")
		}
		if *enable {
			ctx.State.SetEnabled(effID, true)
		}
		if *disable {
			ctx.State.SetEnabled(effID, false)
		}

		// Apply parameter updates for every explicitly-set flag.
		for _, p := range effDef.Params {
			key := strings.ToLower(p.Name)
			if !set[key] {
				continue
			}
			if err := ctx.State.SetParam(effID, p.ParamID, *paramPtrs[key]); err != nil {
				return fmt.Errorf("failed to set %s: %w", p.Name, err)
			}
		}

		if err := ctx.SaveConfig(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		// Send updates to the device when it is connected.
		if err := ctx.EnsureDeviceConnected(); err == nil {
			ctx.Printf("Sending updates to device... ")

			if *enable || *disable {
				packet := protocol.BuildEnablePacket(effID, ctx.State.IsEnabled(effID))
				if err := ctx.Device.Send(packet); err != nil {
					return fmt.Errorf("failed to send enable packet: %w", err)
				}
			}

			for _, p := range effDef.Params {
				key := strings.ToLower(p.Name)
				if !set[key] {
					continue
				}
				encoded := p.EncodeFn(*paramPtrs[key])
				packet := protocol.BuildSetPacket(effID, p.ParamID, encoded)
				if err := ctx.Device.Send(packet); err != nil {
					return fmt.Errorf("failed to send %s: %w", p.Name, err)
				}
			}

			ctx.Println("OK")
		} else {
			ctx.Println("Device not connected. Configuration saved.")
		}

		// Print the updated state for this effect.
		enabledStr := "DISABLED"
		if ctx.State.IsEnabled(effID) {
			enabledStr = "ENABLED"
		}
		ctx.Printf("\nUpdated %s state:\n", effDef.Name)
		ctx.Printf("  %s: %s\n", effDef.Name, enabledStr)
		for _, p := range effDef.Params {
			if val, ok := ctx.State.GetParam(effID, p.ParamID); ok {
				ctx.Printf("    %-14s %s\n", p.Name+":", p.FormatFn(val))
			}
		}

		return nil
	}
}

func init() {
	RegisterCommand("comp", "Compressor: even out loud and quiet passages", makeEffectCommand("comp", protocol.EffComp))
	RegisterCommand("gate", "Noise gate: attenuate background sound", makeEffectCommand("gate", protocol.EffGate))
	RegisterCommand("ae", "Aural exciter: add clarity", makeEffectCommand("ae", protocol.EffAE))
	RegisterCommand("bb", "Big bottom: add low-end weight", makeEffectCommand("bb", protocol.EffBB))
}
