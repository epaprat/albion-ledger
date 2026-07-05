// Package aodp fetches community market prices from the Albion Online Data
// Project's public API (feature 010). It is the BASE valuation layer only: any
// in-game observation (live market browse, declared EMV, K-overview values)
// overrides it, and a fetch failure degrades silently — the app never depends on
// the network. Read-only public HTTP; passive capture stays untouched (ToS-safe).
package aodp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/epaprat/albion-ledger/internal/port"
)

// Client fetches prices in batches. Zero value is not usable; use New.
type Client struct {
	base   string
	cities string
	hc     *http.Client
}

// New builds a client for the public API (base override for tests).
func New(base string) *Client {
	if base == "" {
		base = "https://www.albion-online-data.com"
	}
	return &Client{
		base:   base,
		cities: "Thetford,FortSterling,Lymhurst,Bridgewatch,Martlock,Caerleon",
		hc:     &http.Client{Timeout: 10 * time.Second},
	}
}

// batchSize keeps request URLs comfortably under length limits.
const batchSize = 100

type wireRow struct {
	ItemID       string `json:"item_id"`
	Quality      int    `json:"quality"`
	SellPriceMin int64  `json:"sell_price_min"`
}

// Fetch returns the cheapest sell price across royal cities for each name+quality.
// Names are catalog uniqueNames ("T4_RUNE", "T5_WOOD@1"). Failures return an error;
// partial batches already fetched are still returned.
func (c *Client) Fetch(ctx context.Context, names []string) ([]port.ExternalPrice, error) {
	var out []port.ExternalPrice
	for start := 0; start < len(names); start += batchSize {
		end := start + batchSize
		if end > len(names) {
			end = len(names)
		}
		url := fmt.Sprintf("%s/api/v2/stats/prices/%s.json?locations=%s",
			c.base, strings.Join(names[start:end], ","), c.cities)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return out, err
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			return out, err
		}
		var rows []wireRow
		err = json.NewDecoder(resp.Body).Decode(&rows)
		resp.Body.Close()
		if err != nil {
			return out, err
		}
		// Average of the positive per-city minimum sell prices: a single city's
		// rock-bottom listing under-values everything (live feedback: our totals sat
		// far below the game's own estimate), and a lone outlier can't skew a mean
		// of city minimums much.
		type acc struct {
			sum int64
			n   int64
			p   port.ExternalPrice
		}
		agg := map[string]*acc{}
		for _, r := range rows {
			if r.SellPriceMin <= 0 {
				continue
			}
			k := fmt.Sprintf("%s|%d", r.ItemID, r.Quality)
			a := agg[k]
			if a == nil {
				a = &acc{p: port.ExternalPrice{UniqueName: r.ItemID, Quality: r.Quality}}
				agg[k] = a
			}
			a.sum += r.SellPriceMin
			a.n++
		}
		for _, a := range agg {
			a.p.Silver = a.sum / a.n
			out = append(out, a.p)
		}
	}
	return out, nil
}
