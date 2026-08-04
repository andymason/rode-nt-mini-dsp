package server

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sort"

	"rode-dsp/internal/dsp"
	"rode-dsp/internal/protocol"
)

// The GUI builds itself from this schema rather than hardcoding parameter
// ranges in HTML. The previous version duplicated the registry by hand and bound
// sliders by ordinal position, which is why its hex readouts showed "xxx" — they
// had no way to reach the encoders. Serving the registry means a change to
// dsp.Effects reaches the interface with no HTML edit.

// ParamSchema describes one parameter to the GUI.
type ParamSchema struct {
	ID      int     `json:"id"`
	Key     string  `json:"key"`
	Name    string  `json:"name"`
	Unit    string  `json:"unit"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Default float64 `json:"default"`
	Step    float64 `json:"step"`
	Scale   string  `json:"scale"`
	Table   string  `json:"table,omitempty"`
	Formula string  `json:"formula,omitempty"`
	Indexed bool    `json:"indexed"`
}

// EffectSchema describes one effect and its parameters.
type EffectSchema struct {
	ID     int           `json:"id"`
	Key    string        `json:"key"`
	Name   string        `json:"name"`
	Params []ParamSchema `json:"params"`
}

// DeviceSchema is the fixed protocol context, shown in the GUI's advanced mode.
type DeviceSchema struct {
	VID        string `json:"vid"`
	PID        string `json:"pid"`
	ReportID   string `json:"reportId"`
	PacketSize int    `json:"packetSize"`
	Scale      string `json:"scale"`
	Source     string `json:"source"`
}

// Schema is the whole document served at /api/schema.
type Schema struct {
	Device  DeviceSchema   `json:"device"`
	Effects []EffectSchema `json:"effects"`
}

// BuildSchema renders dsp.Effects into the GUI's schema. Effects and parameters
// are sorted by ID so the interface has a stable order — dsp.Effects is a map.
func BuildSchema() Schema {
	ids := make([]int, 0, len(dsp.Effects))
	for id := range dsp.Effects {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)

	effects := make([]EffectSchema, 0, len(ids))
	for _, id := range ids {
		def := dsp.Effects[byte(id)]

		params := make([]ParamSchema, 0, len(def.Params))
		for _, p := range def.Params {
			params = append(params, ParamSchema{
				ID:      int(p.ParamID),
				Key:     dsp.ConfigKey(p.Name),
				Name:    p.Name,
				Unit:    p.Unit,
				Min:     p.UIMin,
				Max:     p.UIMax,
				Default: p.Default,
				Step:    p.Resolution,
				Scale:   p.Scale,
				Table:   p.Table,
				Formula: p.Formula,
				Indexed: p.IndexFn != nil,
			})
		}
		sort.Slice(params, func(i, j int) bool { return params[i].ID < params[j].ID })

		effects = append(effects, EffectSchema{
			ID:     id,
			Key:    dsp.ConfigKey(def.Name),
			Name:   def.Name,
			Params: params,
		})
	}

	return Schema{
		Device: DeviceSchema{
			VID:        "0x19F7",
			PID:        "0x0015",
			ReportID:   "0x04",
			PacketSize: protocol.PacketSize,
			Scale:      "Q31, saturating (firmware above 2.1.2)",
			Source:     "RODE Connect.exe c3015816…1023c — docs/re/03-encoders.md",
		},
		Effects: effects,
	}
}

// schemaHandler serves the parameter registry.
func (s *Server) schemaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(BuildSchema()); err != nil {
		log.Printf("schemaHandler: %v", err)
	}
}

// Encoding is what a UI value becomes on the wire. The GUI shows this live in
// advanced mode, computed by the same functions that drive the device.
type Encoding struct {
	Hex     string `json:"hex"`
	Bytes   int    `json:"bytes"`
	Index   int    `json:"index"` // -1 when the parameter is not table-indexed
	Packet  string `json:"packet"`
	Payload string `json:"payload,omitempty"`
}

// encodeParam describes one parameter value, or reports that the effect or
// parameter is unknown.
func encodeParam(effectID, paramID byte, value float64) (Encoding, bool) {
	def, ok := dsp.Effects[effectID]
	if !ok {
		return Encoding{}, false
	}

	for _, p := range def.Params {
		if p.ParamID != paramID {
			continue
		}

		payload := p.EncodeFn(value)
		idx := -1
		if p.IndexFn != nil {
			idx = p.IndexFn(value)
		}

		packet := protocol.BuildSetPacket(effectID, paramID, payload)

		return Encoding{
			Hex:     hex.EncodeToString(payload),
			Bytes:   len(payload),
			Index:   idx,
			Packet:  hex.EncodeToString(packet[:]),
			Payload: p.FormatFn(value),
		}, true
	}
	return Encoding{}, false
}
