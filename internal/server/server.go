// Package server is the local web GUI.
//
// It serves one page to one browser on this machine. There is no hub, no
// message queue and no keepalive: a client is a WebSocket connection with a
// mutex around writes, and a broadcast is a loop over the connections. That is
// enough for a handful of tabs on localhost, and it is small enough to read.
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"rode-dsp/internal/dsp"
	"rode-dsp/internal/hid"
	"rode-dsp/internal/protocol"
)

// upgrader rejects WebSocket handshakes from other web pages.
//
// The Origin header is the only thing that says which page is calling; Host is
// this server's own address and is identical whoever sent the request. Checking
// Host instead — as an earlier version did — accepts every origin, which would
// let any site the user visits drive the microphone and rewrite their config.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Not a browser, so there is no ambient session to abuse. curl and
			// the like are fine; the listener is loopback-only regardless.
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		// Compared with the port attached, so a page served from another port
		// on this machine is still a different origin.
		return strings.EqualFold(u.Host, r.Host)
	},
}

// Message types for WebSocket communication
const (
	MsgTypeSetParam      = "set_param"
	MsgTypeSetEnable     = "set_enable"
	MsgTypeResetDefaults = "reset_defaults"
	MsgTypeGetState      = "get_state"
	MsgTypeState         = "state"
	MsgTypeParamUpdate   = "param_update"
	MsgTypeError         = "error"

	// MsgTypePreview asks what a value would encode to without changing
	// anything. The GUI's advanced mode uses it while a slider is being
	// dragged, so the wire bytes track the handle without writing to the
	// device or the config file on every mouse move.
	MsgTypePreview = "preview"

	// MsgTypeImportState carries a whole state document — the same JSON the
	// tool writes to its config file — from a file the user picked.
	MsgTypeImportState = "import_state"

	// Presets: a named complete DSP state. The server answers any of the four
	// inbound messages with MsgTypePresets, which carries the whole list, so
	// the GUI never has to reconcile a partial update against what it had.
	MsgTypeListPresets  = "list_presets"
	MsgTypeSavePreset   = "save_preset"
	MsgTypeLoadPreset   = "load_preset"
	MsgTypeDeletePreset = "delete_preset"
	MsgTypePresets      = "presets"
)

// WSMessage is the WebSocket message structure
type WSMessage struct {
	Type    string          `json:"type"`
	Effect  *int            `json:"effect,omitempty"`
	Param   *int            `json:"param,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
	Enabled *bool           `json:"enabled,omitempty"`
	Error   *string         `json:"error,omitempty"`

	// Encoding carries the wire representation of Value back to the GUI, for
	// preview responses and param_update broadcasts.
	Encoding *Encoding `json:"encoding,omitempty"`

	// Name identifies a preset on the preset messages; Presets carries the
	// list back. Only the metadata travels — the state itself is applied
	// server-side and reaches the GUI as an ordinary state broadcast.
	Name    *string       `json:"name,omitempty"`
	Presets []PresetEntry `json:"presets,omitempty"`
}

// PresetEntry is one row of the GUI's preset list.
type PresetEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Builtin     bool   `json:"builtin"`
}

// Client is one open browser connection. The mutex is because a WebSocket
// allows only one writer at a time, and both the reading goroutine and a
// broadcast from another connection can write to it.
type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// send writes one message. A failed write means the browser has gone; the
// connection is closed and its read loop will notice and unregister it.
func (c *Client) send(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("could not encode a %s message: %v", msg.Type, err)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.conn.Close()
	}
}

// Server owns the HTTP listener, the browser connections and the shared state.
type Server struct {
	port int
	// configPath is the file the GUI loaded, so edits are written back to the
	// same place rather than to whatever the default resolves to.
	configPath string
	dspState   *dsp.DSPState
	device     *hid.Device
	debug      bool

	mu      sync.Mutex
	clients map[*Client]bool
}

// NewServer creates a new HTTP server with WebSocket support
func NewServer(port int, configPath string, dspState *dsp.DSPState, device *hid.Device, debug bool) *Server {
	return &Server{
		port:       port,
		configPath: configPath,
		dspState:   dspState,
		device:     device,
		debug:      debug,
		clients:    make(map[*Client]bool),
	}
}

func (s *Server) add(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c] = true
}

func (s *Server) remove(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c)
}

// broadcast sends a message to every open browser.
func (s *Server) broadcast(msg WSMessage) {
	s.mu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()

	for _, c := range clients {
		c.send(msg)
	}
}

// presetPath keeps user presets beside the config file this server is using.
// An empty result means the location could not be worked out; the dsp package
// reports that as an error when the file is actually read or written.
func (s *Server) presetPath() string {
	path, err := dsp.PresetPathFor(s.configPath)
	if err != nil {
		return ""
	}
	return path
}

// SyncFromDevice replaces the server's idea of the DSP state with what the
// microphone actually holds.
//
// The config file records what this tool last sent, which stops being true the
// moment anything else drives the device — RØDE Connect, the rode-dsp CLI, or a
// replug. Trusting it is what let the browser show an effect as disabled while
// the device had it enabled. The device is the authority.
//
// A failure here is not fatal: whatever was loaded stands in — the saved
// config, or the defaults when no config has been created yet.
func (s *Server) SyncFromDevice() error {
	if s.device == nil || !s.device.Connected() {
		return nil
	}

	live, err := s.device.ReadState()
	if err != nil {
		return err
	}

	// Copy into the existing DSPState rather than swapping the pointer: the CLI
	// context shares it and broadcasts read from it.
	for effID, enabled := range live.GetAllEnabled() {
		s.dspState.SetEnabled(effID, enabled)
	}
	for effID, params := range live.GetAllParams() {
		for paramID, value := range params {
			if err := s.dspState.SetParam(effID, paramID, value); err != nil {
				return fmt.Errorf("effect 0x%02x param 0x%02x: %w", effID, paramID, err)
			}
		}
	}

	// Deliberately no save: reading the device is not a change the user made,
	// and opening the GUI should not create a config file. The next actual edit
	// writes the whole state, synced values included.
	return nil
}

// Listen binds the port and returns the listener, so the caller can know the
// GUI is actually up before pointing a browser at it.
//
// Loopback only, and deliberately not configurable. The GUI has no password and
// hands whoever reaches it control of the microphone; the security model is
// that only this machine can. An earlier version bound every interface, which
// quietly put that control on the local network.
func (s *Server) Listen() (net.Listener, error) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("cannot listen on %s: %w", addr, err)
	}
	return ln, nil
}

// Serve handles requests until the listener is closed.
func (s *Server) Serve(ln net.Listener) error {
	// Adopt the device's state before serving anything, so the first page load
	// shows the microphone rather than the last-saved config.
	if err := s.SyncFromDevice(); err != nil {
		log.Printf("Could not read state from device, using saved config: %v", err)
	}

	staticHandler, err := StaticHandler()
	if err != nil {
		return fmt.Errorf("failed to create static handler: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", staticHandler)
	mux.HandleFunc("/ws", s.wsHandler)
	mux.HandleFunc("/api/state", s.stateHandler)
	mux.HandleFunc("/api/schema", s.schemaHandler)

	server := &http.Server{
		Handler:     mux,
		ReadTimeout: 10 * time.Second,
		// No WriteTimeout: it would apply to WebSocket connections too, which
		// are long-lived by design. Writes carry their own deadline.
	}

	return server.Serve(ln)
}

// wsHandler upgrades a connection and reads from it until the browser goes
// away. This goroutine is the connection's whole lifetime.
func (s *Server) wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{conn: conn}
	s.add(client)
	defer func() {
		s.remove(client)
		conn.Close()
	}()

	// Most messages are a few dozen bytes; an imported settings document is the
	// exception and runs to several hundred. 64 KiB is generous for that and
	// still far too small to be worth anything to a client trying to exhaust
	// memory.
	conn.SetReadLimit(64 << 10)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		s.handleMessage(client, message)
	}
}

// stateHandler handles HTTP requests for current state
func (s *Server) stateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.dspState); err != nil {
		log.Printf("stateHandler: %v", err)
	}
}

// handleMessage processes incoming WebSocket messages
func (s *Server) handleMessage(client *Client, message []byte) {
	var msg WSMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		s.sendError(client, "Invalid message format")
		return
	}

	switch msg.Type {
	case MsgTypeGetState:
		// The browser sends this on connect and on reconnect, which are exactly
		// the moments its view may have gone stale — a reconnect in particular
		// means time passed during which anything could have driven the device.
		if err := s.SyncFromDevice(); err != nil {
			log.Printf("Could not read state from device, using saved config: %v", err)
		}
		client.send(s.stateMessage())

	case MsgTypeSetParam:
		if msg.Effect == nil || msg.Param == nil || msg.Value == nil {
			s.sendError(client, "Missing required fields for set_param")
			return
		}
		s.handleSetParam(client, byte(*msg.Effect), byte(*msg.Param), msg.Value)

	case MsgTypeSetEnable:
		if msg.Effect == nil || msg.Enabled == nil {
			s.sendError(client, "Missing required fields for set_enable")
			return
		}
		s.handleSetEnable(client, byte(*msg.Effect), *msg.Enabled)

	case MsgTypePreview:
		if msg.Effect == nil || msg.Param == nil || msg.Value == nil {
			s.sendError(client, "Missing required fields for preview")
			return
		}
		s.handlePreview(client, byte(*msg.Effect), byte(*msg.Param), msg.Value)

	case MsgTypeImportState:
		if msg.Value == nil {
			s.sendError(client, "Missing value for import_state")
			return
		}
		s.handleImportState(client, msg.Value)

	case MsgTypeListPresets:
		client.send(s.presetsMessage())

	case MsgTypeSavePreset:
		if msg.Name == nil {
			s.sendError(client, "Missing name for save_preset")
			return
		}
		if err := dsp.SaveUserPreset(s.presetPath(), *msg.Name, "", s.dspState); err != nil {
			s.sendError(client, err.Error())
			return
		}
		s.broadcast(s.presetsMessage())

	case MsgTypeDeletePreset:
		if msg.Name == nil {
			s.sendError(client, "Missing name for delete_preset")
			return
		}
		if err := dsp.DeleteUserPreset(s.presetPath(), *msg.Name); err != nil {
			s.sendError(client, err.Error())
			return
		}
		s.broadcast(s.presetsMessage())

	case MsgTypeLoadPreset:
		if msg.Name == nil {
			s.sendError(client, "Missing name for load_preset")
			return
		}
		s.handleLoadPreset(client, *msg.Name)

	case MsgTypeResetDefaults:
		if msg.Effect == nil {
			s.sendError(client, "Missing effect for reset_defaults")
			return
		}
		s.handleResetDefaults(client, byte(*msg.Effect))

	default:
		s.sendError(client, fmt.Sprintf("Unknown message type: %s", msg.Type))
	}
}

// saveConfig writes the state to disk, reporting a failure to the browser
// rather than only to the terminal the user is not looking at.
func (s *Server) saveConfig(client *Client) {
	if err := dsp.SaveConfig(s.configPath, s.dspState); err != nil {
		log.Printf("Failed to save config: %v", err)
		s.sendError(client, "Could not save your settings to disk")
	}
}

// handleSetParam handles parameter update requests
func (s *Server) handleSetParam(client *Client, effectID, paramID byte, valueJSON json.RawMessage) {
	var value float64
	if err := json.Unmarshal(valueJSON, &value); err != nil {
		s.sendError(client, "Invalid value format")
		return
	}

	param, ok := findParam(effectID, paramID)
	if !ok {
		s.sendError(client, fmt.Sprintf("Unknown effect/parameter %d/%d", effectID, paramID))
		return
	}

	if err := s.dspState.SetParam(effectID, paramID, value); err != nil {
		s.sendError(client, err.Error())
		return
	}
	s.saveConfig(client)

	if s.device != nil && s.device.Connected() {
		packet := protocol.BuildSetPacket(effectID, paramID, param.EncodeFn(value))
		if err := s.device.Send(packet); err != nil {
			log.Printf("Failed to send parameter to device: %v", err)
			s.sendError(client, "Failed to send to device")
			return
		}
	}

	// Tell every browser, with the wire bytes attached so each can show what
	// actually went out.
	update := WSMessage{
		Type:   MsgTypeParamUpdate,
		Effect: intPtr(int(effectID)),
		Param:  intPtr(int(paramID)),
		Value:  valueJSON,
	}
	if enc, ok := encodeParam(effectID, paramID, value); ok {
		update.Encoding = &enc
	}
	s.broadcast(update)
}

// handlePreview answers "what would this value encode to?" without touching the
// device, the DSP state or the config file. It is read-only by construction:
// encodeParam only calls the pure encoder functions.
func (s *Server) handlePreview(client *Client, effectID, paramID byte, valueJSON json.RawMessage) {
	var value float64
	if err := json.Unmarshal(valueJSON, &value); err != nil {
		s.sendError(client, "Invalid value format")
		return
	}

	enc, ok := encodeParam(effectID, paramID, value)
	if !ok {
		s.sendError(client, fmt.Sprintf("Unknown effect/parameter %d/%d", effectID, paramID))
		return
	}

	client.send(WSMessage{
		Type:     MsgTypePreview,
		Effect:   intPtr(int(effectID)),
		Param:    intPtr(int(paramID)),
		Value:    valueJSON,
		Encoding: &enc,
	})
}

// handleSetEnable handles enable/disable requests
func (s *Server) handleSetEnable(client *Client, effectID byte, enabled bool) {
	s.dspState.SetEnabled(effectID, enabled)
	s.saveConfig(client)

	if s.device != nil && s.device.Connected() {
		packet := protocol.BuildEnablePacket(effectID, enabled)
		if err := s.device.Send(packet); err != nil {
			log.Printf("Failed to send enable to device: %v", err)
			s.sendError(client, "Failed to send to device")
			return
		}
	}

	s.broadcast(WSMessage{
		Type:    MsgTypeState,
		Effect:  intPtr(int(effectID)),
		Enabled: &enabled,
	})
}

// handleResetDefaults returns one effect to its factory values.
func (s *Server) handleResetDefaults(client *Client, effectID byte) {
	effDef, ok := dsp.Effects[effectID]
	if !ok {
		s.sendError(client, fmt.Sprintf("Unknown effect ID: %d", effectID))
		return
	}

	for _, param := range effDef.Params {
		if err := s.dspState.SetParam(effectID, param.ParamID, param.Default); err != nil {
			log.Printf("Failed to reset parameter %d for effect %d: %v", param.ParamID, effectID, err)
		}
	}

	s.saveConfig(client)
	s.applyAndBroadcast(client)
}

// handleImportState adopts a state document from an imported file.
//
// Parsing goes through DSPState's own unmarshaller, so an imported file gets
// exactly the treatment a config file on disk gets: unknown keys ignored, values
// clamped to each parameter's range, anything missing left at its default.
func (s *Server) handleImportState(client *Client, doc json.RawMessage) {
	imported := dsp.NewDSPState()
	if err := json.Unmarshal(doc, imported); err != nil {
		s.sendError(client, fmt.Sprintf("Could not read those settings: %v", err))
		return
	}

	// An unrelated JSON object parses without error and comes out as the
	// defaults, which would silently wipe the user's settings. Require the
	// document to name at least one effect this tool knows.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(doc, &raw); err != nil {
		s.sendError(client, "Settings must be a JSON object")
		return
	}
	recognised := false
	for _, eff := range dsp.Effects {
		if _, ok := raw[dsp.ConfigKey(eff.Name)]; ok {
			recognised = true
			break
		}
	}
	if !recognised {
		s.sendError(client, "That file does not contain any RØDE DSP settings")
		return
	}

	if err := (dsp.Preset{Name: "imported", State: imported}).CopyInto(s.dspState); err != nil {
		s.sendError(client, err.Error())
		return
	}

	s.saveConfig(client)
	s.applyAndBroadcast(client)
}

// handleLoadPreset makes a preset the live state: it goes into the shared
// DSPState, out to the device, into the config file, and back to every browser.
func (s *Server) handleLoadPreset(client *Client, name string) {
	preset, err := dsp.FindPreset(s.presetPath(), name)
	if err != nil {
		s.sendError(client, err.Error())
		return
	}

	if err := preset.CopyInto(s.dspState); err != nil {
		s.sendError(client, err.Error())
		return
	}

	s.saveConfig(client)
	s.applyAndBroadcast(client)
}

// applyAndBroadcast writes the whole state to the microphone and shows every
// browser the result. Used wherever more than one parameter changed at once.
//
// A device failure still leaves the new state worth showing: the sliders hold
// it even if the microphone did not take it.
func (s *Server) applyAndBroadcast(client *Client) {
	if s.device != nil && s.device.Connected() {
		if err := s.device.Apply(s.dspState); err != nil {
			log.Printf("Failed to send settings to device: %v", err)
			s.sendError(client, "Failed to send to device")
		}
	}
	s.broadcast(s.stateMessage())
}

// stateMessage is the whole DSP state, for a client that needs all of it.
func (s *Server) stateMessage() WSMessage {
	stateJSON, err := json.Marshal(s.dspState)
	if err != nil {
		log.Printf("Failed to marshal state: %v", err)
		return WSMessage{Type: MsgTypeError, Error: strPtr("Failed to read current settings")}
	}
	return WSMessage{Type: MsgTypeState, Value: stateJSON}
}

// presetsMessage lists what the GUI should offer, built-ins first.
func (s *Server) presetsMessage() WSMessage {
	all, err := dsp.AllPresets(s.presetPath())
	if err != nil {
		log.Printf("Failed to read presets: %v", err)
		all = dsp.BuiltinPresets()
	}

	entries := make([]PresetEntry, 0, len(all))
	for _, p := range all {
		entries = append(entries, PresetEntry{
			Name:        p.Name,
			Description: p.Description,
			Builtin:     p.Builtin,
		})
	}
	return WSMessage{Type: MsgTypePresets, Presets: entries}
}

// sendError reports a problem to one browser.
func (s *Server) sendError(client *Client, msg string) {
	client.send(WSMessage{Type: MsgTypeError, Error: &msg})
}

// findParam looks up one parameter's definition.
func findParam(effectID, paramID byte) (dsp.ParamDef, bool) {
	effDef, ok := dsp.Effects[effectID]
	if !ok {
		return dsp.ParamDef{}, false
	}
	for _, p := range effDef.Params {
		if p.ParamID == paramID {
			return p, true
		}
	}
	return dsp.ParamDef{}, false
}

func intPtr(i int) *int       { return &i }
func strPtr(s string) *string { return &s }
