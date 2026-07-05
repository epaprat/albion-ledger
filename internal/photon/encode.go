package photon

import (
	"encoding/binary"
	"math"
)

// This encoder produces valid Photon payloads for tests and synthetic fixtures.
// It is the inverse of the decoder for the subset of types we craft. Encoding
// real game traffic is out of scope — the game is the encoder in production.

// Field is one parameter-table entry to encode.
type Field struct {
	Key  byte
	Type byte
	Val  interface{}
}

// BuildEventPacket builds a full Photon UDP payload containing one reliable
// EventData with the given code and parameters.
func BuildEventPacket(code byte, fields []Field) []byte {
	body := append([]byte{code}, encodeParamTable(fields)...)
	return buildPacket(reliableCommand(MsgEvent, body))
}

// BuildResponsePacket builds a payload containing one reliable OperationResponse.
func BuildResponsePacket(opCode byte, returnCode int16, fields []Field) []byte {
	body := []byte{opCode}
	body = binary.LittleEndian.AppendUint16(body, uint16(returnCode))
	body = append(body, typeNull) // empty debug message
	body = append(body, encodeParamTable(fields)...)
	return buildPacket(reliableCommand(MsgResponse, body))
}

// BuildRequestPacket builds a payload containing one reliable OperationRequest.
func BuildRequestPacket(opCode byte, fields []Field) []byte {
	body := append([]byte{opCode}, encodeParamTable(fields)...)
	return buildPacket(reliableCommand(MsgRequest, body))
}

// BuildEncryptedPacket builds a payload flagged as fully encrypted.
func BuildEncryptedPacket() []byte {
	hdr := make([]byte, photonHeaderLength)
	hdr[2] = 1 // flags == 1 → encrypted
	hdr[3] = 0 // commandCount
	return hdr
}

// buildPacket wraps one or more command blocks in a Photon header.
func buildPacket(commands ...[]byte) []byte {
	hdr := make([]byte, photonHeaderLength)
	hdr[2] = 0                    // flags
	hdr[3] = byte(len(commands))  // commandCount
	out := hdr
	for _, c := range commands {
		out = append(out, c...)
	}
	return out
}

// reliableCommand frames a message body as a SendReliable command.
func reliableCommand(msgType byte, body []byte) []byte {
	payload := append([]byte{0, msgType}, body...) // signalByte + msgType
	cmdLen := commandHeaderLength + len(payload)
	cmd := make([]byte, 0, cmdLen)
	cmd = append(cmd, cmdSendReliable, 0, 0, 0) // type, channel, flags, reserved
	cmd = binary.BigEndian.AppendUint32(cmd, uint32(cmdLen))
	cmd = binary.BigEndian.AppendUint32(cmd, 0) // reliableSequenceNumber
	cmd = append(cmd, payload...)
	return cmd
}

func encodeParamTable(fields []Field) []byte {
	out := appendCompressedUint32(nil, uint32(len(fields)))
	for _, f := range fields {
		out = append(out, f.Key, f.Type)
		out = append(out, encodeValue(f.Type, f.Val)...)
	}
	return out
}

func encodeValue(tc byte, val interface{}) []byte {
	switch tc {
	case typeByte:
		return []byte{val.(byte)}
	case typeBoolean:
		if val.(bool) {
			return []byte{1}
		}
		return []byte{0}
	case typeShort:
		return binary.LittleEndian.AppendUint16(nil, uint16(val.(int16)))
	case typeCompressedInt:
		return appendCompressedInt32(nil, val.(int32))
	case typeString:
		s := val.(string)
		out := appendCompressedUint32(nil, uint32(len(s)))
		return append(out, s...)
	case typeFloat:
		return binary.LittleEndian.AppendUint32(nil, math.Float32bits(val.(float32)))
	default:
		if tc&typeArray == typeArray {
			return encodeTypedArray(tc&^typeArray, val)
		}
		return nil
	}
}

func encodeTypedArray(elemType byte, val interface{}) []byte {
	switch elemType {
	case typeByte:
		arr := val.([]byte)
		out := appendCompressedUint32(nil, uint32(len(arr)))
		return append(out, arr...)
	case typeShort:
		arr := val.([]int16)
		out := appendCompressedUint32(nil, uint32(len(arr)))
		for _, n := range arr {
			out = binary.LittleEndian.AppendUint16(out, uint16(n))
		}
		return out
	case typeCompressedLong:
		arr := val.([]int64)
		out := appendCompressedUint32(nil, uint32(len(arr)))
		for _, n := range arr {
			out = appendCompressedInt64(out, n)
		}
		return out
	case typeFloat:
		arr := val.([]float32)
		out := appendCompressedUint32(nil, uint32(len(arr)))
		for _, f := range arr {
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(f))
		}
		return out
	case typeString:
		arr := val.([]string)
		out := appendCompressedUint32(nil, uint32(len(arr)))
		for _, s := range arr {
			out = appendCompressedUint32(out, uint32(len(s)))
			out = append(out, s...)
		}
		return out
	case typeCompressedInt:
		arr := val.([]int32)
		out := appendCompressedUint32(nil, uint32(len(arr)))
		for _, n := range arr {
			out = appendCompressedInt32(out, n)
		}
		return out
	default:
		return nil
	}
}

func appendCompressedUint32(dst []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

func appendCompressedInt32(dst []byte, n int32) []byte {
	zig := (uint32(n) << 1) ^ uint32(n>>31)
	return appendCompressedUint32(dst, zig)
}

func appendCompressedInt64(dst []byte, n int64) []byte {
	zig := (uint64(n) << 1) ^ uint64(n>>63)
	for {
		b := byte(zig & 0x7F)
		zig >>= 7
		if zig != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if zig == 0 {
			return dst
		}
	}
}
