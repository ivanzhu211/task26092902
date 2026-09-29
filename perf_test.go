package kbmatch

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"
)

// medianMatch runs Match n times and returns the median wall time.
func medianMatch(eng *Engine, probe string, n int) time.Duration {
	ds := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		eng.Match(probe)
		ds = append(ds, time.Since(start))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return ds[n/2]
}

// importBatches imports entries in batches of 1000.
func importBatches(t *testing.T, eng *Engine, entries []Entry) {
	t.Helper()
	for i := 0; i < len(entries); i += 1000 {
		end := i + 1000
		if end > len(entries) {
			end = len(entries)
		}
		if errs := eng.Import(entries[i:end]); len(errs) != 0 {
			t.Fatalf("import batch %d: %v", i/1000, errs[:3])
		}
	}
}

// baseEntry builds a random base-library entry from a shared vocabulary so
// that probe tokens occur with realistic frequency.
func baseEntry(rng *rand.Rand, id string, vocab []string) Entry {
	n := 2 + rng.Intn(3)
	e := Entry{ID: id, Threshold: "0.7"}
	seen := map[string]bool{}
	for len(e.Terms) < n {
		text := vocab[rng.Intn(len(vocab))]
		if rng.Intn(3) == 0 {
			text += " " + vocab[rng.Intn(len(vocab))]
		}
		tokens := tokenize(text)
		if len(tokens) == 0 || len(tokens) > 8 {
			continue
		}
		key := termKey(tokens)
		if seen[key] {
			continue
		}
		seen[key] = true
		e.Terms = append(e.Terms, Term{Text: text, Weight: 1 + rng.Intn(30)})
	}
	return e
}

// hopelessEntry builds an entry that contains hotTerm but can never pass
// for the probe: the hot term's weight is tiny next to three cold terms
// whose tokens never appear in the probe.
func hopelessEntry(id int, hotTerm string, cold []string, rng *rand.Rand) Entry {
	return Entry{
		ID:        fmt.Sprintf("adv%08d", id),
		Threshold: "0.6",
		Terms: []Term{
			{Text: hotTerm, Weight: 1},
			{Text: cold[id%len(cold)], Weight: 40},
			{Text: cold[(id+1)%len(cold)], Weight: 40},
			{Text: cold[(id+2)%len(cold)], Weight: 40},
		},
	}
}

func TestPerfHotWordRatio(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf test in -short mode")
	}
	rng := rand.New(rand.NewSource(42))
	// vocabulary covering the probe tokens so the base library does real work
	vocab := []string{"请", "问", "如", "何", "办", "理", "业", "务"}
	for i := 0; i < 500; i++ {
		vocab = append(vocab, fmt.Sprintf("v%04d", i))
	}
	cold := make([]string, 1000)
	for i := range cold {
		cold[i] = fmt.Sprintf("zx%04d", i)
	}
	eng := New()
	base := make([]Entry, 0, 50000)
	for i := 0; i < 50000; i++ {
		base = append(base, baseEntry(rng, fmt.Sprintf("b%08d", i), vocab))
	}
	importBatches(t, eng, base)
	probe := "请问如何办理业务"
	before := medianMatch(eng, probe, 201)
	adv := make([]Entry, 0, 150000)
	for i := 0; i < 150000; i++ {
		adv = append(adv, hopelessEntry(i, "请问", cold, rng))
	}
	importBatches(t, eng, adv)
	after := medianMatch(eng, probe, 201)
	ratio := float64(after) / float64(before)
	t.Logf("median before=%v after=%v ratio=%.2f", before, after, ratio)
	if ratio > 2 {
		t.Fatalf("Match median ratio %.2f exceeds 2 (before=%v after=%v)", ratio, before, after)
	}
}

func TestPerfDriftRatio(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf test in -short mode")
	}
	rng := rand.New(rand.NewSource(43))
	vocab := []string{"怎", "么", "还", "款", "查", "询"}
	for i := 0; i < 500; i++ {
		vocab = append(vocab, fmt.Sprintf("v%04d", i))
	}
	cold := make([]string, 1000)
	for i := range cold {
		cold[i] = fmt.Sprintf("zx%04d", i)
	}
	eng := New()
	base := make([]Entry, 0, 100000)
	for i := 0; i < 100000; i++ {
		base = append(base, baseEntry(rng, fmt.Sprintf("b%08d", i), vocab))
	}
	// "账单" is rare: only a dozen entries contain it
	for i := 0; i < 12; i++ {
		base[i*100].Terms = append(base[i*100].Terms[:1],
			Term{Text: "账单", Weight: 5}, Term{Text: "zx0001", Weight: 50})
	}
	importBatches(t, eng, base)
	probe := "账单怎么还款"
	before := medianMatch(eng, probe, 201)
	// the word drifts hot: 100k new entries all contain "账单"
	adv := make([]Entry, 0, 100000)
	for i := 0; i < 100000; i++ {
		adv = append(adv, hopelessEntry(i, "账单", cold, rng))
	}
	importBatches(t, eng, adv)
	after := medianMatch(eng, probe, 201)
	ratio := float64(after) / float64(before)
	t.Logf("median before=%v after=%v ratio=%.2f", before, after, ratio)
	if ratio > 2 {
		t.Fatalf("Match median ratio %.2f exceeds 2 (before=%v after=%v)", ratio, before, after)
	}
}
