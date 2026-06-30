package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `{"items":[
  {"index":1,"uniqueName":"T4_BAG","name":"Adept's Bag"},
  {"index":3,"uniqueName":"T6_2H_AXE@2","name":"Expert's Battleaxe"}
]}`

func TestResolveKnown(t *testing.T) {
	c, err := New([]byte(sample))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	it := c.Resolve(1, 2)
	if !it.Known || it.DisplayName != "Adept's Bag" || it.Tier != 4 || it.Enchant != 0 || it.Quality != 2 {
		t.Fatalf("resolve(1,2) = %+v", it)
	}
	it = c.Resolve(3, 1)
	if it.Tier != 6 || it.Enchant != 2 {
		t.Fatalf("tier/enchant parse failed: %+v", it)
	}
}

func TestResolveUnknown(t *testing.T) {
	c, _ := New([]byte(sample))
	it := c.Resolve(999, 0)
	if it.Known {
		t.Fatal("unknown index must be Known=false")
	}
	if it.DisplayName != "Unknown item #999" {
		t.Fatalf("unknown display = %q", it.DisplayName)
	}
}

func TestReloadSwapAndReject(t *testing.T) {
	c, _ := New([]byte(sample))
	dir := t.TempDir()
	good := filepath.Join(dir, "good.json")
	os.WriteFile(good, []byte(`{"items":[{"index":7,"uniqueName":"T2_BAG","name":"Novice's Bag"}]}`), 0o644)
	if err := c.Reload(good); err != nil {
		t.Fatalf("reload good: %v", err)
	}
	if !c.Resolve(7, 0).Known {
		t.Fatal("reloaded catalog missing index 7")
	}

	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`{not json`), 0o644)
	if err := c.Reload(bad); err == nil {
		t.Fatal("malformed reload must error")
	}
	// Previous catalog kept after a rejected reload.
	if !c.Resolve(7, 0).Known {
		t.Fatal("rejected reload must keep previous catalog")
	}
}
