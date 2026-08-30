package cli

import "rode-dsp/internal/setup"

func init() {
	RegisterCommand("setup",
		"Set this up, and keep your settings after a restart",
		setup.Run)
}
