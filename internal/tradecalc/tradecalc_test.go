package tradecalc

import "testing"

func TestSoldFromGross(t *testing.T) {
	// 100000 gross at 4% → 4000 tax, 96000 net.
	b := SoldFromGross(100_000, 0.04)
	if b.Gross != 100_000 || b.SalesTax != 4_000 || b.Net != 96_000 || !b.Estimated {
		t.Fatalf("SoldFromGross wrong: %+v", b)
	}
}

func TestSoldFromNet(t *testing.T) {
	// 96000 net at 4% → gross 100000, tax 4000.
	b := SoldFromNet(96_000, 0.04)
	if b.Gross != 100_000 || b.SalesTax != 4_000 || b.Net != 96_000 || !b.Estimated {
		t.Fatalf("SoldFromNet wrong: %+v", b)
	}
	// Guard degenerate rate.
	if b := SoldFromNet(500, 1.0); b.Net != 500 || b.SalesTax != 0 {
		t.Fatalf("degenerate rate: %+v", b)
	}
}

func TestSoldExact(t *testing.T) {
	b := SoldExact(100_000, 95_500)
	if b.Gross != 100_000 || b.SalesTax != 4_500 || b.Net != 95_500 || b.Estimated {
		t.Fatalf("SoldExact wrong: %+v", b)
	}
}

func TestSetupFee(t *testing.T) {
	// 2.5% of 106520 = 2663.
	if f := SetupFee(106_520); f != 2663 {
		t.Fatalf("SetupFee wrong: %d", f)
	}
	if f := SetupFee(179_834); f != 4496 {
		t.Fatalf("SetupFee wrong: %d", f)
	}
}

func TestBought(t *testing.T) {
	b := Bought(78_445)
	if b.Gross != 78_445 || b.SalesTax != 0 || b.Net != -78_445 || b.Estimated {
		t.Fatalf("Bought wrong: %+v", b)
	}
}
