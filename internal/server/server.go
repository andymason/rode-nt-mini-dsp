package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"rode-dsp/internal/dsp"
	"rode-dsp/internal/hid"
	"rode-dsp/internal/protocol"
)

// WebSocket upgrader configuration
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from localhost on any port
		host := r.Host
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		return host == "localhost" || host == "127.0.0.1"
	},
}

// Message types for WebSocket communication
const (
	MsgTypeSetParam         = "set_param"
	MsgTypeSetEnable        = "set_enable"
	MsgTypeResetDefaults    = "reset_defaults"
	MsgTypeGetState         = "get_state"
	MsgTypeState            = "state"
	MsgTypeParamUpdate      = "param_update"
	MsgTypeConnectionStatus = "connection_status"
	MsgTypeError            = "error"

	// MsgTypePreview asks what a value would encode to without changing
	// anything. The GUI's advanced mode uses it while a slider is being
	// dragged, so the wire bytes track the handle without writing to the
	// device or the config file on every mouse move.
	MsgTypePreview = "preview"

	// Presets: a named complete DSP state. The four inbound messages are the
	// obvious ones; the server answers any of them with MsgTypePresets, which
	// carries the whole list, so the GUI never has to reconcile a partial
	// update against what it already had.
	// MsgTypeImportState carries a whole state document — the same JSON the
	// tool writes to its config file — from a file the user picked.
	MsgTypeImportState = "import_state"

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

// Client represents a WebSocket connection
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub manages WebSocket clients
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

// Server manages the HTTP server and WebSocket hub
type Server struct {
	port     int
	hub      *Hub
	dspState *dsp.DSPState
	device   *hid.Device
	debug    bool
}

// NewHub creates a new WebSocket hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			if len(h.clients) == 1 {
				log.Println("First WebSocket client connected")
			}

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			if len(h.clients) == 0 {
				log.Println("Last WebSocket client disconnected")
			}

		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

// NewServer creates a new HTTP server with WebSocket support
func NewServer(port int, dspState *dsp.DSPState, device *hid.Device, debug bool) *Server {
	hub := NewHub()
	return &Server{
		port:     port,
		hub:      hub,
		dspState: dspState,
		device:   device,
		debug:    debug,
	}
}

// SyncFromDevice replaces the server's idea of the DSP state with what the
// microphone actually holds.
//
// The config file records what this tool last sent, which stops being true the
// moment anything else drives the device — RØDE Connect, the rode-dsp CLI, or a
// replug. Trusting it is what let the browser show an effect as disabled while
// the device had it enabled, so moving that effect's sliders produced no
// audible change and the server then wrote its stale view back over the config.
// The device is the authority. See docs/re/02-protocol.md.
//
// A failure here is not fatal: the config stands in, which is no worse than the
// behaviour this replaces.
func (s *Server) SyncFromDevice() error {
	if s.device == nil || !s.device.Connected() {
		return nil
	}

	live, err := s.device.ReadState()
	if err != nil {
		return err
	}

	// Copy into the existing DSPState rather than swapping the pointer: the CLI
	// context shares it and the hub broadcasts from it.
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

	if err := dsp.SaveConfig("", s.dspState); err != nil {
		return fmt.Errorf("saving synced state: %w", err)
	}
	return nil
}

// Start begins the HTTP server
func (s *Server) Start() error {
	// Start the WebSocket hub
	go s.hub.Run()

	// Adopt the device's state before serving anything, so the first page load
	// shows the microphone rather than the last-saved config.
	if err := s.SyncFromDevice(); err != nil {
		log.Printf("Could not read state from device, using saved config: %v", err)
	}

	// Get static file handler
	staticHandler, err := StaticHandler()
	if err != nil {
		return fmt.Errorf("failed to create static handler: %w", err)
	}

	// Set up HTTP routes
	mux := http.NewServeMux()
	mux.Handle("/", staticHandler)
	mux.HandleFunc("/ws", s.wsHandler)
	mux.HandleFunc("/api/state", s.stateHandler)
	mux.HandleFunc("/api/schema", s.schemaHandler)

	// Create HTTP server
	addr := fmt.Sprintf(":%d", s.port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("Starting HTTP server on http://localhost%s", addr)
	log.Printf("Web GUI available at http://localhost:%d", s.port)

	return server.ListenAndServe()
}

// wsHandler handles WebSocket connections
func (s *Server) wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		hub:  s.hub,
		conn: conn,
		send: make(chan []byte, 256),
	}

	// Register client
	s.hub.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump(s)
}

// stateHandler handles HTTP requests for current state
func (s *Server) stateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stateJSON, err := json.Marshal(s.dspState)
	if err != nil {
		http.Error(w, "Failed to marshal state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(stateJSON); err != nil {
		log.Printf("stateHandler: failed to write response: %v", err)
	}
}

// readPump reads messages from the WebSocket connection
func (c *Client) readPump(s *Server) {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	// Most messages are a few dozen bytes; an imported settings document is the
	// exception and runs to several hundred, more once someone has hand-edited
	// the file. 64 KiB is generous for that and still far too small to be worth
	// anything to a client trying to exhaust memory.
	c.conn.SetReadLimit(64 << 10)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}

		// Handle the message
		s.handleMessage(c, message)
	}
}

// writePump writes messages to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

			// Send any queued messages as separate WebSocket frames
			n := len(c.send)
			for i := 0; i < n; i++ {
				if err := c.conn.WriteMessage(websocket.TextMessage, <-c.send); err != nil {
					return
				}
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
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
		s.sendState(client)

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
		s.sendPresets(client)

	case MsgTypeSavePreset:
		if msg.Name == nil {
			s.sendError(client, "Missing name for save_preset")
			return
		}
		if err := dsp.SaveUserPreset("", *msg.Name, "", s.dspState); err != nil {
			s.sendError(client, err.Error())
			return
		}
		s.broadcastPresets()

	case MsgTypeDeletePreset:
		if msg.Name == nil {
			s.sendError(client, "Missing name for delete_preset")
			return
		}
		if err := dsp.DeleteUserPreset("", *msg.Name); err != nil {
			s.sendError(client, err.Error())
			return
		}
		s.broadcastPresets()

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

// handleSetParam handles parameter update requests
func (s *Server) handleSetParam(client *Client, effectID, paramID byte, valueJSON json.RawMessage) {
	var value float64
	if err := json.Unmarshal(valueJSON, &value); err != nil {
		s.sendError(client, "Invalid value format")
		return
	}

	// Update DSP state
	if err := s.dspState.SetParam(effectID, paramID, value); err != nil {
		s.sendError(client, err.Error())
		return
	}

	// Save configuration
	if err := dsp.SaveConfig("", s.dspState); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	// Send to device if connected
	if s.device != nil && s.device.Connected() {
		// Find encoding function for this parameter
		effDef, ok := dsp.Effects[effectID]
		if !ok {
			s.sendError(client, fmt.Sprintf("Unknown effect ID: %d", effectID))
			return
		}

		var encodeFn func(float64) []byte
		for _, param := range effDef.Params {
			if param.ParamID == paramID {
				encodeFn = param.EncodeFn
				break
			}
		}

		if encodeFn == nil {
			s.sendError(client, fmt.Sprintf("Unknown parameter ID: %d for effect %d", paramID, effectID))
			return
		}

		// Encode and send
		encoded := encodeFn(value)
		packet := protocol.BuildSetPacket(effectID, paramID, encoded)
		if err := s.device.Send(packet); err != nil {
			log.Printf("Failed to send parameter to device: %v", err)
			s.sendError(client, "Failed to send to device")
			return
		}
	}

	// Broadcast update to all clients, with the wire bytes attached so every
	// connected GUI can show what actually went out.
	updateMsg := WSMessage{
		Type:   MsgTypeParamUpdate,
		Effect: intPtr(int(effectID)),
		Param:  intPtr(int(paramID)),
		Value:  valueJSON,
	}
	if enc, ok := encodeParam(effectID, paramID, value); ok {
		updateMsg.Encoding = &enc
	}

	if msgJSON, err := json.Marshal(updateMsg); err == nil {
		s.hub.broadcast <- msgJSON
	}
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

	msg := WSMessage{
		Type:     MsgTypePreview,
		Effect:   intPtr(int(effectID)),
		Param:    intPtr(int(paramID)),
		Value:    valueJSON,
		Encoding: &enc,
	}
	if msgJSON, err := json.Marshal(msg); err == nil {
		client.send <- msgJSON
	}
}

// handleSetEnable handles enable/disable requests
func (s *Server) handleSetEnable(client *Client, effectID byte, enabled bool) {
	// Update DSP state
	s.dspState.SetEnabled(effectID, enabled)

	// Save configuration
	if err := dsp.SaveConfig("", s.dspState); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	// Send to device if connected
	if s.device != nil && s.device.Connected() {
		packet := protocol.BuildEnablePacket(effectID, enabled)
		if err := s.device.Send(packet); err != nil {
			log.Printf("Failed to send enable to device: %v", err)
			s.sendError(client, "Failed to send to device")
			return
		}
	}

	// Broadcast update to all clients
	stateMsg := WSMessage{
		Type:    MsgTypeState,
		Effect:  intPtr(int(effectID)),
		Enabled: &enabled,
	}

	if msgJSON, err := json.Marshal(stateMsg); err == nil {
		s.hub.broadcast <- msgJSON
	}
}

// handleResetDefaults handles reset defaults requests
func (s *Server) handleResetDefaults(client *Client, effectID byte) {
	effDef, ok := dsp.Effects[effectID]
	if !ok {
		s.sendError(client, fmt.Sprintf("Unknown effect ID: %d", effectID))
		return
	}

	// Reset all parameters for this effect to defaults
	for _, param := range effDef.Params {
		if err := s.dspState.SetParam(effectID, param.ParamID, param.Default); err != nil {
			log.Printf("Failed to reset parameter %d for effect %d: %v", param.ParamID, effectID, err)
		}
	}

	// Save configuration
	if err := dsp.SaveConfig("", s.dspState); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	// Send all parameters to device if connected
	if s.device != nil && s.device.Connected() {
		if err := s.device.SendAllParams(s.dspState); err != nil {
			log.Printf("Failed to send defaults to device: %v", err)
			s.sendError(client, "Failed to send to device")
			return
		}
	}

	// Send full state update
	s.sendState(client)
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

	// Reuse the preset path: it is the same job — replace every effect, on the
	// device as well as in the state, and tell every browser.
	if err := (dsp.Preset{Name: "imported", State: imported}).CopyInto(s.dspState); err != nil {
		s.sendError(client, err.Error())
		return
	}

	if err := dsp.SaveConfig("", s.dspState); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	if err := s.applyStateToDevice(); err != nil {
		log.Printf("Failed to send imported settings to device: %v", err)
		s.sendError(client, "Failed to send to device")
	}

	s.broadcastState()
}

// handleLoadPreset makes a preset the live state: it goes into the shared
// DSPState, out to the device, into the config file, and back to every browser.
func (s *Server) handleLoadPreset(client *Client, name string) {
	preset, err := dsp.FindPreset("", name)
	if err != nil {
		s.sendError(client, err.Error())
		return
	}

	if err := preset.CopyInto(s.dspState); err != nil {
		s.sendError(client, err.Error())
		return
	}

	if err := dsp.SaveConfig("", s.dspState); err != nil {
		log.Printf("Failed to save config: %v", err)
	}

	if err := s.applyStateToDevice(); err != nil {
		log.Printf("Failed to send preset to device: %v", err)
		s.sendError(client, "Failed to send to device")
		// The state still stands and is still worth showing: the sliders now
		// hold the preset even if the microphone did not take it.
	}

	s.broadcastState()
}

// applyStateToDevice writes the whole of the current state to the microphone.
//
// This is not device.SendAllParams: that one sends an enable packet only for
// effects that are on, which is right when applying a config to a device in a
// known state but wrong here. Loading a preset that turns an effect off has to
// say so explicitly, or an effect the user had running stays running.
//
// Parameters go first and the enable flag last, so an effect being switched on
// is already carrying the preset's values at the moment it starts processing.
func (s *Server) applyStateToDevice() error {
	if s.device == nil || !s.device.Connected() {
		return nil
	}

	for _, effID := range []byte{protocol.EffComp, protocol.EffGate, protocol.EffAE, protocol.EffBB} {
		effDef, ok := dsp.Effects[effID]
		if !ok {
			continue
		}

		for _, param := range effDef.Params {
			value, ok := s.dspState.GetParam(effID, param.ParamID)
			if !ok {
				continue
			}
			packet := protocol.BuildSetPacket(effID, param.ParamID, param.EncodeFn(value))
			if err := s.device.Send(packet); err != nil {
				return fmt.Errorf("effect 0x%02x param 0x%02x: %w", effID, param.ParamID, err)
			}
		}

		packet := protocol.BuildEnablePacket(effID, s.dspState.IsEnabled(effID))
		if err := s.device.Send(packet); err != nil {
			return fmt.Errorf("effect 0x%02x enable: %w", effID, err)
		}
	}

	s.device.Flush()
	return nil
}

// presetEntries lists what the GUI should offer, built-ins first.
func presetEntries() []PresetEntry {
	all, err := dsp.AllPresets("")
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
	return entries
}

// sendPresets sends the preset list to one client.
func (s *Server) sendPresets(client *Client) {
	msg := WSMessage{Type: MsgTypePresets, Presets: presetEntries()}
	if msgJSON, err := json.Marshal(msg); err == nil {
		client.send <- msgJSON
	}
}

// broadcastPresets tells every client the list changed. Presets live in a file
// shared by all of them, so a save in one window belongs in the others too.
func (s *Server) broadcastPresets() {
	msg := WSMessage{Type: MsgTypePresets, Presets: presetEntries()}
	if msgJSON, err := json.Marshal(msg); err == nil {
		s.hub.broadcast <- msgJSON
	}
}

// broadcastState sends the whole state to every client, which is what a preset
// load needs: it moves every parameter of every effect at once.
func (s *Server) broadcastState() {
	stateJSON, err := json.Marshal(s.dspState)
	if err != nil {
		log.Printf("Failed to marshal state: %v", err)
		return
	}

	msg := WSMessage{Type: MsgTypeState, Value: stateJSON}
	if msgJSON, err := json.Marshal(msg); err == nil {
		s.hub.broadcast <- msgJSON
	}
}

// sendState sends the current DSP state to a client
func (s *Server) sendState(client *Client) {
	stateJSON, err := json.Marshal(s.dspState)
	if err != nil {
		s.sendError(client, "Failed to marshal state")
		return
	}

	stateMsg := WSMessage{
		Type:  MsgTypeState,
		Value: stateJSON,
	}

	if msgJSON, err := json.Marshal(stateMsg); err == nil {
		client.send <- msgJSON
	}
}

// sendError sends an error message to a client
func (s *Server) sendError(client *Client, errorMsg string) {
	errorJSON := WSMessage{
		Type:  MsgTypeError,
		Error: &errorMsg,
	}

	if msgJSON, err := json.Marshal(errorJSON); err == nil {
		client.send <- msgJSON
	}
}

// intPtr returns a pointer to an int value
func intPtr(i int) *int {
	return &i
}
