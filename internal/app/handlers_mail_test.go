package app

// Mail P&L handler tests (017): drive the REAL dispatch path (classifier → extractors →
// mailtrade parser → service) with wire-shaped R:174/R:176 params, asserting the
// contract in specs/017-mail-capture/contracts/mail-capture.md §7.

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func mailInfosParams(id int64, typ string) map[byte]interface{} {
	return map[byte]interface{}{
		3:  []int64{id},
		7:  []string{"3005"},
		11: []string{typ},
		12: []int64{1_549_840_000},
	}
}

func readMailParams(id int64, body string) map[byte]interface{} {
	return map[byte]interface{}{0: id, 1: body}
}

func TestMailFlow_SellIncome(t *testing.T) {
	svc, p := newGlue(t)
	// GetMailInfos first (populates id→type), then the opened mail body.
	p.dispatch(probe.KindResponse, 174, mailInfosParams(1001, "MARKETPLACE_SELLORDER_FINISHED_SUMMARY"))
	p.dispatch(probe.KindResponse, 176, readMailParams(1001, "1|T5_2H_SHAPESHIFTER_SET3@1|1549840000|1549840000"))

	trades := svc.Trades()
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	tr := trades[0]
	// gross 154984 (mail total); tax 4% = 6199; net = 148785 (rate-estimated).
	if tr.Direction != "sold" || tr.ItemID != "T5_2H_SHAPESHIFTER_SET3@1" || tr.Gross != 154_984 {
		t.Fatalf("trade wrong: %+v", tr)
	}
	if tr.SalesTax != 6_199 || tr.Net != 148_785 || !tr.TaxEstimated || tr.Source != "mail" {
		t.Fatalf("breakdown wrong: %+v", tr)
	}
	if sum := svc.TradeSummary("all"); sum.GrossIncome != 154_984 || sum.SalesTax != 6_199 || sum.Net != 148_785 || sum.Count != 1 {
		t.Fatalf("summary wrong: %+v", sum)
	}
}

func TestMailFlow_BulkListArrivesAsOp1(t *testing.T) {
	svc, p := newGlue(t)
	// A large mailbox's GetMailInfos sync arrives as an op-1 response (shared with bank
	// tab content), NOT R:174 — live-decoded 2026-07-08. The MARKETPLACE type signature
	// must route it to mail caching so the subsequent reads decode.
	op1Mail := map[byte]interface{}{
		2:  uint8(0),
		3:  []int32{1001},
		7:  []string{"4301"},
		11: []string{"MARKETPLACE_SELLORDER_FINISHED_SUMMARY"},
		12: []int64{1_549_840_000},
	}
	p.dispatch(probe.KindResponse, 1, op1Mail) // classified as bank_tab_content, delegated to mail
	p.dispatch(probe.KindResponse, 176, readMailParams(1001, "1|T5_2H_SHAPESHIFTER_SET3@1|1549840000|1549840000"))

	if trades := svc.Trades(); len(trades) != 1 || trades[0].Direction != "sold" {
		t.Fatalf("op-1 bulk mail list must feed a trade, got %+v", trades)
	}
}

func TestMailFlow_ReadWithoutInfosDropped(t *testing.T) {
	svc, p := newGlue(t)
	// ReadMail arrives with no prior GetMailInfos → type unknown → dropped (passive limit).
	p.dispatch(probe.KindResponse, 176, readMailParams(1001, "1|T5_2H_SHAPESHIFTER_SET3@1|1549840000|1549840000"))
	if trades := svc.Trades(); len(trades) != 0 {
		t.Fatalf("expected 0 trades (infos not seen), got %d", len(trades))
	}
}

func TestMailFlow_DedupSameMail(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindResponse, 174, mailInfosParams(1001, "MARKETPLACE_SELLORDER_FINISHED_SUMMARY"))
	body := "1|T5_2H_SHAPESHIFTER_SET3@1|1549840000|1549840000"
	p.dispatch(probe.KindResponse, 176, readMailParams(1001, body))
	p.dispatch(probe.KindResponse, 176, readMailParams(1001, body)) // opened again

	if trades := svc.Trades(); len(trades) != 1 {
		t.Fatalf("expected 1 trade after re-open (dedup), got %d", len(trades))
	}
	if sum := svc.TradeSummary("all"); sum.Count != 1 {
		t.Fatalf("count must stay 1, got %d", sum.Count)
	}
}

func TestMailFlow_BothDirectionsNet(t *testing.T) {
	svc, p := newGlue(t)
	p.dispatch(probe.KindResponse, 174, mailInfosParams(1001, "MARKETPLACE_SELLORDER_FINISHED_SUMMARY"))
	p.dispatch(probe.KindResponse, 176, readMailParams(1001, "1|T5_2H_SHAPESHIFTER_SET3@1|1549840000|1549840000")) // +154984 income
	p.dispatch(probe.KindResponse, 174, mailInfosParams(1002, "MARKETPLACE_BUYORDER_FINISHED_SUMMARY"))
	p.dispatch(probe.KindResponse, 176, readMailParams(1002, "10|T7_ALCHEMY_RARE_ENT|11000100000|1100010000")) // -1100010 expense

	// sell gross 154984 (net 148785 after 4% tax); buy gross 1100010 (net −1100010).
	sum := svc.TradeSummary("all")
	if sum.GrossIncome != 154_984 || sum.GrossExpense != 1_100_010 || sum.Net != 148_785-1_100_010 {
		t.Fatalf("two-direction summary wrong: %+v", sum)
	}
}
