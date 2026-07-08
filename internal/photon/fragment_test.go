package photon

import (
	"encoding/binary"
	"testing"
)

// fragPacket builds a single-command Photon packet carrying one SendFragment (type 8).
func fragPacket(startSeq, count, num, totalLen, fragOff uint32, data []byte) []byte {
	pkt := make([]byte, 12) // peerId(2) flags(1) commandCount(1) ts+challenge(8)
	pkt[3] = 1              // one command

	cmd := make([]byte, 12) // type(1) channel(1) flags(1) reserved(1) cmdLen(4) reliableSeq(4)
	cmd[0] = cmdSendFragment
	payloadLen := 20 + len(data)                               // fragment header + data
	binary.BigEndian.PutUint32(cmd[4:], uint32(12+payloadLen)) // cmdLen includes the 12-byte header

	fh := make([]byte, 20)
	binary.BigEndian.PutUint32(fh[0:], startSeq)
	binary.BigEndian.PutUint32(fh[4:], count)
	binary.BigEndian.PutUint32(fh[8:], num)
	binary.BigEndian.PutUint32(fh[12:], totalLen)
	binary.BigEndian.PutUint32(fh[16:], fragOff)

	out := append(pkt, cmd...)
	out = append(out, fh...)
	return append(out, data...)
}

// reassembled payload = SendReliable body: signalByte(0) + msgType(MsgEvent) + code + empty param table.
var eventBody = []byte{0x00, MsgEvent, 42, 0x00}

func TestFragmentReassembly(t *testing.T) {
	var gotCode byte
	calls := 0
	p := NewPhotonParser(nil, nil, func(code byte, _ map[byte]interface{}) { gotCode = code; calls++ })

	// Split the 4-byte body into two 2-byte fragments.
	p.ReceivePacket(fragPacket(100, 2, 0, 4, 0, eventBody[0:2]))
	if calls != 0 {
		t.Fatal("must not dispatch before all fragments arrive")
	}
	p.ReceivePacket(fragPacket(100, 2, 1, 4, 2, eventBody[2:4]))

	if calls != 1 || gotCode != 42 {
		t.Fatalf("expected one event code 42 after reassembly, calls=%d code=%d", calls, gotCode)
	}
	if p.PendingSegments() != 0 {
		t.Fatalf("segment buffer not cleared: %d", p.PendingSegments())
	}
}

func TestFragmentReassemblyDedupsRetransmit(t *testing.T) {
	calls := 0
	p := NewPhotonParser(nil, nil, func(byte, map[byte]interface{}) { calls++ })

	// A retransmitted fragment 0 must NOT double-count bytes and falsely "complete" the
	// message (the pre-fix bug that lost the large GetMailInfos response).
	p.ReceivePacket(fragPacket(200, 2, 0, 4, 0, eventBody[0:2]))
	p.ReceivePacket(fragPacket(200, 2, 0, 4, 0, eventBody[0:2])) // retransmit of #0
	if calls != 0 {
		t.Fatalf("retransmit must not complete the message (calls=%d)", calls)
	}
	p.ReceivePacket(fragPacket(200, 2, 1, 4, 2, eventBody[2:4])) // the real #1
	if calls != 1 {
		t.Fatalf("expected exactly one dispatch after the genuine final fragment, got %d", calls)
	}
}
