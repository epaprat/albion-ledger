package capture

import (
	"context"
	"os"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// Replay is a deterministic PacketSource that reads UDP-5056 payloads from a
// recorded pcap file. Pure-Go (pcapgo) — no libpcap, runs offline in CI.
type Replay struct {
	path   string
	status model.CaptureStatus
}

// NewReplay creates a replay source over the given pcap file.
func NewReplay(path string) *Replay {
	return &Replay{path: path, status: model.CaptureStatus{Interface: path}}
}

// Packets reads the file and emits each Albion UDP payload in order, then closes.
func (r *Replay) Packets(ctx context.Context) (<-chan []byte, error) {
	f, err := os.Open(r.path)
	if err != nil {
		return nil, err
	}
	reader, err := pcapgo.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	out := make(chan []byte)
	go func() {
		defer f.Close()
		defer close(out)
		for {
			data, _, err := reader.ReadPacketData()
			if err != nil {
				return
			}
			payload, ok := extractAlbionPayload(data)
			if !ok {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case out <- payload:
			}
		}
	}()
	return out, nil
}

// Status returns the replay source status.
func (r *Replay) Status() model.CaptureStatus { return r.status }

// extractAlbionPayload pulls the UDP payload from an Ethernet frame when either
// endpoint is the Albion Photon port. Uses a fresh parse per packet for safety.
func extractAlbionPayload(data []byte) ([]byte, bool) {
	pkt := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
	udpLayer := pkt.Layer(layers.LayerTypeUDP)
	if udpLayer == nil {
		return nil, false
	}
	udp, _ := udpLayer.(*layers.UDP)
	if udp == nil || (uint16(udp.SrcPort) != albionPort && uint16(udp.DstPort) != albionPort) {
		return nil, false
	}
	if len(udp.Payload) == 0 {
		return nil, false
	}
	return udp.Payload, true
}
