package kbengine

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"
)

// medianMatch runs Match n times and returns the median duration.
func medianMatch(kb *KB, msg string, n int) time.Duration {
	ds := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		if _, err := kb.Match(msg); err != nil {
			panic(err)
		}
		ds = append(ds, time.Since(start))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return ds[len(ds)/2]
}

// importAll imports entries in batches of 1000.
func importAll(t *testing.T, kb *KB, entries []Entry) {
	t.Helper()
	for i := 0; i < len(entries); i += maxBatchSize {
		j := i + maxBatchSize
		if j > len(entries) {
			j = len(entries)
		}
		if errs := kb.Import(entries[i:j]); errs != nil {
			t.Fatalf("import batch at %d: %v", i, errs[:1])
		}
	}
}

// baseLibrary builds n random entries from a skewed vocabulary.
func baseLibrary(rng *rand.Rand, n int, idPrefix string) []Entry {
	v := newVocabGen(rng)
	entries := make([]Entry, n)
	for i := range entries {
		entries[i] = v.makeEntry(fmt.Sprintf("%s-%d", idPrefix, i), "账单", false)
	}
	return entries
}

// decoyEntry builds an entry that contains driftTerm but can never qualify
// against the probe: the drift term's weight is far below the required hit,
// and its other terms are tokens absent from the probe.
func decoyEntry(id, driftTerm string, uniq []string) Entry {
	terms := []Term{{Text: driftTerm, Weight: 1}}
	for _, u := range uniq {
		terms = append(terms, Term{Text: u, Weight: 30})
	}
	// total = 1 + 30*len(uniq); required hit = ceil(0.6*total) >= 55 >> 1
	return Entry{ID: id, Answer: "decoy", Threshold: "0.60", Terms: terms}
}

// TestPerfHighFrequencyTerm: 50k base library, then import 150k entries all
// containing 请问 but unable to qualify. Median Match time ratio must be <= 2.
func TestPerfHighFrequencyTerm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in -short mode")
	}
	rng := rand.New(rand.NewSource(11))
	kb := New()
	importAll(t, kb, baseLibrary(rng, 50000, "base"))

	probe := "请问 " + strings.Join([]string{
		"服务", "咨询", "问题", "用户", "系统", "信息", "处理", "查询",
	}, " ")
	// Warm up and measure before.
	medianMatch(kb, probe, 5)
	before := medianMatch(kb, probe, 41)

	const decoys = 150000
	entries := make([]Entry, 0, maxBatchSize)
	seq := 0
	imported := 0
	for imported < decoys {
		uniq := []string{
			fmt.Sprintf("zza%d", seq), fmt.Sprintf("zzb%d", seq), fmt.Sprintf("zzc%d", seq),
		}
		entries = append(entries, decoyEntry(fmt.Sprintf("decoy-%d", seq), "请问", uniq))
		seq++
		imported++
		if len(entries) == maxBatchSize {
			importAll(t, kb, entries)
			entries = entries[:0]
		}
	}
	after := medianMatch(kb, probe, 41)

	ratio := float64(after) / float64(before)
	t.Logf("median before=%v after=%v ratio=%.3f", before, after, ratio)
	if ratio > 2 {
		t.Fatalf("Match slowdown ratio %.3f exceeds 2 (before=%v after=%v)", ratio, before, after)
	}
}

// TestPerfFrequencyDrift: 100k library where 账单 appears in only a dozen
// entries; then 100k decoys containing 账单 are imported in batches. The
// median Match time ratio must be <= 2.
func TestPerfFrequencyDrift(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in -short mode")
	}
	rng := rand.New(rand.NewSource(12))
	kb := New()
	base := baseLibrary(rng, 100000, "base")
	// 账单 appears in only a handful of entries initially.
	for i := 0; i < 12; i++ {
		base[i*7].Terms = append(base[i*7].Terms, Term{Text: "账单", Weight: 5})
	}
	importAll(t, kb, base)

	probe := "我的账单和 " + strings.Join([]string{
		"服务", "咨询", "问题", "用户", "系统", "信息",
	}, " ")
	medianMatch(kb, probe, 5)
	before := medianMatch(kb, probe, 41)

	const decoys = 100000
	entries := make([]Entry, 0, maxBatchSize)
	seq := 0
	for imported := 0; imported < decoys; imported++ {
		uniq := []string{
			fmt.Sprintf("zza%d", seq), fmt.Sprintf("zzb%d", seq), fmt.Sprintf("zzc%d", seq),
		}
		entries = append(entries, decoyEntry(fmt.Sprintf("drift-%d", seq), "账单", uniq))
		seq++
		if len(entries) == maxBatchSize {
			importAll(t, kb, entries)
			entries = entries[:0]
		}
	}
	after := medianMatch(kb, probe, 41)

	ratio := float64(after) / float64(before)
	t.Logf("median before=%v after=%v ratio=%.3f", before, after, ratio)
	if ratio > 2 {
		t.Fatalf("Match slowdown ratio %.3f exceeds 2 (before=%v after=%v)", ratio, before, after)
	}
}

// TestPerfImportScaling: importing 200k entries in batches of 1000 must scale
// near-linearly; per-batch cost in the last quarter must not blow up.
func TestPerfImportScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in -short mode")
	}
	kb := New()
	const total = 200000
	const batches = total / maxBatchSize
	firstQuarter := batches / 4
	var firstTime, lastTime time.Duration
	for b := 0; b < batches; b++ {
		entries := make([]Entry, maxBatchSize)
		for i := range entries {
			entries[i] = decoyEntry(
				fmt.Sprintf("s-%d-%d", b, i), "请问",
				[]string{
					fmt.Sprintf("zza%d", b*1000+i),
					fmt.Sprintf("zzb%d", b*1000+i),
					fmt.Sprintf("zzc%d", b*1000+i),
				})
		}
		start := time.Now()
		importAll(t, kb, entries)
		d := time.Since(start)
		if b < firstQuarter {
			firstTime += d
		} else if b >= batches-firstQuarter {
			lastTime += d
		}
	}
	ratio := float64(lastTime) / float64(firstTime)
	t.Logf("per-batch first quarter=%v last quarter=%v ratio=%.3f",
		firstTime/time.Duration(firstQuarter), lastTime/time.Duration(firstQuarter), ratio)
	if ratio > 3 {
		t.Fatalf("import per-batch cost grew %.3fx (super-linear)", ratio)
	}
}
