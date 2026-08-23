package cli

import (
	"flag"
	"fmt"

	"rode-dsp/internal/dsp"
	"rode-dsp/internal/hid"
	"rode-dsp/internal/protocol"
)

// probeEffectsCommand walks a range of effect IDs and reports which ones the
// device actually implements.
//
// RØDE Connect contains seven DSP panels — the four this tool drives plus an
// Equalizer, High Pass Filter and De-Esser it hides for the NT-USB Mini. This
// command answered that question for the NT-USB Mini in August 2026: effect IDs
// 0x04 and up hold nothing. Q5 in docs/re/04-open-questions.md records the
// result; the command stays because it is the test to re-run on other RØDE
// hardware, or after a firmware update.
//
// It does NOT test for an ACK. The device acknowledges every effect ID it is
// sent, including IDs no firmware block backs, so an ACK sweep reports all
// sixteen IDs present and means nothing. What discriminates is whether an ID
// has storage:
//
//   - Read phase (always, read-only): GET each parameter. An implemented block
//     answers with its coefficients; an unimplemented one answers all zeros.
//   - Write phase (--write-probe): write a sentinel to an unregistered ID and
//     read it back. This settles the ambiguous case of a block that exists but
//     happens to be zeroed. It is skipped for the four known effects so a probe
//     can never disturb a working parameter.
func probeEffectsCommand(args []string) error {
	fs := flag.NewFlagSet("probe-effects", flag.ExitOnError)
	maxID := fs.Uint("max-id", 0x0F, "Highest effect ID to probe (inclusive)")
	maxParam := fs.Uint("max-param", 0x07, "Highest parameter ID to read on each effect")
	timeout := fs.Duration("timeout", hid.DefaultTimeout, "Reply wait per packet")
	writeProbe := fs.Bool("write-probe", false,
		"Also write a sentinel to unregistered effect IDs and read it back")
	debug := fs.Bool("debug", false, "Enable debug output")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if *maxID > 0xFF {
		return fmt.Errorf("max-id must be <= 0xff")
	}
	if *maxParam > 0xFF {
		return fmt.Errorf("max-param must be <= 0xff")
	}

	ctx := NewContext()
	defer ctx.Close()

	ctx.Debug = *debug
	ctx.Device.SetDebug(*debug)

	if err := ctx.EnsureDeviceConnected(); err != nil {
		return fmt.Errorf("device not connected: %w", err)
	}

	known := map[byte]string{}
	for id, eff := range dsp.Effects {
		known[id] = eff.Name
	}

	fmt.Printf("Reading effect IDs 0x00-0x%02x, parameters 0x00-0x%02x\n\n", *maxID, *maxParam)

	var live, dead []byte

	for id := 0; id <= int(*maxID); id++ {
		effID := byte(id)

		nonZero := 0
		replies := 0
		for pid := byte(0); pid <= byte(*maxParam); pid++ {
			data, err := ctx.Device.ReadParam(effID, pid, *timeout)
			if err != nil {
				// No reply at all is itself a result, not a failure.
				continue
			}
			replies++
			for _, b := range data {
				if b != 0 {
					nonZero++
					break
				}
			}
		}

		label := known[effID]
		if label == "" {
			label = "(not in registry)"
		}

		switch {
		case replies == 0:
			fmt.Printf("  0x%02x  no reply          %s\n", effID, label)
			dead = append(dead, effID)
		case nonZero > 0:
			fmt.Printf("  0x%02x  %d/%d params hold data  %s\n", effID, nonZero, replies, label)
			live = append(live, effID)
		default:
			fmt.Printf("  0x%02x  all reads zero    %s\n", effID, label)
			dead = append(dead, effID)
		}
	}

	if *writeProbe {
		fmt.Println("\nWrite-readback probe on unregistered IDs:")
		sentinel := []byte{0x78, 0x56, 0x34, 0x12}

		stored := false
		for _, effID := range dead {
			if _, isKnown := known[effID]; isKnown {
				continue // never write to a working effect
			}

			packet := protocol.BuildSetPacket(effID, 0x01, sentinel)
			if _, err := ctx.Device.SendAndAwaitReply(packet, *timeout); err != nil {
				return fmt.Errorf("write probe on effect 0x%02x: %w", effID, err)
			}

			data, err := ctx.Device.ReadParam(effID, 0x01, *timeout)
			if err != nil {
				fmt.Printf("  0x%02x  no reply after write\n", effID)
				continue
			}

			match := len(data) >= 4 &&
				data[0] == sentinel[0] && data[1] == sentinel[1] &&
				data[2] == sentinel[2] && data[3] == sentinel[3]
			if match {
				fmt.Printf("  0x%02x  SENTINEL STORED — this ID has backing storage\n", effID)
				stored = true
			} else {
				fmt.Printf("  0x%02x  discarded (read back % 02x)\n", effID, data[:4])
			}
		}
		if !stored {
			fmt.Println("  No unregistered ID retained a write.")
		}
	}

	fmt.Printf("\n%d effect ID(s) hold data:", len(live))
	for _, id := range live {
		fmt.Printf(" 0x%02x", id)
	}
	fmt.Println()

	var unknown []byte
	for _, id := range live {
		if _, ok := known[id]; !ok {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		fmt.Printf("\nUnregistered effect IDs hold data:")
		for _, id := range unknown {
			fmt.Printf(" 0x%02x", id)
		}
		fmt.Println()
		fmt.Println("The firmware implements DSP blocks this tool does not drive.")
		fmt.Println("Record these in docs/re/04-open-questions.md.")
	} else {
		fmt.Println("\nNo unregistered effect ID holds data.")
		if !*writeProbe {
			fmt.Println("Re-run with --write-probe to rule out a block that exists but reads zero.")
		}
	}

	return nil
}

func init() {
	RegisterAdvancedCommand("probe-effects",
		"Probe effect IDs for undocumented DSP blocks (experimental RE)",
		probeEffectsCommand)
}
