package protocol

// BuildPacket creates a USB HID packet with the given parameters
func BuildPacket(effectID, cmd, paramID byte, value []byte) [PacketSize]byte {
	var packet [PacketSize]byte
	packet[0] = ReportID
	packet[1] = effectID
	packet[2] = cmd
	packet[3] = paramID

	// Copy value bytes
	copy(packet[4:], value)
	// Remaining bytes are already zero (array initialized to zero)

	return packet
}

// BuildInitPacket creates an INIT command packet (cmd = CmdInit)
func BuildInitPacket(effectID, paramID byte) [PacketSize]byte {
	// INIT packets have empty value field (all zeros)
	return BuildPacket(effectID, CmdInit, paramID, []byte{})
}

// BuildEnablePacket creates a SET command packet to enable/disable an effect
func BuildEnablePacket(effectID byte, enabled bool) [PacketSize]byte {
	enableByte := byte(0x00)
	if enabled {
		enableByte = 0x01
	}
	return BuildPacket(effectID, CmdSet, 0x00, []byte{enableByte})
}

// BuildSetPacket creates a SET command packet for a parameter with value
func BuildSetPacket(effectID, paramID byte, value []byte) [PacketSize]byte {
	return BuildPacket(effectID, CmdSet, paramID, value)
}
