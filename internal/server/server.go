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
)

// WSMessage is the WebSocket message structure
type WSMessage struct {
	Type    string          `json:"type"`
	Effect  *int            `json:"effect,omitempty"`
	Param   *int            `json:"param,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
	Enabled *bool           `json:"enabled,omitempty"`
	Hex     *string         `json:"hex,omitempty"`
	Error   *string         `json:"error,omitempty"`
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

// Start begins the HTTP server
func (s *Server) Start() error {
	// Start the WebSocket hub
	go s.hub.Run()

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

	c.conn.SetReadLimit(512)
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

	// Broadcast update to all clients
	updateMsg := WSMessage{
		Type:   MsgTypeParamUpdate,
		Effect: intPtr(int(effectID)),
		Param:  intPtr(int(paramID)),
		Value:  valueJSON,
	}

	if msgJSON, err := json.Marshal(updateMsg); err == nil {
		s.hub.broadcast <- msgJSON
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
