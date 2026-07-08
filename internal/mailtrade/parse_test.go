package mailtrade

import "testing"

func TestParseBody_Golden(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		body string
		want Parsed
	}{
		{
			// Reference: "1|T5_2H_SHAPESHIFTER_SET3@1|1549840000|1549840000"
			name: "sell finished",
			typ:  "MARKETPLACE_SELLORDER_FINISHED_SUMMARY",
			body: "1|T5_2H_SHAPESHIFTER_SET3@1|1549840000|1549840000",
			want: Parsed{Direction: Sold, ItemID: "T5_2H_SHAPESHIFTER_SET3@1", PartialAmount: 1, TotalAmount: 1, TotalSilver: 154_984, UnitSilver: 154_984},
		},
		{
			// "10|T7_ALCHEMY_RARE_ENT|11000100000|1100010000" — unit derived total/amount.
			name: "buy finished",
			typ:  "MARKETPLACE_BUYORDER_FINISHED_SUMMARY",
			body: "10|T7_ALCHEMY_RARE_ENT|11000100000|1100010000",
			want: Parsed{Direction: Bought, ItemID: "T7_ALCHEMY_RARE_ENT", PartialAmount: 10, TotalAmount: 10, TotalSilver: 1_100_010, UnitSilver: 110_001},
		},
		{
			// "0|39|0|T7_JOURNAL_HUNTER_FULL|" — nothing sold before expiry (item idx3).
			name: "sell expired zero",
			typ:  "MARKETPLACE_SELLORDER_EXPIRED_SUMMARY",
			body: "0|39|0|T7_JOURNAL_HUNTER_FULL|",
			want: Parsed{Direction: Sold, ItemID: "T7_JOURNAL_HUNTER_FULL", PartialAmount: 0, TotalAmount: 39, TotalSilver: 0, UnitSilver: 0},
		},
		{
			// "23|100|65450000000|T5_ALCHEMY_RARE_PANTHER|" — refund/remaining infers unit.
			// refund=6_545_000; remaining=77; unit=85_000; total=unit*bought=1_955_000.
			name: "buy expired refund-derived",
			typ:  "MARKETPLACE_BUYORDER_EXPIRED_SUMMARY",
			body: "23|100|65450000000|T5_ALCHEMY_RARE_PANTHER|",
			want: Parsed{Direction: Bought, ItemID: "T5_ALCHEMY_RARE_PANTHER", PartialAmount: 23, TotalAmount: 100, TotalSilver: 1_955_000, UnitSilver: 85_000},
		},
		{
			// "6|53|4420680000|T6_OFF_HORN_KEEPER@1|" — blackmarket = sold (item idx3).
			name: "blackmarket sell expired",
			typ:  "BLACKMARKET_SELLORDER_EXPIRED_SUMMARY",
			body: "6|53|4420680000|T6_OFF_HORN_KEEPER@1|",
			want: Parsed{Direction: Sold, ItemID: "T6_OFF_HORN_KEEPER@1", PartialAmount: 6, TotalAmount: 53, TotalSilver: 442_068, UnitSilver: 73_678},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseBody(TypeFromString(c.typ), c.body)
			if !ok {
				t.Fatalf("ParseBody ok=false for %s", c.body)
			}
			if got != c.want {
				t.Fatalf("ParseBody mismatch\n got=%+v\nwant=%+v", got, c.want)
			}
		})
	}
}

func TestParseBody_Rejects(t *testing.T) {
	// Unknown type → ignored.
	if _, ok := ParseBody(TypeFromString("GUILD_MESSAGE"), "whatever"); ok {
		t.Fatal("unknown mail type must be rejected")
	}
	// Too few fields.
	if _, ok := ParseBody(SellOrderFinished, "1|item|100"); ok {
		t.Fatal("short body must be rejected")
	}
	// Non-numeric amount.
	if _, ok := ParseBody(SellOrderFinished, "x|item|100|100"); ok {
		t.Fatal("non-numeric amount must be rejected")
	}
	// Non-numeric silver.
	if _, ok := ParseBody(BuyOrderFinished, "10|item|nope|100"); ok {
		t.Fatal("non-numeric silver must be rejected")
	}
	// A fully-filled expired buy (remaining 0) can't infer a price → dropped, not recorded
	// as a free purchase.
	if _, ok := ParseBody(BuyOrderExpired, "50|50|0|T5_ITEM|"); ok {
		t.Fatal("fully-filled expired buy must be dropped, not recorded free")
	}
}

func TestTypeFromString(t *testing.T) {
	if TypeFromString("MARKETPLACE_SELLORDER_FINISHED_SUMMARY") != SellOrderFinished {
		t.Fatal("sell finished mapping")
	}
	if TypeFromString("nonsense") != Unknown {
		t.Fatal("unknown mapping")
	}
}
