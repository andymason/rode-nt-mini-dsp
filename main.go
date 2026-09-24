// Command rode-dsp controls the RODE NT-USB Mini's onboard DSP.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"rode-dsp/internal/cli"
	"rode-dsp/internal/hid"
)

func main() {
	// Usage messages should say "rode-dsp", not the path it was invoked by.
	if len(os.Args) > 0 {
		os.Args[0] = filepath.Base(os.Args[0])
	}

	if len(os.Args) < 2 {
		cli.BriefStatus()
		cli.Usage()
		return
	}

	switch os.Args[1] {
	case "--help", "-h", "help":
		cli.Usage()
		return
	case "--version", "-v", "version":
		fmt.Printf("rode-dsp %s\n", version())
		return
	}

	if err := cli.Dispatch(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		// Exit 2 when the microphone is not plugged in, so scripts and the
		// systemd unit can tell that apart from a real failure:
		//   if rode-dsp load --quiet; then :
		//   elif [ $? -eq 2 ]; then echo "mic not connected, skipping"; fi
		if errors.Is(err, hid.ErrDeviceNotFound) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// versionTag is stamped in by the release workflow with -ldflags. It is empty
// for an ordinary `go build`, which falls back to the build info below.
var versionTag string

// version reports the version stamped in by the Go toolchain — a tag for
// `go install`, otherwise the commit. Nothing to remember to bump by hand.
func version() string {
	if versionTag != "" {
		return versionTag
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			return s.Value[:12]
		}
	}
	return "(devel)"
}
