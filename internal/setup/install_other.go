//go:build !linux

package setup

import (
	"fmt"
	"os"
)

// Apply explains that there is nothing to set up here.
//
// Linux needs a rule before you can reach the microphone, and a service to send
// your settings back after a restart. macOS and Windows need neither: the
// microphone is readable as it is, and RODE Connect already runs on both.
func Apply(o Options) error {
	if o.DryRun {
		self, err := selfPath()
		if err != nil {
			return err
		}
		return printPlan(os.Stdout, o, self)
	}

	fmt.Println("Nothing needs to be set up on this computer.")
	fmt.Println()
	fmt.Println("Run \"rode-dsp gui\" to change settings.")
	fmt.Println()
	fmt.Println("The microphone loses its settings when it loses power. Run \"rode-dsp load\"")
	fmt.Println("to send them again, or use RODE Connect, which does this automatically.")
	return nil
}

// Remove has nothing to undo, and says so.
func Remove(Options) error {
	fmt.Println("Nothing was installed on this computer, so nothing was removed.")
	fmt.Println()
	fmt.Println("Delete the rode-dsp program manually, and its settings folder to remove")
	fmt.Println("saved settings too.")
	return nil
}
