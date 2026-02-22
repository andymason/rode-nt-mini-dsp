package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"rode-dsp/internal/cli"
	"rode-dsp/internal/hid"
)

func main() {
	// If no arguments, show brief info and usage
	if len(os.Args) < 2 {
		cli.BriefStatus()
		cli.Usage()
		os.Exit(0)
	}

	// Handle --help flag
	if os.Args[1] == "--help" || os.Args[1] == "-h" {
		cli.Usage()
		os.Exit(0)
	}

	// Handle --version flag
	if os.Args[1] == "--version" || os.Args[1] == "-v" {
		fmt.Printf("rode-dsp v0.1.0\n")
		fmt.Printf("Cross-platform CLI for RODE NT-USB Mini DSP control\n")
		os.Exit(0)
	}

	// Dispatch to appropriate command
	if err := cli.Dispatch(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		// Exit 2 when the device is not found so shell scripts can distinguish
		// "microphone not plugged in" from other failures:
		//   if rode-dsp load --quiet; then …
		//   elif [ $? -eq 2 ]; then echo "mic not connected, skipping"; fi
		if errors.Is(err, hid.ErrDeviceNotFound) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// init ensures the binary name is correct in usage
func init() {
	// Update binary name for usage
	if len(os.Args) > 0 {
		// Use just the basename of the executable
		os.Args[0] = filepath.Base(os.Args[0])
	}
}
