// Package hid talks to the RODE NT-USB Mini over USB HID.
//
// Every exchange is synchronous: write one packet, wait for the device's reply,
// return. The microphone answers in well under a millisecond and a full config
// is twenty packets, so there is nothing here worth making concurrent — and a
// synchronous write is the only kind that can report that it failed.
package hid

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	gohid "github.com/sstallion/go-hid"
	"rode-dsp/internal/dsp"
	"rode-dsp/internal/protocol"
)

// ErrDeviceNotFound means the microphone is not plugged in (or is switched
// off). main turns this into exit code 2, which is how the systemd unit tells
// "no microphone" apart from a real failure.
var ErrDeviceNotFound = errors.New("RODE NT-USB Mini not found")

// DefaultTimeout is how long to wait for a reply that carries data. Reads are
// worth waiting for; a plain acknowledgement is not, so Send uses the much
// shorter protocol.AckTimeoutMs.
const DefaultTimeout = 250 * time.Millisecond

// Device is an open connection to the microphone. It is safe for concurrent
// use: the mutex serialises whole exchanges, so two callers cannot interleave a
// write with someone else's reply.
type Device struct {
	mu    sync.Mutex
	dev   *gohid.Device
	debug bool
}

// NewDevice returns a Device that is not yet connected.
func NewDevice() *Device { return &Device{} }

// hidapi is a process-wide library, so it is initialised once and the outcome
// remembered — including a failure, which must not look like success to the
// second caller.
var (
	initOnce sync.Once
	initErr  error
)

func hidInit() error {
	initOnce.Do(func() {
		if err := gohid.Init(); err != nil {
			initErr = fmt.Errorf("cannot initialise the HID library: %w", err)
		}
	})
	return initErr
}

// Connect opens the microphone.
//
// It looks before it opens so that "not plugged in" and "plugged in but I am
// not allowed to touch it" come back as different errors: the first is a normal
// outcome, the second is a fixable mistake and says how to fix it.
func (d *Device) Connect() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.dev != nil {
		return nil
	}
	if err := hidInit(); err != nil {
		return err
	}

	found := false
	err := gohid.Enumerate(protocol.VID, protocol.PID, func(*gohid.DeviceInfo) error {
		found = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("cannot list USB HID devices: %w", err)
	}
	if !found {
		return fmt.Errorf("%w (looked for USB %04x:%04x)", ErrDeviceNotFound, protocol.VID, protocol.PID)
	}

	dev, err := gohid.OpenFirst(protocol.VID, protocol.PID)
	if err != nil {
		return fmt.Errorf("the microphone is plugged in but could not be opened: %w%s", err, permissionHint())
	}

	d.dev = dev
	d.logf("connected to %04x:%04x", protocol.VID, protocol.PID)
	return nil
}

// permissionHint turns the usual first-run failure into an instruction. On
// Linux this is almost always the missing udev rule.
func permissionHint() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	return "\n  This usually means setup has not run yet. Run:\n" +
		"    sudo rode-dsp setup\n" +
		"  then unplug the microphone and plug it back in."
}

// Disconnect closes the microphone. It is safe to call when not connected.
func (d *Device) Disconnect() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.dev == nil {
		return
	}
	_ = d.dev.Close()
	d.dev = nil
	d.logf("disconnected")
}

// Connected reports whether the microphone is open.
func (d *Device) Connected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dev != nil
}

// SetDebug turns on packet logging, which goes to stderr so it cannot corrupt
// the JSON that `status --json` writes to stdout.
func (d *Device) SetDebug(debug bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.debug = debug
}

// logf writes a debug line. Callers hold the mutex.
func (d *Device) logf(format string, args ...any) {
	if d.debug {
		fmt.Fprintf(os.Stderr, "hid: "+format+"\n", args...)
	}
}

// isReply reports whether a frame is the device answering a packet we sent.
// Report ID and the acknowledgement byte are the only markers it carries.
func isReply(frame []byte) bool {
	return len(frame) >= 3 &&
		(frame[0] == 0x01 || frame[0] == 0x03 || frame[0] == 0x07) &&
		frame[2] == protocol.AckByte
}

// exchange writes one packet and returns the device's reply, or nil if the
// device did not answer within timeout. A nil reply is not an error: the
// microphone does not acknowledge everything, and for a SET it never mattered
// whether it did. An error means the USB write or read itself failed.
//
// For a GET the reply carries the parameter's value, which is the only way to
// observe what the device actually holds.
func (d *Device) exchange(packet [protocol.PacketSize]byte, timeout time.Duration) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.dev == nil {
		return nil, errors.New("device not connected")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	buf := make([]byte, 64)

	// Discard anything the device said before now. A reply we gave up waiting
	// for would otherwise be read as the answer to this packet.
	for {
		if n, err := d.dev.ReadWithTimeout(buf, time.Millisecond); err != nil || n == 0 {
			break
		}
	}

	d.logf("send % 02x", packet[:8])

	n, err := d.dev.Write(packet[:])
	if err != nil {
		return nil, fmt.Errorf("writing to the microphone: %w", err)
	}
	if n != len(packet) {
		return nil, fmt.Errorf("wrote %d of %d bytes to the microphone", n, len(packet))
	}

	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			d.logf("no reply within %v", timeout)
			return nil, nil
		}

		n, err := d.dev.ReadWithTimeout(buf, remaining)
		if errors.Is(err, gohid.ErrTimeout) {
			d.logf("no reply within %v", timeout)
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("reading from the microphone: %w", err)
		}
		if isReply(buf[:n]) {
			d.logf("recv % 02x", buf[:n])
			reply := make([]byte, n)
			copy(reply, buf[:n])
			return reply, nil
		}
	}
}

// Send writes one packet and returns once the device has taken it.
func (d *Device) Send(packet [protocol.PacketSize]byte) error {
	_, err := d.exchange(packet, time.Duration(protocol.AckTimeoutMs)*time.Millisecond)
	return err
}

// SendAndAwaitReply writes one packet and returns the device's whole reply, or
// nil if none arrived. Used by the reverse-engineering commands, where the
// absence of a reply is itself the result.
func (d *Device) SendAndAwaitReply(packet [protocol.PacketSize]byte, timeout time.Duration) ([]byte, error) {
	return d.exchange(packet, timeout)
}

// SendAndAwaitAck writes one packet and reports whether the device answered.
func (d *Device) SendAndAwaitAck(packet [protocol.PacketSize]byte, timeout time.Duration) (bool, error) {
	reply, err := d.exchange(packet, timeout)
	return reply != nil, err
}

// ReadParam reads one parameter and returns the data field of the reply.
func (d *Device) ReadParam(effectID, paramID byte, timeout time.Duration) ([]byte, error) {
	reply, err := d.exchange(protocol.BuildGetPacket(effectID, paramID), timeout)
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, fmt.Errorf("no reply reading effect 0x%02x param 0x%02x", effectID, paramID)
	}
	_, data, err := protocol.ParseResponse(reply)
	if err != nil {
		return nil, fmt.Errorf("reading effect 0x%02x param 0x%02x: %w", effectID, paramID, err)
	}
	return data, nil
}

// effectOrder is the order RODE Connect walks the effects, and the order used
// everywhere here so captures line up.
var effectOrder = []byte{protocol.EffComp, protocol.EffGate, protocol.EffAE, protocol.EffBB}

// ReadState asks the device for every parameter of every effect and returns
// what it actually holds — which is the truth whenever anything else has driven
// the microphone since this tool last wrote to it. See docs/re/02-protocol.md.
func (d *Device) ReadState() (*dsp.DSPState, error) {
	state := dsp.NewDSPState()

	for _, effID := range effectOrder {
		for pid := byte(0); pid < byte(protocol.ParamCounts[effID]); pid++ {
			data, err := d.ReadParam(effID, pid, DefaultTimeout)
			if err != nil {
				return nil, err
			}

			if pid == 0x00 {
				enabled, ok := protocol.DecodeEnabled(data)
				if !ok {
					return nil, fmt.Errorf("effect 0x%02x: empty enable reply", effID)
				}
				state.SetEnabled(effID, enabled)
				continue
			}

			value, ok := protocol.DecodeParam(effID, pid, data)
			if !ok {
				// Aural Exciter param 0x03 has no UI meaning and no decoder; it
				// is read because RODE Connect reads it, not because the value
				// is understood. See Q6 in docs/re/04-open-questions.md.
				continue
			}
			if err := state.SetParam(effID, pid, value); err != nil {
				return nil, fmt.Errorf("effect 0x%02x param 0x%02x: %w", effID, pid, err)
			}
		}
	}

	return state, nil
}

// Apply writes the whole of a state to the microphone: every parameter of every
// effect, then that effect's on/off switch.
//
// Both halves matter. An effect that is off has to be told so — leaving the
// packet out, as an earlier version did, meant `defaults` and `load` could turn
// effects on but never off. And the switch goes last so that an effect being
// enabled is already carrying its new values the moment it starts processing.
func (d *Device) Apply(state *dsp.DSPState) error {
	for _, effID := range effectOrder {
		effDef, ok := dsp.Effects[effID]
		if !ok {
			continue
		}

		for _, param := range effDef.Params {
			value, ok := state.GetParam(effID, param.ParamID)
			if !ok {
				continue
			}
			packet := protocol.BuildSetPacket(effID, param.ParamID, param.EncodeFn(value))
			if err := d.Send(packet); err != nil {
				return fmt.Errorf("%s %s: %w", effDef.Name, param.Name, err)
			}
		}

		if err := d.Send(protocol.BuildEnablePacket(effID, state.IsEnabled(effID))); err != nil {
			return fmt.Errorf("%s on/off: %w", effDef.Name, err)
		}
	}
	return nil
}

// ReadRaw reads one input report, whatever the device happens to send next.
// Experimental, for protocol work only.
func (d *Device) ReadRaw(timeout time.Duration) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.dev == nil {
		return nil, errors.New("device not connected")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	buf := make([]byte, 64)
	n, err := d.dev.ReadWithTimeout(buf, timeout)
	if errors.Is(err, gohid.ErrTimeout) {
		return nil, errors.New("read timeout")
	}
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}
