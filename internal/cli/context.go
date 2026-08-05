package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"rode-dsp/internal/dsp"
	"rode-dsp/internal/hid"
)

// Context holds shared state for CLI commands
type Context struct {
	// Device is the HID device interface
	Device *hid.Device

	// ConfigPath is the configuration file to use. Empty means the per-user
	// default, resolved when the file is actually read or written so that a
	// missing home directory is reported as an error rather than guessed at.
	ConfigPath string

	// Debug enables debug output
	Debug bool

	// Quiet suppresses progress output to stdout. Errors are still written to
	// stderr. Use this in boot scripts or cron jobs where stdout is redirected.
	Quiet bool

	// State holds the current DSP parameter state
	State *dsp.DSPState

	// deviceInitialized tracks whether we've attempted to initialize the device
	deviceInitialized bool
}

// NewContext creates a new CLI context with default values
func NewContext() *Context {
	return &Context{
		Device: hid.NewDevice(),
		Debug:  false,
		Quiet:  false,
		State:  dsp.NewDSPState(),
	}
}

// DisplayConfigPath is the config file in use, for messages. It resolves the
// default and makes the path absolute, so output names a file the user can go
// and look at.
func (c *Context) DisplayConfigPath() string {
	path, err := dsp.ConfigPath(c.ConfigPath)
	if err != nil {
		return "(unknown)"
	}
	return path
}

// Printf prints a formatted message to stdout unless Quiet is set.
// Use this for progress/status output within commands.
func (c *Context) Printf(format string, args ...interface{}) {
	if !c.Quiet {
		fmt.Printf(format, args...)
	}
}

// Println prints a message to stdout unless Quiet is set.
func (c *Context) Println(args ...interface{}) {
	if !c.Quiet {
		fmt.Println(args...)
	}
}

// GetDevice returns the device, connecting if necessary
func (c *Context) GetDevice() (*hid.Device, error) {
	if c.deviceInitialized {
		return c.Device, nil
	}

	// Set debug mode
	c.Device.SetDebug(c.Debug)

	// Try to connect
	if err := c.Device.Connect(); err != nil {
		return nil, err
	}

	c.deviceInitialized = true
	return c.Device, nil
}

// LoadConfig loads the configuration file into the state
func (c *Context) LoadConfig() error {
	state, err := dsp.LoadConfig(c.ConfigPath)
	if err != nil {
		return err
	}
	c.State = state
	return nil
}

// SaveConfig saves the current state to the configuration file
func (c *Context) SaveConfig() error {
	return dsp.SaveConfig(c.ConfigPath, c.State)
}

// EnsureDeviceConnected ensures the device is connected and initialized
func (c *Context) EnsureDeviceConnected() error {
	_, err := c.GetDevice()
	return err
}

// EnsureDeviceConnectedWithWait tries to connect, retrying every second until
// the device appears or the wait duration expires. Only hid.ErrDeviceNotFound
// triggers a retry; other errors (e.g. permission denied) are returned
// immediately. Pass wait=0 to skip retry logic entirely.
//
// This is the key flag for Linux boot scripts: the USB HID device may not be
// enumerated by the kernel at the instant the script runs. A wait of 10–30 s
// covers the typical desktop startup window.
func (c *Context) EnsureDeviceConnectedWithWait(wait time.Duration) error {
	if wait <= 0 {
		return c.EnsureDeviceConnected()
	}

	deadline := time.Now().Add(wait)
	for {
		err := c.EnsureDeviceConnected()
		if err == nil {
			return nil
		}
		if !errors.Is(err, hid.ErrDeviceNotFound) {
			// Non-retryable error (e.g. permission denied, HID init failure)
			return err
		}
		if time.Now().After(deadline) {
			return err
		}
		if !c.Quiet {
			fmt.Fprintf(os.Stderr, "Device not found, waiting...\n")
		}
		// Reset deviceInitialized so the next loop iteration retries Connect()
		c.deviceInitialized = false
		time.Sleep(time.Second)
	}
}

// Close cleans up resources
func (c *Context) Close() {
	if c.Device != nil {
		c.Device.Disconnect()
	}
}
