package capture

import "testing"

func TestMailInfos_ShapeTolerant(t *testing.T) {
	types := []string{
		"MARKETPLACE_SELLORDER_FINISHED_SUMMARY",
		"MARKETPLACE_BUYORDER_FINISHED_SUMMARY",
	}
	ids := []int64{1001, 1002}
	locs := []string{"3005", "4002"}
	recv := []int64{1_549_840_000, 1_549_850_000}

	// The two reference clients disagree on key indices; both must resolve identically
	// because ids ride k3 (agreed) and types are found by content signature.
	variants := map[string]map[byte]interface{}{
		"C#-variant": {3: ids, 7: locs, 11: types, 12: recv},
		"Go-variant": {3: ids, 6: locs, 10: types, 11: recv},
	}
	for name, params := range variants {
		t.Run(name, func(t *testing.T) {
			got, ok := MailInfos(params)
			if !ok || len(got) != 2 {
				t.Fatalf("MailInfos ok=%v n=%d", ok, len(got))
			}
			want := []MailInfo{
				{ID: 1001, Type: types[0], LocationID: "3005", Received: 1_549_840_000},
				{ID: 1002, Type: types[1], LocationID: "4002", Received: 1_549_850_000},
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("mail %d\n got=%+v\nwant=%+v", i, got[i], want[i])
				}
			}
		})
	}
}

func TestMailInfos_AlignsToShortest(t *testing.T) {
	// A truncated types array must not index past the ids array.
	params := map[byte]interface{}{
		3:  []int64{1, 2, 3},
		11: []string{"MARKETPLACE_SELLORDER_FINISHED_SUMMARY"}, // only one
	}
	got, ok := MailInfos(params)
	if !ok || len(got) != 1 {
		t.Fatalf("expected 1 aligned row, got ok=%v n=%d", ok, len(got))
	}
}

func TestMailInfos_Rejects(t *testing.T) {
	// No marketplace-type array → not a trade mail packet.
	if _, ok := MailInfos(map[byte]interface{}{3: []int64{1}, 11: []string{"GUILD_INVITE"}}); ok {
		t.Fatal("non-trade mail packet must be rejected")
	}
	// Missing id array (k3).
	if _, ok := MailInfos(map[byte]interface{}{11: []string{"MARKETPLACE_SELLORDER_FINISHED_SUMMARY"}}); ok {
		t.Fatal("missing id array must be rejected")
	}
}

func TestReadMail(t *testing.T) {
	// Width-free id + body.
	if id, body, ok := ReadMail(map[byte]interface{}{0: int64(1002), 1: "1|T5_2H_SHAPESHIFTER_SET3@1|1549840000|1549840000"}); !ok || id != 1002 || body == "" {
		t.Fatalf("ReadMail wrong: id=%d ok=%v", id, ok)
	}
	if id, _, ok := ReadMail(map[byte]interface{}{0: int32(7), 1: "x"}); !ok || id != 7 {
		t.Fatalf("int32 id decode wrong: id=%d ok=%v", id, ok)
	}
	// Missing body.
	if _, _, ok := ReadMail(map[byte]interface{}{0: int64(1)}); ok {
		t.Fatal("missing body must be rejected")
	}
	// Missing id.
	if _, _, ok := ReadMail(map[byte]interface{}{1: "body"}); ok {
		t.Fatal("missing id must be rejected")
	}
}
