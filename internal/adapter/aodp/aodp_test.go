package aodp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 010 review: batch splitting, per-city min averaging, positive filtering and
// non-200 handling — the client shipped untested.
func TestFetchAveragesAndBatches(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		fmt.Fprint(w, `[
			{"item_id":"T4_RUNE","quality":1,"sell_price_min":100},
			{"item_id":"T4_RUNE","quality":1,"sell_price_min":300},
			{"item_id":"T4_RUNE","quality":1,"sell_price_min":0},
			{"item_id":"T5_WOOD","quality":2,"sell_price_min":50}
		]`)
	}))
	defer srv.Close()

	c := New(srv.URL)
	names := make([]string, 150) // forces two batches of ≤100
	for i := range names {
		names[i] = fmt.Sprintf("T%d_X", i)
	}
	got, err := c.Fetch(context.Background(), names)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("batches = %d, want 2", len(paths))
	}
	if !strings.Contains(paths[0], "T0_X") || strings.Contains(paths[0], "T120_X") {
		t.Fatalf("batch split wrong: %s", paths[0][:80])
	}
	// Each batch returns the same body → 2× the two dedup keys.
	byKey := map[string]int64{}
	for _, p := range got {
		byKey[fmt.Sprintf("%s|%d", p.UniqueName, p.Quality)] = p.Silver
	}
	if byKey["T4_RUNE|1"] != 200 { // (100+300)/2, zero filtered out
		t.Fatalf("average wrong: %d, want 200", byKey["T4_RUNE|1"])
	}
	if byKey["T5_WOOD|2"] != 50 {
		t.Fatalf("single-city price wrong: %d", byKey["T5_WOOD|2"])
	}
}

func TestFetchNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, "<html>rate limited</html>")
	}))
	defer srv.Close()
	_, err := New(srv.URL).Fetch(context.Background(), []string{"T4_RUNE"})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("non-200 must surface a status-labeled error, got %v", err)
	}
}
