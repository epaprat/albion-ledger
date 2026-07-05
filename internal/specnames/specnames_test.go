package specnames

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/data"
)

func TestResolveBundled(t *testing.T) {
	c, err := New(data.SpecNodesJSON)
	if err != nil {
		t.Fatal(err)
	}
	if name, cat, ok := c.Resolve(22); !ok || name == "" || cat == "" {
		t.Fatalf("bundled node 22 unresolved: %q/%q/%v", name, cat, ok)
	}
	if _, _, ok := c.Resolve(999999); ok {
		t.Fatal("unknown id must be not-ok (renders Node #N)")
	}
}

func TestReloadRejectsMalformed(t *testing.T) {
	c, err := New([]byte(`{"nodes":[{"id":1,"name":"Alpha","category":"x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{ not json"), 0o644)
	if err := c.Reload(bad); err == nil {
		t.Fatal("malformed reload must error")
	}
	if name, _, ok := c.Resolve(1); !ok || name != "Alpha" {
		t.Fatal("previous catalog must survive a bad reload")
	}
	good := filepath.Join(dir, "good.json")
	os.WriteFile(good, []byte(`{"nodes":[{"id":2,"name":"Beta","category":"y"}]}`), 0o644)
	if err := c.Reload(good); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Resolve(1); ok {
		t.Fatal("reload must REPLACE")
	}
	if name, _, ok := c.Resolve(2); !ok || name != "Beta" {
		t.Fatal("reload must load the new file")
	}
}
