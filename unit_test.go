package kbmatch

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"hello world", []string{"hello", "world"}},
		{"Hello WORLD", []string{"hello", "world"}},
		{"abc123", []string{"abc123"}},
		{"ＡＢＣ１２３", []string{"abc123"}},
		{"Ｑｑ", []string{"qq"}},
		{"你好", []string{"你", "好"}},
		{"!", nil},
		{"ab中cd", []string{"ab", "中", "cd"}},
		{"中中", []string{"中", "中"}},
		{"a-b_c!", []string{"a", "b", "c"}},
		{"café", []string{"café"}},
	}
	for _, c := range cases {
		got := tokenize(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("tokenize(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseThreshold(t *testing.T) {
	good := map[string]int{
		"0": 0, "0.0": 0, "0.00": 0, "0.6": 60, "0.60": 60,
		"0.05": 5, "0.99": 99, "1": 100, "1.0": 100, "1.00": 100,
	}
	for s, want := range good {
		if got, ok := parseThreshold(s); !ok || got != want {
			t.Errorf("parseThreshold(%q) = %d,%v, want %d,true", s, got, ok, want)
		}
	}
	bad := []string{"", ".5", "0.", "0.555", "1.01", "2", "-0.5", "abc", "0.5.0", " 0.5", "0,5"}
	for _, s := range bad {
		if _, ok := parseThreshold(s); ok {
			t.Errorf("parseThreshold(%q) should fail", s)
		}
	}
}

func TestImportValidation(t *testing.T) {
	eng := New()
	valid := Entry{ID: "ok", Threshold: "0.5", Terms: []Term{{Text: "你好", Weight: 10}}}
	batch := []Entry{
		valid,
		{ID: "", Threshold: "0.5", Terms: []Term{{Text: "a", Weight: 1}}},              // empty id
		{ID: "x1", Threshold: "1.5", Terms: []Term{{Text: "a", Weight: 1}}},            // bad threshold
		{ID: "x2", Threshold: "0.5", Terms: nil},                                       // no terms
		{ID: "x3", Threshold: "0.5", Terms: []Term{{Text: "a", Weight: 0}}},            // bad weight
		{ID: "x4", Threshold: "0.5", Terms: []Term{{Text: "!!!", Weight: 1}}},          // empty tokens
		{ID: "x5", Threshold: "0.5", Terms: []Term{{Text: "一 二 三 四 五 六 七 八 九", Weight: 1}}}, // >8 tokens
		{ID: "x6", Threshold: "0.5", Terms: []Term{{Text: "ab", Weight: 1}, {Text: "AB", Weight: 2}}}, // dup term
		{ID: "ok", Threshold: "0.5", Terms: []Term{{Text: "b", Weight: 1}}},            // dup id in batch
	}
	errs := eng.Import(batch)
	if len(errs) != 8 {
		t.Fatalf("expected 8 row errors, got %d: %v", len(errs), errs)
	}
	for _, e := range errs {
		if e.Line < 1 || e.Line > 8 {
			t.Errorf("bad line in %+v", e)
		}
		if e.Field == "" || e.Reason == "" {
			t.Errorf("missing field/reason in %+v", e)
		}
	}
	// whole batch rejected
	if _, found, _ := eng.Match("你好"); found {
		t.Fatal("batch should have been rejected entirely")
	}
	// too many terms
	many := Entry{ID: "m", Threshold: "0.5"}
	for i := 0; i < 21; i++ {
		many.Terms = append(many.Terms, Term{Text: string(rune('a'+i)), Weight: 1})
	}
	if errs := eng.Import([]Entry{many}); len(errs) != 1 {
		t.Fatalf("expected 1 error for 21 terms, got %v", errs)
	}
	// batch too large
	big := make([]Entry, 1001)
	for i := range big {
		big[i] = Entry{ID: string(rune(i + 1)), Threshold: "0.5", Terms: []Term{{Text: "a", Weight: 1}}}
	}
	if errs := eng.Import(big); len(errs) != 1 || errs[0].Field != "batch" {
		t.Fatalf("expected batch error, got %v", errs)
	}
	// valid batch applies
	if errs := eng.Import([]Entry{valid}); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if _, found, _ := eng.Match("你好"); !found {
		t.Fatal("expected match after valid import")
	}
}

func TestBoundaryAndTie(t *testing.T) {
	eng := New()
	// need = ceil(10*60/100) = 6; craft exact hit 6 vs 5.
	errs := eng.Import([]Entry{
		{ID: "e1", Threshold: "0.6", Terms: []Term{{Text: "你好", Weight: 3}, {Text: "世界", Weight: 7}}},
		{ID: "e2", Threshold: "0.60", Terms: []Term{{Text: "你好", Weight: 4}, {Text: "朋友", Weight: 6}}},
	})
	if len(errs) != 0 {
		t.Fatalf("import: %v", errs)
	}
	// message hits only "你好": e1 hit(3) < need(6), e2 hit(4) < need(6) -> no match
	if _, found, _ := eng.Match("你好"); found {
		t.Fatal("unexpected match below threshold")
	}
	// replace e1 so that "你好" alone hits exactly need=6
	eng.Import([]Entry{
		{ID: "e1", Threshold: "0.6", Terms: []Term{{Text: "你好", Weight: 6}, {Text: "世界", Weight: 4}}},
	})
	res, found, _ := eng.Match("你好")
	if !found || res.ID != "e1" || res.HitWeight != 6 || res.TotalWeight != 10 {
		t.Fatalf("boundary hit failed: %+v found=%v", res, found)
	}
	// tie: e3 same score 6/10, larger id; e0 same score, smaller id wins
	eng.Import([]Entry{
		{ID: "e3", Threshold: "0.6", Terms: []Term{{Text: "你好", Weight: 6}, {Text: "世界", Weight: 4}}},
		{ID: "e0", Threshold: "0.6", Terms: []Term{{Text: "你好", Weight: 12}, {Text: "世界", Weight: 8}}},
	})
	res, found, _ = eng.Match("你好")
	if !found || res.ID != "e0" || res.HitWeight != 12 || res.TotalWeight != 20 {
		t.Fatalf("tie-break failed: %+v found=%v", res, found)
	}
	// threshold below 0.6 still requires 0.6: total 10, need 6
	eng.Import([]Entry{
		{ID: "low", Threshold: "0.1", Terms: []Term{{Text: "你好", Weight: 5}, {Text: "世界", Weight: 5}}},
	})
	res, _, _ = eng.Match("你好")
	if res.ID == "low" {
		t.Fatal("0.6 floor not applied")
	}
	// delete then no match for that id
	eng.Delete([]string{"e0", "e1", "e3", "nonexistent"})
	if _, found, _ := eng.Match("你好"); found {
		t.Fatal("match after delete")
	}
}

func TestMatchEdges(t *testing.T) {
	eng := New()
	eng.Import([]Entry{{ID: "a", Threshold: "0.5", Terms: []Term{{Text: "你好", Weight: 1}}}})
	// empty / tokenless message
	if _, found, err := eng.Match(""); found || err != nil {
		t.Fatal("empty message should not match")
	}
	if _, found, err := eng.Match("！？。、"); found || err != nil {
		t.Fatal("tokenless message should not match")
	}
	// overlong message: 2001 runes
	long := make([]rune, 2001)
	for i := range long {
		long[i] = 'a'
	}
	if _, _, err := eng.Match(string(long)); err != ErrMessageTooLong {
		t.Fatalf("expected ErrMessageTooLong, got %v", err)
	}
	// exactly 2000 runes is fine
	if _, _, err := eng.Match(string(long[:2000])); err != nil {
		t.Fatalf("2000 runes should be accepted: %v", err)
	}
}
