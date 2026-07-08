package photon

import (
	"bytes"
	"encoding/binary"
)

const (
	photonHeaderLength   = 12
	commandHeaderLength  = 12
	fragmentHeaderLength = 20

	// maxPendingSegments bounds the fragment-reassembly buffer. A never-completed
	// fragment can never accumulate past this cap (Constitution Principle XI:
	// no unbounded growth). Oldest pending segment is evicted FIFO when over cap.
	maxPendingSegments = 64
)

// Photon command type constants.
const (
	cmdDisconnect     = byte(4)
	cmdSendReliable   = byte(6)
	cmdSendUnreliable = byte(7)
	cmdSendFragment   = byte(8)
)

// Photon reliable message type constants (exported for the encoder/tests).
const (
	MsgRequest     = byte(2)
	MsgResponse    = byte(3)
	MsgEvent       = byte(4)
	msgResponseAlt = byte(7)
	msgEncrypted   = byte(131)
)

type segmentedPackage struct {
	totalLength   int
	fragmentCount int
	received      map[int]bool // distinct fragment numbers seen (dedups reliable retransmits)
	payload       []byte
}

// PhotonParser decodes raw Photon UDP payloads and fires callbacks per message.
type PhotonParser struct {
	pendingSegments map[int]*segmentedPackage
	segOrder        []int // FIFO of startSeq for bounded eviction

	OnRequest   func(operationCode byte, params map[byte]interface{})
	OnResponse  func(operationCode byte, returnCode int16, debugMessage string, params map[byte]interface{})
	OnEvent     func(code byte, params map[byte]interface{})
	OnEncrypted func()
}

// NewPhotonParser creates a PhotonParser wired to the given callbacks.
func NewPhotonParser(
	onRequest func(byte, map[byte]interface{}),
	onResponse func(byte, int16, string, map[byte]interface{}),
	onEvent func(byte, map[byte]interface{}),
) *PhotonParser {
	return &PhotonParser{
		pendingSegments: make(map[int]*segmentedPackage),
		OnRequest:       onRequest,
		OnResponse:      onResponse,
		OnEvent:         onEvent,
	}
}

// ReceivePacket processes one raw Photon payload and fires the appropriate
// callbacks. It NEVER panics on malformed/adversarial input: a recover guard
// turns any unexpected condition into a dropped packet (Principle IV).
// Returns true if the packet header was valid and fully consumed.
func (p *PhotonParser) ReceivePacket(payload []byte) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	if len(payload) < photonHeaderLength {
		return false
	}

	offset := 2 // skip peerId
	flags := payload[offset]
	offset++
	commandCount := int(payload[offset])
	offset++
	offset += 8 // skip timestamp + challenge

	if flags == 1 {
		// Whole packet encrypted — surface, never decode into garbage.
		if p.OnEncrypted != nil {
			p.OnEncrypted()
		}
		return false
	}

	for i := 0; i < commandCount; i++ {
		var good bool
		offset, good = p.handleCommand(payload, offset)
		if !good {
			return false
		}
	}
	return true
}

func (p *PhotonParser) handleCommand(src []byte, offset int) (int, bool) {
	if !available(src, offset, commandHeaderLength) {
		return offset, false
	}

	cmdType := src[offset]
	offset += 4 // type + channelId + commandFlags + reserved
	cmdLen := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	offset += 4 // reliableSequenceNumber
	cmdLen -= commandHeaderLength

	if cmdLen < 0 || !available(src, offset, cmdLen) {
		return offset, false
	}

	switch cmdType {
	case cmdDisconnect:
		return offset + cmdLen, true
	case cmdSendUnreliable:
		if cmdLen < 4 {
			return offset + cmdLen, false
		}
		offset += 4
		cmdLen -= 4
		newOffset, _ := p.handleSendReliable(src, offset, cmdLen)
		return newOffset, true
	case cmdSendReliable:
		newOffset, _ := p.handleSendReliable(src, offset, cmdLen)
		return newOffset, true
	case cmdSendFragment:
		return p.handleSendFragment(src, offset, cmdLen), true
	default:
		return offset + cmdLen, true
	}
}

func (p *PhotonParser) handleSendReliable(src []byte, offset, cmdLen int) (int, bool) {
	if cmdLen < 2 || !available(src, offset, cmdLen) {
		return offset + cmdLen, false
	}

	offset++ // signalByte
	msgType := src[offset]
	offset++
	cmdLen -= 2

	if !available(src, offset, cmdLen) {
		return offset + cmdLen, false
	}

	if msgType == msgEncrypted {
		if p.OnEncrypted != nil {
			p.OnEncrypted()
		}
		return offset + cmdLen, true
	}

	data := src[offset : offset+cmdLen]
	offset += cmdLen

	switch msgType {
	case MsgRequest:
		p.dispatchRequest(data)
	case MsgResponse, msgResponseAlt:
		p.dispatchResponse(data)
	case MsgEvent:
		p.dispatchEvent(data)
	}
	return offset, true
}

func (p *PhotonParser) dispatchRequest(data []byte) {
	if len(data) < 1 {
		return
	}
	opCode := data[0]
	params := deserializeParameterTable(data[1:])
	if p.OnRequest != nil {
		p.OnRequest(opCode, params)
	}
}

func (p *PhotonParser) dispatchResponse(data []byte) {
	if len(data) < 3 {
		return
	}
	opCode := data[0]
	returnCode := int16(binary.LittleEndian.Uint16(data[1:3]))

	buf := bytes.NewBuffer(data[3:])
	debugMsg := ""
	budget := maxNodes
	if buf.Len() > 0 {
		tc, _ := buf.ReadByte()
		val := deserialize(buf, tc, &budget)
		switch v := val.(type) {
		case string:
			debugMsg = v
		case []string:
			// Albion embeds market-order data as a string array where the debug
			// message would be; surface it as params[0].
			params := map[byte]interface{}{0: v}
			if p.OnResponse != nil {
				p.OnResponse(opCode, returnCode, "", params)
			}
			return
		}
	}
	params := readParameterTable(buf, &budget)
	if p.OnResponse != nil {
		p.OnResponse(opCode, returnCode, debugMsg, params)
	}
}

func (p *PhotonParser) dispatchEvent(data []byte) {
	if len(data) < 1 {
		return
	}
	code := data[0]
	params := deserializeParameterTable(data[1:])
	if p.OnEvent != nil {
		p.OnEvent(code, params)
	}
}

func (p *PhotonParser) handleSendFragment(src []byte, offset, cmdLen int) int {
	if cmdLen < fragmentHeaderLength || !available(src, offset, fragmentHeaderLength) {
		return offset + cmdLen
	}

	startSeq := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4
	fragmentCount := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4
	fragmentNumber := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4
	totalLen := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4
	fragOffset := int(binary.BigEndian.Uint32(src[offset:]))
	offset += 4
	cmdLen -= 4

	fragLen := cmdLen
	if fragLen < 0 || !available(src, offset, fragLen) {
		return offset + max(fragLen, 0)
	}
	// Reject absurd headers so a corrupt one can't request a huge alloc or loop.
	if totalLen < 0 || totalLen > 1<<20 || fragOffset < 0 || fragOffset > totalLen ||
		fragmentCount <= 0 || fragmentCount > 4096 || fragmentNumber < 0 || fragmentNumber >= fragmentCount {
		return offset + fragLen
	}

	seg, ok := p.pendingSegments[startSeq]
	if !ok {
		p.evictIfFull()
		seg = &segmentedPackage{totalLength: totalLen, fragmentCount: fragmentCount, received: make(map[int]bool, fragmentCount), payload: make([]byte, totalLen)}
		p.pendingSegments[startSeq] = seg
		p.segOrder = append(p.segOrder, startSeq)
	}

	// Copy each fragment number ONCE — the Photon reliable layer retransmits, and
	// counting duplicate bytes would "complete" a message while leaving holes (the large
	// GetMailInfos response never reassembled, live-hit 2026-07-08). Completion is by
	// distinct fragment count, not byte total.
	end := fragOffset + fragLen
	if !seg.received[fragmentNumber] && end <= len(seg.payload) {
		copy(seg.payload[fragOffset:end], src[offset:offset+fragLen])
		seg.received[fragmentNumber] = true
	}
	offset += fragLen

	if len(seg.received) >= seg.fragmentCount {
		p.removeSegment(startSeq)
		p.handleSendReliable(seg.payload, 0, len(seg.payload))
	}
	return offset
}

// evictIfFull drops the oldest pending fragment when the buffer is at capacity.
func (p *PhotonParser) evictIfFull() {
	for len(p.pendingSegments) >= maxPendingSegments && len(p.segOrder) > 0 {
		oldest := p.segOrder[0]
		p.segOrder = p.segOrder[1:]
		delete(p.pendingSegments, oldest)
	}
}

func (p *PhotonParser) removeSegment(startSeq int) {
	delete(p.pendingSegments, startSeq)
	for i, s := range p.segOrder {
		if s == startSeq {
			p.segOrder = append(p.segOrder[:i], p.segOrder[i+1:]...)
			break
		}
	}
}

// PendingSegments reports the current fragment-buffer size (for tests/metrics).
func (p *PhotonParser) PendingSegments() int { return len(p.pendingSegments) }

// available reports whether src[offset:offset+count] is in bounds.
func available(src []byte, offset, count int) bool {
	return count >= 0 && offset >= 0 && len(src)-offset >= count
}
