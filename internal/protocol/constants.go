package protocol

const (
	VID      = 0x19F7
	PID      = 0x0015
	ReportID = 0x04
	CmdSet   = 0x02
	CmdInit  = 0x03
	AckByte  = 0x41

	AckTimeoutMs = 50
	PacketSize   = 29 // 1 Report ID + 28 payload

	EffComp = 0x00
	EffGate = 0x01
	EffAE   = 0x02
	EffBB   = 0x03
)

// InitCounts is the number of INIT packets per effect (includes hidden params).
var InitCounts = map[byte]int{
	EffComp: 6,
	EffGate: 7,
	EffAE:   4,
	EffBB:   3,
}
