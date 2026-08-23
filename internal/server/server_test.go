package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"rode-dsp/internal/dsp"
	"rode-dsp/internal/protocol"
)

// start brings up a real server on a random loopback port with no microphone
// attached, and returns a browser-like connection to it.
func start(t *testing.T) (*websocket.Conn, string) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.json")
	srv := NewServer(0, configPath, dsp.NewDSPState(), nil)

	ln, err := srv.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go srv.Serve(ln)

	addr := ln.Addr().String()
	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", http.Header{
		"Origin": {"http://" + addr},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	resp.Body.Close()
	t.Cleanup(func() { conn.Close() })

	return conn, configPath
}

// read returns the next message, failing the test rather than hanging.
func read(t *testing.T, conn *websocket.Conn) WSMessage {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var msg WSMessage
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	if msg.Type == MsgTypeError {
		t.Fatalf("server returned an error: %v", *msg.Error)
	}
	return msg
}

func TestGetState(t *testing.T) {
	conn, _ := start(t)

	if err := conn.WriteJSON(WSMessage{Type: MsgTypeGetState}); err != nil {
		t.Fatal(err)
	}

	msg := read(t, conn)
	if msg.Type != MsgTypeState {
		t.Fatalf("got type %q, want %q", msg.Type, MsgTypeState)
	}

	var state map[string]any
	if err := json.Unmarshal(msg.Value, &state); err != nil {
		t.Fatalf("state is not an object: %v", err)
	}
	for _, want := range []string{"compressor", "noise_gate", "aural_exciter", "big_bottom"} {
		if _, ok := state[want]; !ok {
			t.Errorf("state is missing %q", want)
		}
	}
}

// A slider move should reach every browser and land in the config file, with
// the wire bytes attached so the GUI can show what went out.
func TestSetParamBroadcastsAndSaves(t *testing.T) {
	conn, configPath := start(t)

	const threshold = -25.5
	value, err := json.Marshal(threshold)
	if err != nil {
		t.Fatal(err)
	}
	effect, param := int(protocol.EffComp), 0x01
	if err := conn.WriteJSON(WSMessage{
		Type: MsgTypeSetParam, Effect: &effect, Param: &param, Value: value,
	}); err != nil {
		t.Fatal(err)
	}

	msg := read(t, conn)
	if msg.Type != MsgTypeParamUpdate {
		t.Fatalf("got type %q, want %q", msg.Type, MsgTypeParamUpdate)
	}
	if msg.Encoding == nil {
		t.Fatal("no encoding attached, so the GUI cannot show the wire bytes")
	}
	if msg.Encoding.Hex == "" {
		t.Error("encoding carries no bytes")
	}

	// The config file is written before the broadcast goes out, so it is on
	// disk by now.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config was not written: %v", err)
	}
	var saved struct {
		Compressor struct {
			Threshold float64 `json:"threshold"`
		} `json:"compressor"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Compressor.Threshold != threshold {
		t.Errorf("saved threshold = %v, want %v", saved.Compressor.Threshold, threshold)
	}
}

// An out-of-range value must be refused, not clamped silently and not applied.
func TestSetParamRejectsOutOfRange(t *testing.T) {
	conn, _ := start(t)

	value, err := json.Marshal(999.0)
	if err != nil {
		t.Fatal(err)
	}
	effect, param := int(protocol.EffComp), 0x01
	if err := conn.WriteJSON(WSMessage{
		Type: MsgTypeSetParam, Effect: &effect, Param: &param, Value: value,
	}); err != nil {
		t.Fatal(err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var msg WSMessage
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	if msg.Type != MsgTypeError {
		t.Fatalf("got type %q, want an error", msg.Type)
	}
}

// Importing something that is not a settings file must not wipe the user's
// settings by parsing as an empty state.
func TestImportRejectsUnrelatedJSON(t *testing.T) {
	conn, _ := start(t)

	if err := conn.WriteJSON(WSMessage{
		Type:  MsgTypeImportState,
		Value: json.RawMessage(`{"unrelated":"document"}`),
	}); err != nil {
		t.Fatal(err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var msg WSMessage
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	if msg.Type != MsgTypeError {
		t.Fatalf("got type %q, want an error", msg.Type)
	}
}
