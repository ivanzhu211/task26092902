package kbengine

import (
	"strconv"
	"strings"
	"testing"
)

func ent(id, threshold string, terms ...Term) Entry {
	return Entry{ID: id, Answer: "a-" + id, Threshold: threshold, Terms: terms}
}

func tm(text string, w int) Term { return Term{Text: text, Weight: w} }

func mustImport(t *testing.T, kb *KB, entries ...Entry) {
	t.Helper()
	if errs := kb.Import(entries); errs != nil {
		t.Fatalf("Import failed: %v", errs)
	}
}

func mustMatch(t *testing.T, kb *KB, text string) MatchResult {
	t.Helper()
	r, err := kb.Match(text)
	if err != nil {
		t.Fatalf("Match(%q) error: %v", text, err)
	}
	return r
}

func TestImportValidation(t *testing.T) {
	kb := New()
	long := strings.Repeat("a", 9) // 9 tokens? no: one token; build 9-token term below
	_ = long
	nineTokens := strings.Join([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}, " ")
	errs := kb.Import([]Entry{
		ent("", "0.6", tm("x", 1)),                      // 0: empty id
		ent("a", "0.555", tm("x", 1)),                   // 1: bad threshold
		ent("b", "0.6"),                                 // 2: no terms
		ent("c", "0.6", tm("x", 0)),                     // 3: weight too small
		ent("d", "0.6", tm("x", 101)),                   // 4: weight too big
		ent("e", "0.6", tm("！！！", 5)),                   // 5: zero tokens after tokenize
		ent("f", "0.6", tm(nineTokens, 5)),              // 6: too many tokens
		ent("g", "0.6", tm("Hello", 5), tm("hello", 3)), // 7: dup after tokenization
		ent("h", "0.6", tm("x", 1)),
		ent("h", "0.6", tm("y", 1)), // 9: dup id in batch
	})
	if len(errs) == 0 {
		t.Fatal("expected row errors")
	}
	byLineField := map[[2]string]string{}
	for _, e := range errs {
		byLineField[[2]string{string(rune('0' + e.Line)), e.Field}] = e.Reason
	}
	wantFields := map[int][]string{
		0: {"id"}, 1: {"threshold"}, 2: {"terms"}, 3: {"terms[0].weight"},
		4: {"terms[0].weight"}, 5: {"terms[0].text"}, 6: {"terms[0].text"},
		7: {"terms[1].text"}, 9: {"id"},
	}
	gotLines := map[int][]string{}
	for _, e := range errs {
		gotLines[e.Line] = append(gotLines[e.Line], e.Field)
	}
	for line, fields := range wantFields {
		for _, f := range fields {
			found := false
			for _, gf := range gotLines[line] {
				if gf == f {
					found = true
				}
			}
			if !found {
				t.Errorf("line %d: missing error for field %q (got %v)", line, f, gotLines[line])
			}
		}
	}
	// Whole batch rejected: line 8 was valid but must not be visible.
	if r := mustMatch(t, kb, "x"); r.Found {
		t.Errorf("rejected batch leaked into KB: %+v", r)
	}
}

func TestImportBatchTooLarge(t *testing.T) {
	kb := New()
	entries := make([]Entry, 1001)
	for i := range entries {
		entries[i] = ent(strings.Repeat("x", 1)+strconv.Itoa(i), "0.6", tm("x", 1))
	}
	errs := kb.Import(entries)
	if len(errs) != 1 || errs[0].Line != -1 || errs[0].Field != "batch" {
		t.Fatalf("expected single batch error, got %v", errs)
	}
}

func TestMatchBasics(t *testing.T) {
	kb := New()
	mustImport(t, kb,
		ent("e1", "0.6", tm("你好", 10)),              // tokens 你 好
		ent("e2", "0.6", tm("hello world", 10)),     // phrase, contiguous
		ent("e3", "0.6", tm("world hello", 10)),     // reversed phrase
		ent("e4", "0.6", tm("你好", 3), tm("abc", 7)), // total 10
	)
	if r := mustMatch(t, kb, "你好"); !r.Found || r.ID != "e1" || r.HitWeight != 10 || r.TotalWeight != 10 {
		t.Errorf("match 你好: %+v", r)
	}
	if r := mustMatch(t, kb, "say hello world please"); !r.Found || r.ID != "e2" {
		t.Errorf("contiguous phrase: %+v", r)
	}
	if r := mustMatch(t, kb, "say hello world please"); r.Found && r.ID == "e3" {
		t.Errorf("reversed phrase must not match: %+v", r)
	}
	// e4: only 你好 hits -> 3/10 = 0.3 < 0.6, must not qualify.
	if r := mustMatch(t, kb, "你好"); r.Found && r.ID == "e4" {
		t.Errorf("partial hit below threshold: %+v", r)
	}
	// "你好 abc": e1 and e4 both score 1.0; tie goes to smaller id e1.
	if r := mustMatch(t, kb, "你好 abc"); !r.Found || r.ID != "e1" {
		t.Errorf("tie on 1.0 should pick e1: %+v", r)
	}
	kbSolo := New()
	mustImport(t, kbSolo, ent("e4", "0.6", tm("你好", 3), tm("abc", 7)))
	if r := mustMatch(t, kbSolo, "你好 abc"); !r.Found || r.ID != "e4" || r.HitWeight != 10 {
		t.Errorf("full hit: %+v", r)
	}
	if r := mustMatch(t, kb, "unrelated text"); r.Found {
		t.Errorf("expected miss, got %+v", r)
	}
}

func TestMatchExactBoundary(t *testing.T) {
	kb := New()
	// total 100, hit exactly 60 == threshold 0.60 -> qualifies.
	mustImport(t, kb,
		ent("eq", "0.60", tm("hit", 60), tm("miss", 40)),
		// total 100, hit 59 < 60 -> never qualifies on "hit".
		ent("below", "0.60", tm("hit", 59), tm("miss", 41)),
		// effective threshold is max(0.6, 0.3) = 0.6: total 5, hit 3 -> 0.6 qualifies.
		ent("floor", "0.3", tm("hit", 3), tm("miss", 2)),
		// threshold 1.00: needs every term.
		ent("perfect", "1.00", tm("hit", 1), tm("other", 1)),
	)
	r := mustMatch(t, kb, "hit")
	if !r.Found || r.ID != "eq" || r.HitWeight != 60 || r.TotalWeight != 100 {
		t.Errorf("exact boundary: %+v", r)
	}
	kb2 := New()
	mustImport(t, kb2, ent("below", "0.60", tm("hit", 59), tm("miss", 41)))
	if r := mustMatch(t, kb2, "hit"); r.Found {
		t.Errorf("59/100 must not reach 0.60: %+v", r)
	}
	kb3 := New()
	mustImport(t, kb3, ent("floor", "0.3", tm("hit", 3), tm("miss", 2)))
	if r := mustMatch(t, kb3, "hit"); !r.Found || r.HitWeight != 3 {
		t.Errorf("0.6 floor: %+v", r)
	}
	kb4 := New()
	mustImport(t, kb4, ent("perfect", "1.00", tm("hit", 1), tm("other", 1)))
	if r := mustMatch(t, kb4, "hit"); r.Found {
		t.Errorf("1.00 threshold needs all terms: %+v", r)
	}
	if r := mustMatch(t, kb4, "hit other"); !r.Found || r.ID != "perfect" {
		t.Errorf("1.00 full hit: %+v", r)
	}
}

func TestMatchTieBreak(t *testing.T) {
	kb := New()
	// Same score 3/5 vs 6/10 -> equal; bytewise smallest id wins.
	mustImport(t, kb,
		ent("b", "0.6", tm("hit", 6), tm("x", 4)),
		ent("a", "0.6", tm("hit", 3), tm("x", 2)),
		ent("c", "0.6", tm("hit", 60), tm("x", 40)),
	)
	r := mustMatch(t, kb, "hit")
	if !r.Found || r.ID != "a" {
		t.Errorf("tie should pick smallest id, got %+v", r)
	}
	// Higher score wins regardless of id.
	mustImport(t, kb, ent("z", "0.6", tm("hit", 4), tm("x", 1)))
	if r := mustMatch(t, kb, "hit"); !r.Found || r.ID != "z" {
		t.Errorf("higher score should win, got %+v", r)
	}
}

func TestReplaceAndDelete(t *testing.T) {
	kb := New()
	mustImport(t, kb, ent("e1", "0.6", tm("apple", 10)))
	mustImport(t, kb, ent("e1", "0.6", tm("banana", 10))) // replace
	if r := mustMatch(t, kb, "apple"); r.Found {
		t.Errorf("replaced entry must forget old terms: %+v", r)
	}
	if r := mustMatch(t, kb, "banana"); !r.Found || r.ID != "e1" {
		t.Errorf("replaced entry should match new terms: %+v", r)
	}
	kb.Delete([]string{"e1", "missing", "e1"})
	if r := mustMatch(t, kb, "banana"); r.Found {
		t.Errorf("deleted entry must not match: %+v", r)
	}
	// Re-import after delete works.
	mustImport(t, kb, ent("e1", "0.6", tm("cherry", 10)))
	if r := mustMatch(t, kb, "cherry"); !r.Found || r.ID != "e1" {
		t.Errorf("re-import after delete: %+v", r)
	}
}

func TestMatchTextTooLong(t *testing.T) {
	kb := New()
	if _, err := kb.Match(strings.Repeat("a", 2001)); err == nil {
		t.Error("expected error for >2000 runes")
	}
	if _, err := kb.Match(strings.Repeat("a", 2000)); err != nil {
		t.Errorf("2000 runes must be accepted: %v", err)
	}
}

func TestOverlappingTerms(t *testing.T) {
	kb := New()
	// Terms overlap and contain each other; all must be scored.
	mustImport(t, kb,
		ent("ov", "0.6",
			tm("机器学习", 5), // 机 器 学 习
			tm("机器", 3),   // 机 器
			tm("学习", 2),   // 学 习
			tm("机器学", 4),  // 机 器 学
		),
	)
	r := mustMatch(t, kb, "机器学习")
	if !r.Found || r.HitWeight != 14 || r.TotalWeight != 14 {
		t.Errorf("overlapping terms: %+v", r)
	}
}
