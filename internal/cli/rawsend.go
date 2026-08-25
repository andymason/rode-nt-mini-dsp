package cli

import (
	"encoding/hex"
	"flag"
	"fmt"
	"strings"

	"rode-dsp/internal/hid"
	"rode-dsp/internal/protocol"
)

// sendRawCommand writes an arbitrary HID report to the device.
//
// This is a reverse-engineering tool: it exists to replay packets observed in a
// USB capture, or to try parameter and effect IDs the registry does not know
// about. It performs no encoding and no validation beyond the packet length, so
// it will happily send coefficients the device has never seen.
//
// Guarded behind --i-know-what-this-does. The realistic worst case is garbled
// audio until the DSP is re-initialised, which `rode-dsp defaults` or a
// replug will fix, but an unknown effect ID reaching firmware is not something
// this tool can reason about.
func sendRawCommand(args []string) error {
	fs := flag.NewFlagSet("send-raw", flag.ExitOnError)
	confirm := fs.Bool("i-know-what-this-does", false,
		"Required. Sends unvalidated bytes to the microphone's DSP.")
	timeout := fs.Duration("timeout", hid.DefaultTimeout, "ACK wait")
	padTo := fs.Bool("pad", true, "Zero-pad short input to the full packet length")
	debug := fs.Bool("debug", false, "Enable debug output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("usage: rode-dsp send-raw --i-know-what-this-does <hex bytes>\n" +
			"  e.g. send-raw --i-know-what-this-does 04 00 02 01 00 00 8f 25")
	}
	if !*confirm {
		return fmt.Errorf("refusing to send unvalidated bytes without --i-know-what-this-does")
	}

	// Accept "04 00 02", "0400 02" and "040002" alike.
	joined := strings.ReplaceAll(strings.Join(rest, ""), "0x", "")
	raw, err := hex.DecodeString(joined)
	if err != nil {
		return fmt.Errorf("parsing hex: %w", err)
	}

	if len(raw) > protocol.PacketSize {
		return fmt.Errorf("input is %d bytes, packet is %d", len(raw), protocol.PacketSize)
	}
	if len(raw) < protocol.PacketSize && !*padTo {
		return fmt.Errorf("input is %d bytes, packet is %d (use --pad to zero-fill)",
			len(raw), protocol.PacketSize)
	}

	var packet [protocol.PacketSize]byte
	copy(packet[:], raw)

	if packet[0] != protocol.ReportID {
		fmt.Printf("Note: byte 0 is 0x%02x, not the usual report ID 0x%02x\n",
			packet[0], protocol.ReportID)
	}

	ctx := NewContext()
	defer ctx.Close()

	ctx.Debug = *debug

	if err := ctx.EnsureDeviceConnected(); err != nil {
		return fmt.Errorf("device not connected: %w", err)
	}

	fmt.Printf("Sending: % 02x\n", packet[:])

	acked, err := ctx.Device.SendAndAwaitAck(packet, *timeout)
	if err != nil {
		return fmt.Errorf("send failed: %w", err)
	}
	if acked {
		fmt.Println("Device acknowledged.")
	} else {
		fmt.Printf("No ACK within %v.\n", *timeout)
	}
	return nil
}

func init() {
	RegisterAdvancedCommand("send-raw",
		"Send a raw HID report to the device (experimental RE; requires confirmation)",
		sendRawCommand)
}
