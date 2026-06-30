package capture

import (
	"os"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"

	"github.com/epaprat/albion-ledger/internal/photon"
)

const albionPort = 5056

// EventCodeParam / OpCodeParam are the Photon parameter keys that carry the
// semantic Albion event / operation code (codes exceed a byte, so the game
// routes them here rather than in the Photon message code byte).
const (
	EventCodeParam byte = 252
	OpCodeParam    byte = 253
)

// buildEvent frames a Photon EventData whose Albion event code lives in param 252.
func buildEvent(albionCode int, fields []photon.Field) []byte {
	fields = append(fields, photon.Field{Key: EventCodeParam, Type: photon.TypeShort, Val: int16(albionCode)})
	return photon.BuildEventPacket(0, fields)
}

// SyntheticPayloads builds a deterministic set of Photon payloads covering each
// handled category, plus a position event (must be unhandled), an unknown code,
// and an encrypted packet. Used to generate the committed CI fixture.
func SyntheticPayloads() [][]byte {
	S := photon.TypeString
	I := photon.TypeInteger
	B := photon.TypeByte
	arrS := photon.TypeArray | photon.TypeString
	f := func(k, ty byte, v interface{}) photon.Field { return photon.Field{Key: k, Type: ty, Val: v} }

	return [][]byte{
		// Market/gold responses route by the operation byte code (all < 256).
		photon.BuildResponsePacket(82, 0, []photon.Field{f(0, arrS, []string{`{"order":1}`})}),  // AuctionGetOffers
		photon.BuildResponsePacket(83, 0, []photon.Field{f(0, arrS, []string{`{"order":2}`})}),  // AuctionGetRequests
		photon.BuildResponsePacket(96, 0, []photon.Field{f(0, I, int32(7)), f(1, S, "hist")}),   // AuctionGetItemAverageStats
		photon.BuildResponsePacket(251, 0, []photon.Field{f(0, I, int32(9)), f(1, I, int32(1))}), // GoldMarketGetAverageInfo
		// Events route by param 252 (codes exceed a byte).
		buildEvent(30, []photon.Field{f(0, I, int32(1)), f(1, I, int32(2))}),                       // NewEquipmentItem
		buildEvent(61, []photon.Field{f(0, I, int32(1)), f(1, I, int32(2))}),                       // HarvestFinished
		buildEvent(62, []photon.Field{f(0, I, int32(1)), f(3, I, int32(5)), f(5, I, int32(1))}),    // TakeSilver
		buildEvent(82, []photon.Field{f(1, I, int32(100)), f(2, I, int32(1)), f(3, I, int32(2))}),  // UpdateFame
		buildEvent(98, []photon.Field{f(0, I, int32(1)), f(3, S, "Mob")}),                          // NewLoot
		buildEvent(99, []photon.Field{f(0, I, int32(1)), f(2, I, int32(3))}),                       // AttachItemContainer
		buildEvent(143, []photon.Field{f(0, B, byte(1))}),                                          // CharacterStats
		buildEvent(414, []photon.Field{f(0, I, int32(1)), f(1, S, "vault"), f(5, I, int32(7))}),    // BankVaultInfo
		buildEvent(466, []photon.Field{f(0, I, int32(1)), f(1, I, int32(999))}),                    // EstimatedMarketValueUpdate
		buildEvent(3, []photon.Field{f(0, I, int32(1)), f(1, I, int32(2))}),                        // Move (position → unhandled)
		buildEvent(9000, []photon.Field{f(0, B, byte(1))}),                                         // unknown code
		photon.BuildEncryptedPacket(),                                                              // encrypted
	}
}

// WriteSyntheticFixture writes the synthetic payloads as a legacy pcap, each
// wrapped in Ethernet/IPv4/UDP(5056) so the replay path matches live capture.
func WriteSyntheticFixture(path string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	w := pcapgo.NewWriter(out)
	if err := w.WriteFileHeader(65536, layers.LinkTypeEthernet); err != nil {
		return err
	}

	for _, payload := range SyntheticPayloads() {
		eth := &layers.Ethernet{
			SrcMAC:       []byte{0, 0, 0, 0, 0, 1},
			DstMAC:       []byte{0, 0, 0, 0, 0, 2},
			EthernetType: layers.EthernetTypeIPv4,
		}
		ip := &layers.IPv4{
			Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolUDP,
			SrcIP: []byte{5, 188, 125, 1}, DstIP: []byte{192, 168, 1, 2},
		}
		udp := &layers.UDP{SrcPort: albionPort, DstPort: 51000}
		_ = udp.SetNetworkLayerForChecksum(ip)

		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
		if err := gopacket.SerializeLayers(buf, opts, eth, ip, udp, gopacket.Payload(payload)); err != nil {
			return err
		}
		data := buf.Bytes()
		ci := gopacket.CaptureInfo{CaptureLength: len(data), Length: len(data)}
		if err := w.WritePacket(ci, data); err != nil {
			return err
		}
	}
	return nil
}
