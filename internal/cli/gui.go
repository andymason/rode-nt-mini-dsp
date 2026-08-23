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

	"rode-dsp/internal/server"
)

// guiCommand serves the web GUI on localhost until interrupted.
func guiCommand(args []string) error {
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
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	ctx := NewContext()
	defer ctx.Close()

	if *configPath != "" {
		ctx.ConfigPath = *configPath
	}
	ctx.Debug = *debug
	ctx.Quiet = *quiet
	ctx.Device.SetDebug(*debug)

	if *quiet {
		log.SetOutput(io.Discard)
	}

	if err := ctx.LoadConfig(); err != nil {
		log.Printf("Warning: Failed to load config: %v", err)
	}

	// A missing microphone is not a reason to refuse to start: the GUI is still
	// useful for editing settings, and it picks the device up on reload.
	if err := ctx.EnsureDeviceConnected(); err != nil {
		log.Printf("Microphone not connected (%v). The GUI will still open.", err)
	}

	srv := server.NewServer(*port, ctx.ConfigPath, ctx.State, ctx.Device, *debug)

	// Bind before opening the browser, so the page is always there when it
	// arrives. This used to be a hopeful half-second sleep.
	ln, err := srv.Listen()
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s", ln.Addr())
	log.Printf("Web GUI at %s — press Ctrl+C to stop", url)

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	if *openBrowser {
		openURL(url)
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case <-interrupt:
		log.Println("Stopping.")
		ln.Close()
	}

	return nil
}

// openURL opens the specified URL in the default browser.
func openURL(url string) {
	var cmd *exec.Cmd

	// runtime.GOOS is the build target, not the %OS% environment variable,
	// which is unset under most non-cmd shells.
	switch runtime.GOOS {
	case "windows":
		// The empty "" is start's window-title argument; without it start
		// treats a quoted URL as the title and opens nothing.
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
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

	if cmd == nil {
		log.Printf("Please open %s in your browser", url)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Could not open a browser (%v). Please open %s yourself.", err, url)
	}
}

func init() {
	RegisterCommand("gui", "Start the web GUI on localhost", guiCommand)
}
