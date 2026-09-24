package protocol

const (
	VID      = 0x19F7
	PID      = 0x0015
	ReportID = 0x04
	AckByte  = 0x41

	AckTimeoutMs = 50
	PacketSize   = 29 // 1 Report ID + 28 payload
	PayloadSize  = 28

	EffComp = 0x00
	EffGate = 0x01
	EffAE   = 0x02
	EffBB   = 0x03
)

// Commands occupy byte 2 of the packet. They form two symmetric pairs, bulk and
// per-parameter, verified in RØDE Connect.exe: the readers FUN_140374630 (Comp),
// FUN_140372eb0 (Gate), FUN_1403725a0 (AE) and FUN_140371d70 (BB) build the GET
// forms, and the encoders at FUN_140375130 / FUN_1403738a0 / FUN_140372910 /
// FUN_140372080 build the SET forms. Which pair a device uses is decided by the
// firmware check at FUN_140102d40; the NT-USB Mini takes the per-parameter path.
//
// The all-zero 0x03 packets RØDE Connect sends at startup are reads: the device
// answers each with the current coefficient. See docs/protocol.md.
const (
	CmdSetAll = 0x00 // bulk write, every parameter of an effect in one packet
	CmdGetAll = 0x01 // bulk read, every parameter of an effect in one reply
	CmdSet    = 0x02 // write one parameter
	CmdGet    = 0x03 // read one parameter
)

// ParamCounts is the number of readable parameters per effect, param IDs 0x00
// upwards. These are exactly the counts RØDE Connect reads at startup, and the
// counts its readers iterate to.
//
// Note Aural Exciter's 4 covers param 0x03, which no UI exposes and which the
// SET encoder never writes. Big Bottom reads 3, but the device does answer a
// read of BB param 0x03 — see docs/protocol.md.
var ParamCounts = map[byte]int{
	EffComp: 6,
	EffGate: 7,
	EffAE:   4,
	EffBB:   3,
}
