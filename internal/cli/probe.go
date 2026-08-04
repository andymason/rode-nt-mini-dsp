package cli

import (
	"flag"
	"fmt"

	"rode-dsp/internal/dsp"
	"rode-dsp/internal/hid"
	"rode-dsp/internal/protocol"
)

// probeEffectsCommand walks a range of effect IDs sending INIT packets and
// reports which the device acknowledges.
//
// RØDE Connect exposes seven DSP blocks — the four this tool drives plus an
// Equalizer, High Pass Filter and De-Esser that its UI hides for the NT-USB
// Mini. Whether the microphone's firmware implements those three at all is the
// open question that decides how much further reverse engineering is worth
// doing. An ACK for an effect ID beyond 0x03 is evidence it does.
//
// INIT (command 0x03) is the safe probe: it is the same all-zero packet RØDE
// Connect sends to every parameter of every effect during startup, so an
// unknown effect ID receives nothing the device does not already see on every
// launch. No SET is sent and no coefficient is changed.
func probeEffectsCommand(args []string) error {
	fs := flag.NewFlagSet("probe-effects", flag.ExitOnError)
	maxID := fs.Uint("max-id", 0x0F, "Highest effect ID to probe (inclusive)")
	paramID := fs.Uint("param", 0x00, "Parameter ID to probe on each effect")
	timeout := fs.Duration("timeout", hid.DefaultAckTimeout, "ACK wait per packet")
	repeat := fs.Int("repeat", 3, "Probes per effect ID; an ID counts as present if any is acknowledged")
	debug := fs.Bool("debug", false, "Enable debug output")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *maxID > 0xFF {
		return fmt.Errorf("max-id must be <= 0xff")
	}
	if *paramID > 0xFF {
		return fmt.Errorf("param must be <= 0xff")
	}
	if *repeat < 1 {
		return fmt.Errorf("repeat must be >= 1")
	}

	ctx := NewContext()
	defer ctx.Close()

	ctx.Debug = *debug
	ctx.Device.SetDebug(*debug)

	if err := ctx.EnsureDeviceConnected(); err != nil {
		return fmt.Errorf("device not connected: %w", err)
	}

	fmt.Printf("Probing effect IDs 0x00-0x%02x with INIT (cmd 0x%02x, param 0x%02x)\n",
		*maxID, protocol.CmdInit, *paramID)
	fmt.Printf("%d attempt(s) per ID, %v ACK timeout\n\n", *repeat, *timeout)

	known := map[byte]string{}
	for id, eff := range dsp.Effects {
		known[id] = eff.Name
	}

	var acked []byte

	for id := 0; id <= int(*maxID); id++ {
		effID := byte(id)
		packet := protocol.BuildInitPacket(effID, byte(*paramID))

		ok := false
		for attempt := 0; attempt < *repeat && !ok; attempt++ {
			got, err := ctx.Device.SendAndAwaitAck(packet, *timeout)
			if err != nil {
				return fmt.Errorf("probing effect 0x%02x: %w", effID, err)
			}
			ok = got
		}

		label := known[effID]
		switch {
		case ok && label != "":
			fmt.Printf("  0x%02x  ACK      %s\n", effID, label)
		case ok:
			fmt.Printf("  0x%02x  ACK      (not in registry)\n", effID)
		case label != "":
			fmt.Printf("  0x%02x  no ACK   %s  <- expected an ACK\n", effID, label)
		default:
			fmt.Printf("  0x%02x  no ACK\n", effID)
		}

		if ok {
			acked = append(acked, effID)
		}
	}

	fmt.Printf("\n%d of %d effect IDs acknowledged\n", len(acked), int(*maxID)+1)

	// The interesting result is an ACK outside the four known effects.
	var unknown []byte
	for _, id := range acked {
		if _, ok := known[id]; !ok {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		fmt.Printf("\nUnregistered effect IDs acknowledged:")
		for _, id := range unknown {
			fmt.Printf(" 0x%02x", id)
		}
		fmt.Println()
		fmt.Println("The firmware appears to implement DSP blocks this tool does not drive.")
		fmt.Println("Record these in docs/re/04-open-questions.md.")
	} else {
		fmt.Println("\nNo unregistered effect IDs responded.")
		fmt.Println("Note this is weak evidence: the device may simply ignore unknown IDs")
		fmt.Println("without NAKing them. See docs/re/04-open-questions.md.")
	}

	return nil
}

func init() {
	RegisterCommand("probe-effects",
		"Probe effect IDs with INIT packets to find undocumented DSP blocks (experimental RE)",
		probeEffectsCommand)
}
