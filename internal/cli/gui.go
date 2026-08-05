package cli

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"rode-dsp/internal/server"
)

// guiCommand implements the "gui" command with actual HTTP server
func guiCommand(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("gui", flag.ExitOnError)
	port := fs.Int("port", 8080, "HTTP server port")
	openBrowser := fs.Bool("open", true, "Open browser automatically")
	configPath := fs.String("config", "", "Path to configuration file (default: auto-detect)")
	debug := fs.Bool("debug", false, "Enable debug output")
	quiet := fs.Bool("quiet", false, "Suppress log output (useful when running as a systemd service)")
	fs.BoolVar(quiet, "q", false, "Shorthand for --quiet")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Create context
	ctx := NewContext()
	defer ctx.Close()

	// Override config path if specified
	if *configPath != "" {
		ctx.ConfigPath = *configPath
	}

	// Set debug / quiet mode
	ctx.Debug = *debug
	ctx.Quiet = *quiet
	ctx.Device.SetDebug(*debug)

	// Suppress log output in quiet mode
	if *quiet {
		log.SetOutput(io.Discard)
	}

	// Load configuration
	if err := ctx.LoadConfig(); err != nil {
		log.Printf("Warning: Failed to load config: %v", err)
	}

	// Try to connect to device (non-blocking, just checks availability)
	go func() {
		if err := ctx.EnsureDeviceConnected(); err != nil {
			if *debug {
				log.Printf("Device connection (background): %v", err)
			}
			return
		}
		log.Printf("Device connected. Use 'load' command or GUI controls to apply settings.")
	}()

	// Create HTTP server
	srv := server.NewServer(*port, ctx.ConfigPath, ctx.State, ctx.Device, *debug)

	// Channel to signal server shutdown
	serverErr := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		log.Printf("Starting RODE DSP Web GUI on http://localhost:%d", *port)
		log.Printf("Press Ctrl+C to stop the server")

		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Open browser if requested
	if *openBrowser {
		// Give server a moment to start
		time.Sleep(500 * time.Millisecond)
		openURL(fmt.Sprintf("http://localhost:%d", *port))
	}

	// Wait for interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case <-interrupt:
		log.Println("Shutting down server...")
		log.Println("Server stopped")
	}

	return nil
}

// openURL opens the specified URL in the default browser
func openURL(url string) {
	var cmd *exec.Cmd

	// Try to open browser based on OS. runtime.GOOS is the build target, not
	// the %OS% environment variable, which is unset under most non-cmd shells.
	switch runtime.GOOS {
	case "windows":
		// The empty "" is start's window-title argument; without it start
		// treats a quoted URL as the title and opens nothing.
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		// Try common Linux/Unix commands
		for _, browserCmd := range []string{"xdg-open", "gio", "gnome-open", "kde-open"} {
			path, err := exec.LookPath(browserCmd)
			if err != nil {
				continue
			}
			if browserCmd == "gio" {
				cmd = exec.Command(path, "open", url)
			} else {
				cmd = exec.Command(path, url)
			}
			break
		}
	}

	if cmd != nil {
		if err := cmd.Start(); err != nil {
			log.Printf("Failed to open browser: %v", err)
			log.Printf("Please open %s manually", url)
		}
	} else {
		log.Printf("Please open %s in your browser", url)
	}
}

// init registers the gui command
func init() {
	RegisterCommand("gui", "Start HTTP server with Web GUI", guiCommand)
}
