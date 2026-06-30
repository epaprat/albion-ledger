//go:build !pcap

package capture

import (
	"errors"

	"github.com/epaprat/albion-ledger/internal/port"
)

// NewLive returns an error in the default build, which excludes libpcap (CGO).
// Build with `-tags pcap` to enable live capture (Constitution VI: capture is
// behind a build-tagged adapter; default build stays pure-Go and CI-friendly).
func NewLive(_ string) (port.PacketSource, error) {
	return nil, errors.New("live capture not compiled in; rebuild with: go build -tags pcap ./cmd/probe")
}
