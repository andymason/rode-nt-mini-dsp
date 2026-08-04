package protocol

import (
	"bytes"
	"testing"
)

func TestBuildPacketLayout(t *testing.T) {
	p := BuildPacket(EffAE, CmdSet, 0x02, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	if len(p) != PacketSize {
		t.Fatalf("packet is %d bytes, want %d", len(p), PacketSize)
	}
	if p[0] != ReportID {
		t.Errorf("byte 0 (report ID) = %#02x, want %#02x", p[0], ReportID)
	}
	if p[1] != EffAE {
		t.Errorf("byte 1 (effect) = %#02x, want %#02x", p[1], EffAE)
	}
	if p[2] != CmdSet {
		t.Errorf("byte 2 (command) = %#02x, want %#02x", p[2], CmdSet)
	}
	if p[3] != 0x02 {
		t.Errorf("byte 3 (param) = %#02x, want 0x02", p[3])
	}
	if got := p[4:8]; !bytes.Equal(got, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Errorf("payload = % x, want de ad be ef", got)
	}
	// Everything past the payload must be zero: the device reads a fixed-size
	// report, and the February 2026 Big Bottom tune capture showed stale buffer
	// bytes being misread as part of the value.
	for i := 8; i < PacketSize; i++ {
		if p[i] != 0 {
			t.Errorf("byte %d = %#02x, want zero padding", i, p[i])
		}
	}
}

func TestBuildInitPacketIsZeroed(t *testing.T) {
	p := BuildInitPacket(EffComp, 0x03)

	if p[2] != CmdInit {
		t.Errorf("command = %#02x, want CmdInit %#02x", p[2], CmdInit)
	}
	for i := 4; i < PacketSize; i++ {
		if p[i] != 0 {
			t.Errorf("byte %d = %#02x, want zero (INIT carries no value)", i, p[i])
		}
	}
}

func TestBuildEnablePacket(t *testing.T) {
	on := BuildEnablePacket(EffGate, true)
	off := BuildEnablePacket(EffGate, false)

	for _, tc := range []struct {
		name string
		p    [PacketSize]byte
		want byte
	}{
		{"on", on, 0x01},
		{"off", off, 0x00},
	} {
		if tc.p[2] != CmdSet {
			t.Errorf("%s: command = %#02x, want CmdSet", tc.name, tc.p[2])
		}
		if tc.p[3] != 0x00 {
			t.Errorf("%s: param = %#02x, want 0x00 (enable)", tc.name, tc.p[3])
		}
		if tc.p[4] != tc.want {
			t.Errorf("%s: value = %#02x, want %#02x", tc.name, tc.p[4], tc.want)
		}
	}
}

// TestPayloadWidths pins the irregular encodings. Aural Exciter tune carries two
// LUT values plus a trailing index (9 bytes) and harmonics/drive carry one LUT
// value plus a trailing index (5 bytes); everything else is a bare 4-byte value
// or a single index byte. Getting a width wrong silently shifts the index byte
// into padding, which the device would read as zero.
func TestPayloadWidths(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		want  int
	}{
		{"CompThreshold", EncodeCompThreshold(-20), 4},
		{"CompRatio", EncodeCompRatio(3.0), 1},
		{"CompAttack", EncodeCompAttack(0.7), 4},
		{"CompRelease", EncodeCompRelease(21), 4},
		{"CompGain", EncodeCompGain(2.0), 4},
		{"NGThreshold", EncodeNGThreshold(-42), 4},
		{"NGAttack", EncodeNGAttack(0.8), 4},
		{"NGHold", EncodeNGHold(80), 4},
		{"NGRelease", EncodeNGRelease(210), 4},
		{"NGRange", EncodeNGRange(-9), 4},
		{"NGHysteresis", EncodeNGHysteresis(50), 4},
		{"AEHarmonics", EncodeAEHarmonics(49), 5},
		{"AETune", EncodeAETune(3516), 9},
		{"BBDrive", EncodeBBDrive(62), 5},
		{"BBTune", EncodeBBTune(131), 1},
	}

	for _, tc := range cases {
		if got := len(tc.bytes); got != tc.want {
			t.Errorf("%s: payload is %d bytes, want %d", tc.name, got, tc.want)
		}
	}
}

// TestTrailingIndexByteSurvivesPacketBuild guards the 5- and 9-byte payloads
// specifically: the trailing index must land inside the packet, not in padding.
func TestTrailingIndexByteSurvivesPacketBuild(t *testing.T) {
	harmonics := BuildSetPacket(EffAE, 0x01, EncodeAEHarmonics(100))
	if harmonics[8] != 0xFF {
		t.Errorf("AE harmonics index byte = %#02x at packet[8], want 0xff", harmonics[8])
	}

	tune := BuildSetPacket(EffAE, 0x02, EncodeAETune(5000))
	if tune[12] != 0xFF {
		t.Errorf("AE tune index byte = %#02x at packet[12], want 0xff", tune[12])
	}
}
