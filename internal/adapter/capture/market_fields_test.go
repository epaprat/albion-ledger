package capture

import (
	"fmt"
	"testing"
)

// 010 review: MarketOrders unit coverage — malformed rows skip, enchant suffix,
// quality clamp, cap truncation, empty rejection.
func TestMarketOrders(t *testing.T) {
	rows := []string{
		`{"UnitPriceSilver":70000,"ItemTypeId":"T4_RUNE","EnchantmentLevel":0,"QualityLevel":1,"AuctionType":"offer"}`,
		`{"UnitPriceSilver":80000,"ItemTypeId":"T5_WOOD","EnchantmentLevel":2,"QualityLevel":9,"AuctionType":"request"}`,
		`{"broken json`,                       // malformed — skipped
		`{"UnitPriceSilver":0,"ItemTypeId":"T4_ORE"}`,  // non-positive — skipped
		`{"UnitPriceSilver":5,"ItemTypeId":""}`,        // no id — skipped
	}
	got, ok := MarketOrders(map[byte]interface{}{0: rows})
	if !ok || len(got) != 2 {
		t.Fatalf("orders = %d/%v, want 2/true", len(got), ok)
	}
	if !got[0].IsOffer || got[0].UniqueName != "T4_RUNE" || got[0].UnitRaw != 70000 {
		t.Fatalf("offer row wrong: %+v", got[0])
	}
	if got[1].IsOffer || got[1].UniqueName != "T5_WOOD@2" || got[1].Quality != 0 {
		t.Fatalf("enchant suffix / quality clamp wrong: %+v", got[1])
	}

	// Cap: 200 rows truncate to maxMarketOrders.
	big := make([]string, 200)
	for i := range big {
		big[i] = fmt.Sprintf(`{"UnitPriceSilver":10,"ItemTypeId":"T%d_X","AuctionType":"offer"}`, i)
	}
	got, ok = MarketOrders(map[byte]interface{}{0: big})
	if !ok || len(got) > maxMarketOrders {
		t.Fatalf("cap breached: %d", len(got))
	}

	if _, ok := MarketOrders(map[byte]interface{}{0: []string{}}); ok {
		t.Fatal("empty list must be not-ok")
	}
}
