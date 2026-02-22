package cli

import (
	"flag"
	"fmt"
	"time"
)

// readrawCommand implements the "read-raw" command
func readrawCommand(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("read-raw", flag.ExitOnError)
	timeout := fs.Duration("timeout", 100*time.Millisecond, "Read timeout duration")
	count := fs.Int("count", 1, "Number of packets to read (0 for infinite)")
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")
	debug := fs.Bool("debug", false, "Enable debug output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate flags
	if *count < 0 {
		return fmt.Errorf("count must be >= 0")
	}

	// Create context
	ctx := NewContext()
	defer ctx.Close()

	// Override config path if specified
	if *configPath != "" {
		ctx.ConfigPath = *configPath
	}

	// Set debug mode
	ctx.Debug = *debug
	ctx.Device.SetDebug(*debug)

	// Load configuration (though not strictly needed for read-raw)
	if err := ctx.LoadConfig(); err != nil {
		fmt.Printf("Note: Failed to load config: %v\n", err)
	}

	// Ensure device is connected
	if err := ctx.EnsureDeviceConnected(); err != nil {
		return fmt.Errorf("device not connected: %w", err)
	}

	fmt.Println("Experimental: Reading raw data from Report ID 0x03")
	fmt.Println("This is for reverse engineering purposes only.")
	fmt.Println()

	// Read specified number of packets
	packetsRead := 0
	for *count == 0 || packetsRead < *count {
		fmt.Printf("Reading (timeout: %v)... ", *timeout)
		data, err := ctx.Device.ReadRaw(*timeout)
		if err != nil {
			if err.Error() == "read timeout" {
				fmt.Println("TIMEOUT")
			} else {
				fmt.Printf("ERROR: %v\n", err)
			}
		} else {
			fmt.Printf("OK (%d bytes)\n", len(data))
			fmt.Printf("  Hex: ")
			for i, b := range data {
				if i > 0 && i%16 == 0 {
					fmt.Printf("\n       ")
				}
				fmt.Printf("%02x ", b)
			}
			fmt.Println()

			// Try to interpret as a DSP packet
			if len(data) >= 29 && (data[0] == 0x03 || data[0] == 0x04) {
				fmt.Printf("  Looks like Report ID 0x%02x packet:\n", data[0])
				if len(data) >= 4 {
					fmt.Printf("    Effect: 0x%02x, Command: 0x%02x, Param: 0x%02x\n",
						data[1], data[2], data[3])
				}
				if len(data) > 4 {
					fmt.Printf("    Data: %02x", data[4])
					for _, b := range data[5:min(20, len(data))] {
						fmt.Printf(" %02x", b)
					}
					if len(data) > 20 {
						fmt.Printf(" ... (%d more bytes)", len(data)-20)
					}
					fmt.Println()
				}
			}

			// ASCII representation (if printable)
			hasPrintable := false
			for _, b := range data {
				if b >= 32 && b <= 126 {
					hasPrintable = true
					break
				}
			}
			if hasPrintable {
				fmt.Printf("  ASCII: ")
				for _, b := range data {
					if b >= 32 && b <= 126 {
						fmt.Printf("%c", b)
					} else {
						fmt.Printf(".")
					}
				}
				fmt.Println()
			}
		}

		fmt.Println()
		packetsRead++

		// Break if we've read the requested count
		if *count > 0 && packetsRead >= *count {
			break
		}

		// Small delay between reads
		if *count != 1 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	fmt.Printf("Read %d packet(s)\n", packetsRead)
	return nil
}

// init registers the read-raw command
func init() {
	RegisterCommand("read-raw", "Probe Report ID 0x03 (experimental RE)", readrawCommand)
}
