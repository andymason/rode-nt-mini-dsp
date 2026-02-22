package cli

import (
	"os"
	"path/filepath"

	"rode-dsp/internal/dsp"
	"rode-dsp/internal/hid"
)

// Context holds shared state for CLI commands
type Context struct {
	// Device is the HID device interface
	Device *hid.Device

	// ConfigPath is the path to the configuration file
	ConfigPath string

	// Debug enables debug output
	Debug bool

	// State holds the current DSP parameter state
	State *dsp.DSPState

	// deviceInitialized tracks whether we've attempted to initialize the device
	deviceInitialized bool
}

// NewContext creates a new CLI context with default values
func NewContext() *Context {
	// Determine default config path
	var configPath string
	if home, err := os.UserHomeDir(); err == nil {
		// Check for config in home directory first
		homeConfig := filepath.Join(home, ".rode-dsp", "config.json")
		if _, err := os.Stat(homeConfig); err == nil {
			configPath = homeConfig
		}
	}

	// If no home config, use current directory default
	if configPath == "" {
		configPath = dsp.DefaultConfigFile
	}

	return &Context{
		Device:     hid.NewDevice(),
		ConfigPath: configPath,
		Debug:      false,
		State:      dsp.NewDSPState(),
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

// Close cleans up resources
func (c *Context) Close() {
	if c.Device != nil {
		c.Device.Disconnect()
	}
}
