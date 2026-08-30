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

	fmt.Println("There is nothing to set up on this computer.")
	fmt.Println()
	fmt.Println("Open the settings page whenever you want to change your sound:")
	fmt.Println("  rode-dsp gui")
	fmt.Println()
	fmt.Println("This microphone forgets its settings when it loses power, so run")
	fmt.Println("\"rode-dsp load\" to put them back. RODE Connect can do this for you.")
	return nil
}

// Remove has nothing to undo, and says so.
func Remove(Options) error {
	fmt.Println("There was nothing to remove on this computer.")
	fmt.Println()
	fmt.Println("Delete the rode-dsp program yourself, and its settings folder if you")
	fmt.Println("want those gone too.")
	return nil
}
