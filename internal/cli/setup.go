package cli

import "rode-dsp/internal/setup"

func init() {
	RegisterCommand("setup",
		"Install, and reapply settings at startup",
		setup.Run)
}
