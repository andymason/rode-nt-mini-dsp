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

// BuildGetPacket creates a read request for one parameter. The value field is
// unused and sent as zeros, which is why these packets look like an "init" or
// "clear" in a capture; the device replies with the parameter's current value.
func BuildGetPacket(effectID, paramID byte) [PacketSize]byte {
	return BuildPacket(effectID, CmdGet, paramID, []byte{})
}

// BuildGetAllPacket creates a read request for every parameter of an effect at
// once. RØDE Connect uses this form only on the older firmware path
// (FUN_140102d40 false); the NT-USB Mini answers it regardless.
func BuildGetAllPacket(effectID byte) [PacketSize]byte {
	return BuildPacket(effectID, CmdGetAll, 0x00, []byte{})
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
