// Package photon implements a pure-Go decoder for the game's Photon
// (Protocol16/18) network messages. Ported and adapted from the AODP Go client
// (itself a port of the C# Protocol18Deserializer). No infrastructure deps.
package photon

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
)

// Exported type code aliases for use in tests and the encoder.
const (
	TypeNull    = typeNull
	TypeBoolean = typeBoolean
	TypeByte    = typeByte
	TypeShort   = typeShort
	TypeInteger = typeCompressedInt
	TypeString  = typeString
	TypeIntZero = typeIntZero
	TypeArray   = typeArray
	TypeFloat   = typeFloat
	TypeInt64   = typeCompressedLong
)

// Protocol18 type codes (match the C# Protocol18Type enum).
const (
	typeUnknown          = byte(0)
	typeBoolean          = byte(2)
	typeByte             = byte(3)
	typeShort            = byte(4)
	typeFloat            = byte(5)
	typeDouble           = byte(6)
	typeString           = byte(7)
	typeNull             = byte(8)
	typeCompressedInt    = byte(9)
	typeCompressedLong   = byte(10)
	typeInt1             = byte(11)
	typeInt1Neg          = byte(12)
	typeInt2             = byte(13)
	typeInt2Neg          = byte(14)
	typeLong1            = byte(15)
	typeLong1Neg         = byte(16)
	typeLong2            = byte(17)
	typeLong2Neg         = byte(18)
	typeCustom           = byte(19)
	typeDictionary       = byte(20)
	typeHashtable        = byte(21)
	typeObjectArray      = byte(23)
	typeOperationRequest = byte(24)
	typeOperationResp    = byte(25)
	typeEventData        = byte(26)
	typeBoolFalse        = byte(27)
	typeBoolTrue         = byte(28)
	typeShortZero        = byte(29)
	typeIntZero          = byte(30)
	typeLongZero         = byte(31)
	typeFloatZero        = byte(32)
	typeDoubleZero       = byte(33)
	typeByteZero         = byte(34)
	typeArray            = byte(0x40)
	customTypeSlimBase   = byte(0x80)
)

// maxNodes bounds the TOTAL number of values decoded from one top-level message.
// A depth cap alone is not enough: nested arrays-of-dictionaries fan out
// exponentially, so a crafted message could hang. The shared budget bounds total
// work — preventing both deep recursion and exponential fan-out (Principle IV).
// 200k is far above any real Albion message yet aborts a decompression bomb fast.
const maxNodes = 200_000

// deserializeParameterTable parses a Protocol18 parameter table from raw bytes.
// Wire format: compressed-varint count | (key | typeCode | value)*
func deserializeParameterTable(data []byte) map[byte]interface{} {
	budget := maxNodes
	return readParameterTable(bytes.NewBuffer(data), &budget)
}

func readParameterTable(buf *bytes.Buffer, budget *int) map[byte]interface{} {
	count := int(readCount(buf))
	params := make(map[byte]interface{}, clampCap(count))
	for i := 0; i < count && buf.Len() > 0 && *budget > 0; i++ {
		key, err := buf.ReadByte()
		if err != nil {
			break
		}
		tc, err := buf.ReadByte()
		if err != nil {
			break
		}
		params[key] = deserialize(buf, tc, budget)
	}
	return params
}

// deserialize decodes a single Protocol18 value. budget is a shared remaining-
// node counter; when exhausted, decoding stops and returns nil.
func deserialize(buf *bytes.Buffer, tc byte, budget *int) interface{} {
	if *budget <= 0 {
		return nil
	}
	*budget--
	if tc >= customTypeSlimBase {
		return deserializeCustom(buf, tc)
	}
	switch tc {
	case typeUnknown, typeNull:
		return nil
	case typeBoolean:
		b, _ := buf.ReadByte()
		return b != 0
	case typeByte:
		b, _ := buf.ReadByte()
		return b
	case typeShort:
		return readInt16(buf)
	case typeFloat:
		return readFloat32(buf)
	case typeDouble:
		return readFloat64(buf)
	case typeString:
		return readString(buf)
	case typeCompressedInt:
		return readCompressedInt32(buf)
	case typeCompressedLong:
		return readCompressedInt64(buf)
	case typeInt1:
		b, _ := buf.ReadByte()
		return int32(b)
	case typeInt1Neg:
		b, _ := buf.ReadByte()
		return -int32(b)
	case typeInt2:
		return int32(readUint16(buf))
	case typeInt2Neg:
		return -int32(readUint16(buf))
	case typeLong1:
		b, _ := buf.ReadByte()
		return int64(b)
	case typeLong1Neg:
		b, _ := buf.ReadByte()
		return -int64(b)
	case typeLong2:
		return int64(readUint16(buf))
	case typeLong2Neg:
		return -int64(readUint16(buf))
	case typeCustom:
		return deserializeCustom(buf, 0)
	case typeDictionary:
		return deserializeDictionary(buf, budget)
	case typeHashtable:
		return deserializeHashtable(buf, budget)
	case typeObjectArray:
		return deserializeObjectArray(buf, budget)
	case typeOperationRequest:
		return deserializeOperationRequestInner(buf, budget)
	case typeOperationResp:
		return deserializeOperationResponseInner(buf, budget)
	case typeEventData:
		return deserializeEventDataInner(buf, budget)
	case typeBoolFalse:
		return false
	case typeBoolTrue:
		return true
	case typeShortZero:
		return int16(0)
	case typeIntZero:
		return int32(0)
	case typeLongZero:
		return int64(0)
	case typeFloatZero:
		return float32(0)
	case typeDoubleZero:
		return float64(0)
	case typeByteZero:
		return byte(0)
	case typeArray:
		return deserializeNestedArray(buf, budget)
	default:
		if tc&typeArray == typeArray {
			return deserializeTypedArray(buf, tc&^typeArray, budget)
		}
		return fmt.Sprintf("ERROR - unknown type 0x%02X", tc)
	}
}

// boundedSize clamps an element count to what the buffer could possibly hold and
// to the remaining node budget, so make() can never request a huge slice.
func boundedSize(raw int, buf *bytes.Buffer, budget *int) int {
	if raw < 0 {
		return 0
	}
	if raw > buf.Len()+1 {
		raw = buf.Len() + 1
	}
	if raw > *budget {
		raw = *budget
	}
	return raw
}

func deserializeTypedArray(buf *bytes.Buffer, elemType byte, budget *int) interface{} {
	size := boundedSize(int(readCount(buf)), buf, budget)
	switch elemType {
	case typeBoolean:
		result := make([]bool, size)
		packed := make([]byte, (size+7)/8)
		buf.Read(packed)
		for i := 0; i < size; i++ {
			result[i] = (packed[i/8] & (1 << uint(i%8))) != 0
		}
		return result
	case typeByte:
		data := make([]byte, size)
		buf.Read(data)
		return data
	case typeShort:
		result := make([]int16, size)
		for i := range result {
			result[i] = readInt16(buf)
		}
		return result
	case typeFloat:
		result := make([]float32, size)
		for i := range result {
			result[i] = readFloat32(buf)
		}
		return result
	case typeDouble:
		result := make([]float64, size)
		for i := range result {
			result[i] = readFloat64(buf)
		}
		return result
	case typeString:
		result := make([]string, size)
		for i := range result {
			result[i] = readString(buf)
		}
		return result
	case typeCustom:
		customTypeID, _ := buf.ReadByte()
		result := make([]interface{}, size)
		for i := range result {
			result[i] = deserializeCustomPayload(buf, customTypeID, false)
		}
		return result
	case typeDictionary:
		result := make([]interface{}, size)
		for i := range result {
			result[i] = deserializeDictionary(buf, budget)
		}
		return result
	case typeHashtable:
		result := make([]interface{}, size)
		for i := range result {
			result[i] = deserializeHashtable(buf, budget)
		}
		return result
	case typeCompressedInt:
		result := make([]int32, size)
		for i := range result {
			result[i] = readCompressedInt32(buf)
		}
		return result
	case typeCompressedLong:
		result := make([]int64, size)
		for i := range result {
			result[i] = readCompressedInt64(buf)
		}
		return result
	default:
		result := make([]interface{}, size)
		for i := range result {
			result[i] = deserialize(buf, elemType, budget)
		}
		return result
	}
}

func deserializeNestedArray(buf *bytes.Buffer, budget *int) interface{} {
	size := int(readCount(buf))
	tc, err := buf.ReadByte()
	if err != nil {
		return nil
	}
	size = boundedSize(size, buf, budget)
	result := make([]interface{}, size)
	for i := range result {
		result[i] = deserialize(buf, tc, budget)
	}
	return result
}

func deserializeObjectArray(buf *bytes.Buffer, budget *int) interface{} {
	size := boundedSize(int(readCount(buf)), buf, budget)
	result := make([]interface{}, size)
	for i := range result {
		tc, err := buf.ReadByte()
		if err != nil {
			break
		}
		result[i] = deserialize(buf, tc, budget)
	}
	return result
}

func deserializeDictionary(buf *bytes.Buffer, budget *int) map[interface{}]interface{} {
	keyTC, _ := buf.ReadByte()
	valTC, _ := buf.ReadByte()
	count := int(readCount(buf))
	out := make(map[interface{}]interface{}, clampCap(count))
	for i := 0; i < count && buf.Len() > 0 && *budget > 0; i++ {
		var kt byte
		if keyTC == 0 {
			kt, _ = buf.ReadByte()
		} else {
			kt = keyTC
		}
		var vt byte
		if valTC == 0 {
			vt, _ = buf.ReadByte()
		} else {
			vt = valTC
		}
		key := deserialize(buf, kt, budget)
		val := deserialize(buf, vt, budget)
		if isComparable(key) {
			out[key] = val
		} else {
			out[fmt.Sprintf("UNHASHABLE_%d_%T", i, key)] = val
		}
	}
	return out
}

func deserializeHashtable(buf *bytes.Buffer, budget *int) map[interface{}]interface{} {
	return deserializeDictionary(buf, budget)
}

func deserializeCustom(buf *bytes.Buffer, gpType byte) interface{} {
	var customID byte
	isSlim := gpType >= customTypeSlimBase
	if isSlim {
		customID = gpType & 0x7F
	} else {
		customID, _ = buf.ReadByte()
	}
	return deserializeCustomPayload(buf, customID, isSlim)
}

func deserializeCustomPayload(buf *bytes.Buffer, customID byte, isSlim bool) interface{} {
	size := int(readCount(buf))
	if size < 0 || size > buf.Len() {
		if isSlim {
			data := make([]byte, buf.Len())
			buf.Read(data)
			return map[string]interface{}{"type": customID, "data": data}
		}
		return nil
	}
	data := make([]byte, size)
	buf.Read(data)
	return map[string]interface{}{"type": customID, "data": data}
}

func deserializeOperationRequestInner(buf *bytes.Buffer, budget *int) interface{} {
	opCode, _ := buf.ReadByte()
	params := readParameterTable(buf, budget)
	return map[string]interface{}{"operationCode": opCode, "parameters": params}
}

func deserializeOperationResponseInner(buf *bytes.Buffer, budget *int) interface{} {
	if buf.Len() < 3 {
		return nil
	}
	opCode, _ := buf.ReadByte()
	returnCode := readInt16(buf)
	debugMsg := ""
	if buf.Len() > 0 {
		tc, _ := buf.ReadByte()
		if v, ok := deserialize(buf, tc, budget).(string); ok {
			debugMsg = v
		}
	}
	params := readParameterTable(buf, budget)
	return map[string]interface{}{
		"operationCode": opCode,
		"returnCode":    returnCode,
		"debugMessage":  debugMsg,
		"parameters":    params,
	}
}

func deserializeEventDataInner(buf *bytes.Buffer, budget *int) interface{} {
	code, _ := buf.ReadByte()
	params := readParameterTable(buf, budget)
	return map[string]interface{}{"code": code, "parameters": params}
}

// ── low-level readers ────────────────────────────────────────────────────────

func readInt16(buf *bytes.Buffer) int16 {
	var v int16
	binary.Read(buf, binary.LittleEndian, &v)
	return v
}

func readUint16(buf *bytes.Buffer) uint16 {
	var v uint16
	binary.Read(buf, binary.LittleEndian, &v)
	return v
}

func readFloat32(buf *bytes.Buffer) float32 {
	var bits uint32
	binary.Read(buf, binary.LittleEndian, &bits)
	return math.Float32frombits(bits)
}

func readFloat64(buf *bytes.Buffer) float64 {
	var bits uint64
	binary.Read(buf, binary.LittleEndian, &bits)
	return math.Float64frombits(bits)
}

func readString(buf *bytes.Buffer) string {
	length := int(readCompressedUint32(buf))
	if length <= 0 || length > buf.Len() {
		return ""
	}
	b := make([]byte, length)
	buf.Read(b)
	return string(b)
}

func readCount(buf *bytes.Buffer) uint32 {
	return readCompressedUint32(buf)
}

func readCompressedUint32(buf *bytes.Buffer) uint32 {
	var value uint32
	shift := uint(0)
	for {
		b, err := buf.ReadByte()
		if err != nil {
			return 0
		}
		value |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return value
		}
		shift += 7
		if shift >= 35 {
			return 0
		}
	}
}

func readCompressedUint64(buf *bytes.Buffer) uint64 {
	var value uint64
	shift := uint(0)
	for {
		b, err := buf.ReadByte()
		if err != nil {
			return 0
		}
		value |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return value
		}
		shift += 7
		if shift >= 70 {
			return 0
		}
	}
}

func readCompressedInt32(buf *bytes.Buffer) int32 {
	v := readCompressedUint32(buf)
	return int32((v >> 1) ^ uint32(-(int32(v & 1))))
}

func readCompressedInt64(buf *bytes.Buffer) int64 {
	v := readCompressedUint64(buf)
	return int64((v >> 1) ^ uint64(-(int64(v & 1))))
}

func isComparable(v interface{}) bool {
	if v == nil {
		return true
	}
	return reflect.TypeOf(v).Comparable()
}

// clampCap bounds a make() capacity hint so a corrupt length can't request a
// huge allocation (Principle IV/XI: untrusted input must not balloon memory).
func clampCap(n int) int {
	const maxHint = 1024
	if n < 0 {
		return 0
	}
	if n > maxHint {
		return maxHint
	}
	return n
}
