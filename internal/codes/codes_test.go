package codes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

const sample = `{"entries":[
  {"kind":"event","code":62,"category":"silver"},
  {"kind":"response","code":2,"category":"character_spec","guardKey":55}
]}`

func TestLookup(t *testing.T) {
	r, err := New([]byte(sample))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	cat, _, hasGuard, ok := r.Lookup("event", 62)
	if !ok || cat != model.CatSilver || hasGuard {
		t.Fatalf("event 62 → %v ok=%v guard=%v", cat, ok, hasGuard)
	}
	cat, gk, hasGuard, ok := r.Lookup("response", 2)
	if !ok || cat != model.CatCharacterSpec || !hasGuard || gk != 55 {
		t.Fatalf("response 2 → %v ok=%v guard=%v key=%d", cat, ok, hasGuard, gk)
	}
	// Unlisted code → unhandled (this is how positions stay excluded).
	if _, _, _, ok := r.Lookup("event", 3); ok {
		t.Fatal("unlisted code must be unhandled")
	}
}

func TestReloadAndReject(t *testing.T) {
	r, _ := New([]byte(sample))
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	os.WriteFile(good, []byte(`{"entries":[{"kind":"event","code":98,"category":"loot"}]}`), 0o644)
	if err := r.Reload(good); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, _, _, ok := r.Lookup("event", 98); !ok {
		t.Fatal("reloaded map missing loot")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{bad`), 0o644)
	if err := r.Reload(bad); err == nil {
		t.Fatal("malformed reload must error")
	}
	if _, _, _, ok := r.Lookup("event", 98); !ok {
		t.Fatal("rejected reload must keep previous map")
	}
}
