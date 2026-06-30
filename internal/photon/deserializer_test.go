package photon

import (
	"reflect"
	"testing"
)

// TestRoundTripEvent encodes an EventData with mixed types, decodes it, and
// asserts the parameter table round-trips exactly (golden by construction).
func TestRoundTripEvent(t *testing.T) {
	const evCode = byte(143)
	fields := []Field{
		{Key: 0, Type: TypeByte, Val: byte(7)},
		{Key: 1, Type: TypeString, Val: "Hideout"},
		{Key: 2, Type: TypeShort, Val: int16(-12)},
		{Key: 3, Type: TypeInteger, Val: int32(123456)},
		{Key: 4, Type: TypeArray | TypeString, Val: []string{"T4_BAG", "T5_BAG"}},
		{Key: 5, Type: TypeArray | TypeInteger, Val: []int32{10, 20, 30}},
	}
	payload := BuildEventPacket(evCode, fields)

	var gotCode byte
	var gotParams map[byte]interface{}
	p := NewPhotonParser(nil, nil, func(code byte, params map[byte]interface{}) {
		gotCode, gotParams = code, params
	})
	if ok := p.ReceivePacket(payload); !ok {
		t.Fatalf("ReceivePacket returned false for valid event")
	}
	if gotCode != evCode {
		t.Fatalf("event code = %d, want %d", gotCode, evCode)
	}
	want := map[byte]interface{}{
		0: byte(7),
		1: "Hideout",
		2: int16(-12),
		3: int32(123456),
		4: []string{"T4_BAG", "T5_BAG"},
		5: []int32{10, 20, 30},
	}
	if !reflect.DeepEqual(gotParams, want) {
		t.Fatalf("params mismatch:\n got=%#v\nwant=%#v", gotParams, want)
	}
}

// TestRoundTripResponse covers OperationResponse with a return code.
func TestRoundTripResponse(t *testing.T) {
	payload := BuildResponsePacket(101, 0, []Field{
		{Key: 1, Type: TypeString, Val: "ok"},
	})
	var gotOp byte
	var gotRC int16
	var gotParams map[byte]interface{}
	p := NewPhotonParser(nil, func(op byte, rc int16, _ string, params map[byte]interface{}) {
		gotOp, gotRC, gotParams = op, rc, params
	}, nil)
	if ok := p.ReceivePacket(payload); !ok {
		t.Fatal("ReceivePacket false for valid response")
	}
	if gotOp != 101 || gotRC != 0 || gotParams[1] != "ok" {
		t.Fatalf("response mismatch: op=%d rc=%d params=%v", gotOp, gotRC, gotParams)
	}
}

// TestEncryptedFlagged ensures an encrypted packet fires OnEncrypted and is not
// decoded into garbage (FR-008).
func TestEncryptedFlagged(t *testing.T) {
	encryptedSeen := false
	p := NewPhotonParser(nil, nil, func(byte, map[byte]interface{}) {
		t.Fatal("encrypted packet must not produce an event")
	})
	p.OnEncrypted = func() { encryptedSeen = true }
	if ok := p.ReceivePacket(BuildEncryptedPacket()); ok {
		t.Fatal("encrypted packet should report not-ok")
	}
	if !encryptedSeen {
		t.Fatal("OnEncrypted not fired")
	}
}

// FuzzDeserialize asserts the parser never panics on arbitrary bytes
// (Constitution Principle IV: untrusted-input resilience).
func FuzzDeserialize(f *testing.F) {
	f.Add(BuildEventPacket(143, []Field{{Key: 1, Type: TypeString, Val: "x"}}))
	f.Add(BuildResponsePacket(1, 0, nil))
	f.Add(BuildEncryptedPacket())
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		p := NewPhotonParser(
			func(byte, map[byte]interface{}) {},
			func(byte, int16, string, map[byte]interface{}) {},
			func(byte, map[byte]interface{}) {},
		)
		p.OnEncrypted = func() {}
		_ = p.ReceivePacket(data) // must not panic
	})
}
