package export

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"
)

// Contract §1: BOM first, CRLF endings, RFC 4180 quoting, empty rows still valid.
func TestEncodeBOMAndCRLF(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, []string{"a", "b"}, [][]string{{"1", "2"}}); err != nil {
		t.Fatal(err)
	}
	out := buf.Bytes()
	if !bytes.HasPrefix(out, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("output must start with the UTF-8 BOM")
	}
	if !bytes.Contains(out, []byte("a,b\r\n")) {
		t.Fatalf("CRLF line endings required, got %q", out)
	}
}

func TestEncodeQuotingRoundTrip(t *testing.T) {
	// Fields with comma, quote and newline must survive a read-back intact.
	tricky := []string{`Ertac's "Axe", T5`, "line1\nline2", "plain"}
	var buf bytes.Buffer
	if err := Encode(&buf, []string{"x", "y", "z"}, [][]string{tricky}); err != nil {
		t.Fatal(err)
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(buf.Bytes(), []byte{0xEF, 0xBB, 0xBF})))
	recs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("want header+1 row, got %d records", len(recs))
	}
	for i, want := range tricky {
		if recs[1][i] != want {
			t.Fatalf("field %d mangled: want %q got %q", i, want, recs[1][i])
		}
	}
}

func TestEncodeTurkishBytes(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, []string{"item"}, [][]string{{"İğüşöçŞÖÇ"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "İğüşöçŞÖÇ") {
		t.Fatal("Turkish characters must pass through byte-exact")
	}
}

func TestEncodeEmptyRows(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, []string{"a", "b"}, nil); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xEF, 0xBB, 0xBF}, []byte("a,b\r\n")...)
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("empty dataset must still be BOM+header, got %q", buf.Bytes())
	}
}

func TestFilename(t *testing.T) {
	ts := time.Date(2026, 7, 6, 15, 30, 0, 0, time.Local)
	if got := Filename("holdings", ts); got != "albion-holdings-20260706-153000.csv" {
		t.Fatalf("filename wrong: %s", got)
	}
}
