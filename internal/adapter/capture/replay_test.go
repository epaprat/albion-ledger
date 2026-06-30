package capture

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/internal/photon"
)

// TestReplayDecodes feeds the synthetic fixture through replay → photon and
// asserts decoded messages arrive and encrypted packets are flagged, not
// decoded (US1 / FR-008). Runs fully offline (pure-Go pcap).
func TestReplayDecodes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "synthetic.pcap")
	if err := WriteSyntheticFixture(path); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	src := NewReplay(path)
	ch, err := src.Packets(context.Background())
	if err != nil {
		t.Fatalf("packets: %v", err)
	}

	var events, responses, encrypted int
	parser := photon.NewPhotonParser(
		nil,
		func(byte, int16, string, map[byte]interface{}) { responses++ },
		func(byte, map[byte]interface{}) { events++ },
	)
	parser.OnEncrypted = func() { encrypted++ }

	for payload := range ch {
		parser.ReceivePacket(payload)
	}

	if events == 0 {
		t.Fatal("expected decoded events, got 0")
	}
	if responses == 0 {
		t.Fatal("expected decoded responses (market), got 0")
	}
	if encrypted == 0 {
		t.Fatal("expected at least one encrypted packet flagged")
	}
	t.Logf("decoded events=%d responses=%d encrypted=%d", events, responses, encrypted)
}
