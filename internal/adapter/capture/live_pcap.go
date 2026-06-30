//go:build pcap

package capture

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/port"
)

const captureBuffer = 4096 // bounded channel; drops counted, never grows (Principle XI)

// Live is a gopacket/libpcap PacketSource (CGO; built with -tags pcap).
type Live struct {
	iface   string
	decoded atomic.Uint64
	dropped atomic.Uint64
	server  atomic.Value // string
}

// NewLive opens a live capture on iface (empty = auto-detect the primary LAN
// interface). Auto-detect picks the first device carrying a real (non-loopback,
// non-link-local) IPv4 address — i.e. the Wi-Fi/Ethernet the game traffic flows on,
// not a VPN tunnel (utun), AirDrop (awdl), or bridge with only link-local addresses.
func NewLive(iface string) (port.PacketSource, error) {
	if iface == "" {
		devs, err := pcap.FindAllDevs()
		if err != nil {
			return nil, err
		}
		for _, d := range devs {
			if isVirtualIface(d.Name) { // skip VPN tunnels, AirDrop, bridges by name
				continue
			}
			for _, a := range d.Addresses {
				ip := a.IP
				if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
					continue
				}
				if ip.To4() != nil { // a real IPv4-bearing device
					iface = d.Name
					break
				}
			}
			if iface != "" {
				break
			}
		}
	}
	l := &Live{iface: iface}
	l.server.Store("")
	return l, nil
}

// isVirtualIface reports whether a device name is a VPN tunnel, AirDrop link, or
// bridge that never carries the game's LAN traffic — skipped during auto-detect.
func isVirtualIface(name string) bool {
	for _, p := range []string{"utun", "awdl", "llw", "bridge", "ap", "tun", "tap", "vnic", "vmnet", "ppp"} {
		if len(name) >= len(p) && name[:len(p)] == p {
			return true
		}
	}
	return false
}

// Packets opens the handle, applies the Albion BPF filter, and streams payloads.
func (l *Live) Packets(ctx context.Context) (<-chan []byte, error) {
	handle, err := pcap.OpenLive(l.iface, 65536, true, pcap.BlockForever)
	if err != nil {
		return nil, err
	}
	if err := handle.SetBPFFilter("udp port 5056"); err != nil {
		handle.Close()
		return nil, err
	}

	out := make(chan []byte, captureBuffer)
	go func() {
		defer handle.Close()
		defer close(out)
		var rawSeen, extracted uint64
		defer func() {
			fmt.Fprintf(os.Stderr, "[capture] iface=%s rawPackets=%d extractedPayloads=%d dropped=%d\n",
				l.iface, rawSeen, extracted, l.dropped.Load())
		}()
		src := gopacket.NewPacketSource(handle, handle.LinkType())
		for {
			select {
			case <-ctx.Done():
				return
			case pkt, ok := <-src.Packets():
				if !ok {
					return
				}
				rawSeen++
				payload, server := l.extract(pkt)
				if payload == nil {
					continue
				}
				extracted++
				if server != "" {
					l.server.Store(server)
				}
				l.decoded.Add(1)
				select {
				case out <- payload:
				default:
					l.dropped.Add(1) // consumer behind: drop, never grow memory
				}
			}
		}
	}()
	return out, nil
}

func (l *Live) extract(pkt gopacket.Packet) ([]byte, string) {
	udpLayer := pkt.Layer(layers.LayerTypeUDP)
	if udpLayer == nil {
		return nil, ""
	}
	udp, _ := udpLayer.(*layers.UDP)
	if udp == nil || len(udp.Payload) == 0 {
		return nil, ""
	}
	server := ""
	if ipLayer := pkt.Layer(layers.LayerTypeIPv4); ipLayer != nil {
		if ip, ok := ipLayer.(*layers.IPv4); ok && uint16(udp.SrcPort) == albionPort {
			server = ip.SrcIP.String()
		}
	}
	return udp.Payload, server
}

// Status reports the live capture state.
func (l *Live) Status() model.CaptureStatus {
	srv, _ := l.server.Load().(string)
	return model.CaptureStatus{
		Capturing: true, Interface: l.iface, GameServer: srv,
		Decoded: l.decoded.Load(),
	}
}
