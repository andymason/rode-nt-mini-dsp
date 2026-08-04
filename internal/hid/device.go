package hid

import (
	"errors"
	"fmt"
	"sync"
	"time"

	gohid "github.com/sstallion/go-hid"
	"rode-dsp/internal/dsp"
	"rode-dsp/internal/protocol"
)

// ErrDeviceNotFound is returned when the RODE NT-USB Mini is not detected on USB.
// Scripts can use this to distinguish a missing device from other failures
// (e.g. check for exit code 2 in shell scripts).
var ErrDeviceNotFound = errors.New("RODE NT-USB Mini not found")

// Device manages USB HID communication with the RODE NT-USB Mini
type Device struct {
	mu         sync.RWMutex
	device     *gohid.Device
	connected  bool
	sendQueue  chan [protocol.PacketSize]byte
	ackChan    chan []byte
	done       chan struct{}
	readDone   chan struct{}
	workerDone chan struct{}
	debug      bool
	initOnce   sync.Once
}

// NewDevice creates a new Device instance
func NewDevice() *Device {
	return &Device{
		sendQueue:  make(chan [protocol.PacketSize]byte, 100),
		ackChan:    make(chan []byte, 1),
		done:       make(chan struct{}),
		readDone:   make(chan struct{}),
		workerDone: make(chan struct{}),
	}
}

// Connect opens the HID device and starts communication goroutines
func (d *Device) Connect() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.connected {
		return nil // Already connected
	}

	// Initialize HID library once
	var initErr error
	d.initOnce.Do(func() {
		if err := gohid.Init(); err != nil {
			initErr = fmt.Errorf("failed to initialize HID library: %w", err)
		}
	})
	if initErr != nil {
		return initErr
	}

	// Enumerate devices with our VID/PID
	if d.debug {
		fmt.Printf("Enumerating HID devices with VID %04x PID %04x\n", protocol.VID, protocol.PID)
	}

	deviceCount := 0
	err := gohid.Enumerate(protocol.VID, protocol.PID, func(info *gohid.DeviceInfo) error {
		deviceCount++
		if d.debug {
			fmt.Printf("  Device %d: VID=%04x PID=%04x Path=%s\n",
				deviceCount, info.VendorID, info.ProductID, info.Path)
			fmt.Printf("    Manufacturer: %s\n", info.MfrStr)
			fmt.Printf("    Product: %s\n", info.ProductStr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to enumerate HID devices: %w", err)
	}

	if deviceCount == 0 {
		return fmt.Errorf("%w (VID %04x PID %04x)", ErrDeviceNotFound, protocol.VID, protocol.PID)
	}

	// Open the first matching device
	if d.debug {
		fmt.Printf("Opening first matching device...\n")
	}
	device, err := gohid.OpenFirst(protocol.VID, protocol.PID)
	if err != nil {
		return fmt.Errorf("failed to open HID device: %w", err)
	}

	d.device = device
	d.connected = true

	// Start worker and read goroutines
	go d.workerLoop()
	go d.readLoop()

	if d.debug {
		fmt.Printf("Connected to HID device %04x:%04x\n", protocol.VID, protocol.PID)
	}
	return nil
}

// Disconnect closes the HID device and stops goroutines
func (d *Device) Disconnect() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.connected {
		return
	}

	// Signal goroutines to stop
	close(d.done)

	// Give goroutines a moment to finish, but don't wait long
	time.Sleep(20 * time.Millisecond)

	// Close device (this will cause Read/Write to fail immediately)
	if d.device != nil {
		_ = d.device.Close()
		d.device = nil
	}

	d.connected = false

	// Reset channels for potential reconnection
	d.done = make(chan struct{})
	d.workerDone = make(chan struct{})
	d.readDone = make(chan struct{})

	if d.debug {
		fmt.Println("Disconnected from HID device")
	}
}

// Connected returns whether the device is currently connected
func (d *Device) Connected() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.connected
}

// Send sends a packet to the device (asynchronous, queued)
func (d *Device) Send(packet [protocol.PacketSize]byte) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if !d.connected {
		return errors.New("device not connected")
	}

	select {
	case d.sendQueue <- packet:
		return nil
	case <-time.After(100 * time.Millisecond):
		return errors.New("send queue full, device may be busy")
	}
}

// Flush waits for the send queue to be emptied (all packets sent)
func (d *Device) Flush() {
	// Wait for queue to empty (with timeout)
	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return
		case <-ticker.C:
			if len(d.sendQueue) == 0 {
				// Queue empty, wait a bit more for last packet to complete
				time.Sleep(50 * time.Millisecond)
				return
			}
		}
	}
}

// sendPacketSync sends a packet and waits for ACK (internal, called from worker)
func (d *Device) sendPacketSync(packet [protocol.PacketSize]byte) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if !d.connected || d.device == nil {
		return false
	}

	if d.debug {
		fmt.Printf("SEND: %02x %02x %02x %02x %02x %02x... (full: % 02x)\n",
			packet[0], packet[1], packet[2], packet[3], packet[4], packet[5], packet[:])
	}

	// Write packet
	n, err := d.device.Write(packet[:])
	if err != nil {
		if d.debug {
			fmt.Printf("Write error: %v\n", err)
		}
		return false
	}
	if d.debug {
		fmt.Printf("Write successful: %d bytes\n", n)
	}
	if n != len(packet) {
		if d.debug {
			fmt.Printf("Write incomplete: %d/%d bytes\n", n, len(packet))
		}
		return false
	}

	// Wait for ACK with timeout
	timeout := time.After(time.Duration(protocol.AckTimeoutMs) * time.Millisecond)
	select {
	case <-d.ackChan:
		return true
	case <-timeout:
		if d.debug {
			fmt.Println("ACK timeout, assuming success")
		}
		return true // Assume success like Python code
	}
}

// DefaultAckTimeout is the ACK wait used by SendAndAwaitAck. RØDE Connect
// allows 500ms; 250ms is comfortably above observed device latency while
// keeping a 16-ID probe sweep responsive.
const DefaultAckTimeout = 250 * time.Millisecond

// SendAndAwaitAck writes one packet and reports whether the device acknowledged
// it, distinguishing an ACK from silence.
//
// This exists because sendPacketSync cannot: on timeout it returns true
// ("assuming success"), which is fine for fire-and-forget parameter updates but
// useless for probing, where the absence of an ACK is the signal. It bypasses
// sendQueue/workerLoop so nothing else can interleave, and drains the
// capacity-1 ackChan first — a late ACK from a previous packet would otherwise
// be misattributed to this one.
//
// A false return means the write succeeded but no ACK arrived within timeout.
// An error means the write itself failed.
func (d *Device) SendAndAwaitAck(packet [protocol.PacketSize]byte, timeout time.Duration) (bool, error) {
	reply, err := d.SendAndAwaitReply(packet, timeout)
	return reply != nil, err
}

// SendAndAwaitReply writes one packet and returns the device's whole reply, or
// nil if none arrived within timeout.
//
// For a SET the reply is just the ACK; for a GET (CmdGet, CmdGetAll) it also
// carries the parameter data, which is the only way to observe device state.
// Callers hand the reply to protocol.ParseResponse.
func (d *Device) SendAndAwaitReply(packet [protocol.PacketSize]byte, timeout time.Duration) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if !d.connected || d.device == nil {
		return nil, errors.New("device not connected")
	}
	if timeout <= 0 {
		timeout = DefaultAckTimeout
	}

	// Discard any ACK left over from an earlier packet.
	select {
	case <-d.ackChan:
	default:
	}

	if d.debug {
		fmt.Printf("PROBE SEND: % 02x\n", packet[:8])
	}

	n, err := d.device.Write(packet[:])
	if err != nil {
		return nil, fmt.Errorf("write failed: %w", err)
	}
	if n != len(packet) {
		return nil, fmt.Errorf("short write: %d of %d bytes", n, len(packet))
	}

	select {
	case reply := <-d.ackChan:
		return reply, nil
	case <-time.After(timeout):
		return nil, nil
	}
}

// ReadParam reads one parameter and returns the data field of the reply.
func (d *Device) ReadParam(effectID, paramID byte, timeout time.Duration) ([]byte, error) {
	reply, err := d.SendAndAwaitReply(protocol.BuildGetPacket(effectID, paramID), timeout)
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

// ReadState queries the device for every parameter of every effect and returns
// the state it actually holds.
//
// This is the sequence RØDE Connect performs at startup — 20 CmdGet packets in
// effect order — which earlier analysis mistook for an INIT/CLEAR because the
// requests carry an all-zero value field. The device answers each with the live
// coefficient. See docs/re/02-protocol.md.
func (d *Device) ReadState() (*dsp.DSPState, error) {
	if !d.Connected() {
		return nil, errors.New("device not connected")
	}

	state := dsp.NewDSPState()
	effectOrder := []byte{protocol.EffComp, protocol.EffGate, protocol.EffAE, protocol.EffBB}

	for _, effID := range effectOrder {
		for pid := byte(0); pid < byte(protocol.ParamCounts[effID]); pid++ {
			data, err := d.ReadParam(effID, pid, DefaultAckTimeout)
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
				// Aural Exciter param 0x03 has no UI meaning and no decoder;
				// it is read because RØDE Connect reads it, not because the
				// value is understood. See Q6 in docs/re/04-open-questions.md.
				continue
			}
			if err := state.SetParam(effID, pid, value); err != nil {
				return nil, fmt.Errorf("effect 0x%02x param 0x%02x: %w", effID, pid, err)
			}
		}
	}

	return state, nil
}

// workerLoop processes packets from the send queue
func (d *Device) workerLoop() {
	defer close(d.workerDone)

	for {
		select {
		case <-d.done:
			return
		case packet := <-d.sendQueue:
			d.sendPacketSync(packet)
		}
	}
}

// readLoop reads from the device looking for ACK bytes
func (d *Device) readLoop() {
	defer close(d.readDone)

	buf := make([]byte, 64)
	for {
		select {
		case <-d.done:
			return
		default:
			d.mu.RLock()
			dev := d.device
			debug := d.debug
			d.mu.RUnlock()

			if dev == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Try to read with short timeout
			n, err := dev.ReadWithTimeout(buf, 10*time.Millisecond)
			if err != nil {
				// Timeout is expected and not an error
				continue
			}

			if n > 0 {
				if debug {
					fmt.Printf("RECV (%d bytes): %02x\n", n, buf[:n])
				}

				// Check for ACK: report ID must be 0x01, 0x03, or 0x07 and byte[2] == AckByte
				//
				// Everything after the ACK byte is the parameter data a GET
				// asked for, so the whole reply is forwarded rather than a bare
				// "yes". buf is reused on the next iteration and must be copied.
				foundAck := false
				if n >= 3 && (buf[0] == 0x01 || buf[0] == 0x03 || buf[0] == 0x07) && buf[2] == protocol.AckByte {
					foundAck = true
					reply := make([]byte, n)
					copy(reply, buf[:n])
					select {
					case d.ackChan <- reply:
						if debug {
							fmt.Printf("ACK delivered to channel (report %02x)\n", buf[0])
						}
					default:
						if debug {
							fmt.Println("ACK channel full, dropped")
						}
					}
				}
				if debug && !foundAck {
					fmt.Println("No ACK byte found in response")
				}
			}
		}
	}
}

// Handshake performs the startup read RØDE Connect performs, discarding the
// results.
//
// It exists to keep rode-dsp's opening exchange byte-identical to the official
// application's, which is the only reason to send it: the device does not
// require it, and it does not clear anything despite the "INIT" name this used
// to carry. Callers that want the values should use ReadState instead.
func (d *Device) Handshake() error {
	if !d.Connected() {
		return errors.New("device not connected")
	}

	if d.debug {
		fmt.Println("Sending startup GET sweep")
	}

	effectOrder := []byte{protocol.EffComp, protocol.EffGate, protocol.EffAE, protocol.EffBB}
	for _, effID := range effectOrder {
		count := protocol.ParamCounts[effID]
		for pid := byte(0); pid < byte(count); pid++ {
			packet := protocol.BuildGetPacket(effID, pid)
			if err := d.Send(packet); err != nil {
				return fmt.Errorf("failed to send GET for effect %02x param %02x: %w", effID, pid, err)
			}
		}
	}

	if d.debug {
		fmt.Println("Startup GET sweep complete")
	}

	// Wait for all packets to be transmitted
	d.Flush()

	return nil
}

// SendAllParams sends all current parameter values from a DSPState.
// Packet order matches RØDE Connect: for each effect, send enable (only if ON)
// then all parameter packets. Disabled effects never send an enable packet.
func (d *Device) SendAllParams(state *dsp.DSPState) error {
	if !d.Connected() {
		return errors.New("device not connected")
	}

	if d.debug {
		fmt.Println("Sending all parameters")
	}

	// Send in fixed order: Comp, Gate, AE, BB
	// Each effect: [enable if ON] then [all params] — mirrors RØDE Connect Phase 2
	effectOrder := []byte{protocol.EffComp, protocol.EffGate, protocol.EffAE, protocol.EffBB}

	for _, effID := range effectOrder {
		// Only send enable packet when effect is ON; disabled effects send no enable packet
		if state.IsEnabled(effID) {
			packet := protocol.BuildEnablePacket(effID, true)
			if err := d.Send(packet); err != nil {
				return fmt.Errorf("failed to send enable packet for effect %02x: %w", effID, err)
			}
		}

		effDef, ok := dsp.Effects[effID]
		if !ok {
			continue
		}

		for _, param := range effDef.Params {
			value, _ := state.GetParam(effID, param.ParamID)
			encoded := param.EncodeFn(value)
			packet := protocol.BuildSetPacket(effID, param.ParamID, encoded)
			if err := d.Send(packet); err != nil {
				return fmt.Errorf("failed to send param %02x for effect %02x: %w", param.ParamID, effID, err)
			}
		}
	}

	if d.debug {
		fmt.Println("All parameters sent")
	}

	// Wait for all packets to be transmitted
	d.Flush()

	return nil
}

// ReadRaw attempts to read raw data from Report ID 0x03 (experimental)
func (d *Device) ReadRaw(timeout time.Duration) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if !d.connected || d.device == nil {
		return nil, errors.New("device not connected")
	}

	// Note: This is experimental - Report ID 0x03 reading
	buf := make([]byte, 64)

	if timeout > 0 {
		// Use a goroutine with timeout
		resultChan := make(chan []byte, 1)
		errorChan := make(chan error, 1)

		go func() {
			n, err := d.device.Read(buf)
			if err != nil {
				errorChan <- err
				return
			}
			resultChan <- buf[:n]
		}()

		select {
		case result := <-resultChan:
			return result, nil
		case err := <-errorChan:
			return nil, err
		case <-time.After(timeout):
			return nil, errors.New("read timeout")
		}
	}

	// No timeout, blocking read
	n, err := d.device.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// SetDebug enables or disables debug output
func (d *Device) SetDebug(debug bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.debug = debug
}
