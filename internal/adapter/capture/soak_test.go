package capture

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/internal/photon"
)

// TestSoakBounded replays the fixture many times and asserts the parser's
// fragment buffer never accumulates and nothing panics — a fast proxy for the
// 3h soak (Constitution Principle XI). The full 3h run is a manual release step.
func TestSoakBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak in -short")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "s.pcap")
	if err := WriteSyntheticFixture(path); err != nil {
		t.Fatal(err)
	}

	parser := photon.NewPhotonParser(
		func(byte, map[byte]interface{}) {},
		func(byte, int16, string, map[byte]interface{}) {},
		func(byte, map[byte]interface{}) {},
	)
	parser.OnEncrypted = func() {}

	const iterations = 3000
	for i := 0; i < iterations; i++ {
		ch, err := NewReplay(path).Packets(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for payload := range ch {
			parser.ReceivePacket(payload)
		}
		if n := parser.PendingSegments(); n > 64 {
			t.Fatalf("fragment buffer grew to %d (> cap 64) at iter %d", n, i)
		}
	}
}
