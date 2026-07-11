package app

// 021: end-to-end proof that -reconcile flags a holdings divergence through the REAL op-2
// path — the self-check that replaces eyeballing the game against the app (works identically
// live and under -replay, since it runs in the handlers, not the capture source).

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/epaprat/albion-ledger/internal/holdings"
)

func TestReconcileFlagsInventoryLeak(t *testing.T) {
	svc, p := newGlue(t)
	p.SetReconcile(true)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// A foreign container leaks into inventory (unknown owner → LocInventory "Bag"), the
	// exact shape of the viewed-loot-bag bug.
	svc.IngestContainer("foreignguid00000000000000000000", "notabank",
		[]holdings.SlotItem{{ObjID: 900, Ref: holdings.ItemRef{Index: 837, Count: 2}}})

	// The authoritative op-2 bag carries only item 920 — so the app now shows one item the
	// wire doesn't, and reconciling the op-2 snapshot must flag it.
	p.registerNewItem(32, declParams(910, 920, 1))
	p.ingestSelf(SelfBagGUID, "Bag", []int{910})

	out := buf.String()
	if !strings.Contains(out, "[RECON]") || !strings.Contains(out, "EXTRA") {
		t.Fatalf("reconcile must flag the leaked container: log=%q", out)
	}

	// Control: a clean bag (op-2 matches the app) emits no [RECON] line.
	svc2, p2 := newGlue(t)
	p2.SetReconcile(true)
	buf.Reset()
	p2.registerNewItem(32, declParams(911, 920, 1))
	p2.ingestSelf(SelfBagGUID, "Bag", []int{911})
	_ = svc2
	if strings.Contains(buf.String(), "[RECON]") {
		t.Fatalf("a matching bag must stay silent, got %q", buf.String())
	}
}
