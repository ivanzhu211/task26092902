package kbengine

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// bruteMatch is the reference implementation: it scores every live entry
// exactly, directly from the raw Entry inputs.
func bruteMatch(entries map[string]Entry, text string) MatchResult {
	tokens := tokenize(text)
	ngrams := make(map[string]struct{}, len(tokens)*4)
	for n := 1; n <= maxTermTokens && n <= len(tokens); n++ {
		for i := 0; i+n <= len(tokens); i++ {
			ngrams[termKey(tokens[i:i+n])] = struct{}{}
		}
	}
	var bestID string
	bestHit, bestTotal := 0, 0
	found := false
	for id, e := range entries {
		th, err := parseThreshold(e.Threshold)
		if err != nil {
			continue
		}
		m := th
		if m < minEffectiveTh {
			m = minEffectiveTh
		}
		hit, total := 0, 0
		seen := make(map[string]bool)
		for _, t := range e.Terms {
			key := termKey(tokenize(t.Text))
			if seen[key] {
				continue
			}
			seen[key] = true
			total += t.Weight
			if _, ok := ngrams[key]; ok {
				hit += t.Weight
			}
		}
		if total == 0 || hit*100 < m*total {
			continue
		}
		if !found ||
			int64(hit)*int64(bestTotal) > int64(bestHit)*int64(total) ||
			(int64(hit)*int64(bestTotal) == int64(bestHit)*int64(total) && id < bestID) {
			found, bestID, bestHit, bestTotal = true, id, hit, total
		}
	}
	return MatchResult{ID: bestID, HitWeight: bestHit, TotalWeight: bestTotal, Found: found}
}

// vocabGen builds a skewed token vocabulary: Han chars, latin words, digits.
type vocabGen struct {
	rng   *rand.Rand
	hans  []string
	latin []string
}

func newVocabGen(rng *rand.Rand) *vocabGen {
	v := &vocabGen{rng: rng}
	for i := 0; i < 120; i++ {
		v.hans = append(v.hans, string(rune(0x4E00+rng.Intn(0x3000))))
	}
	for i := 0; i < 120; i++ {
		n := 2 + rng.Intn(5)
		b := make([]byte, n)
		for j := range b {
			b[j] = byte('a' + rng.Intn(26))
		}
		v.latin = append(v.latin, string(b))
	}
	return v
}

// token picks a vocabulary token with zipf-like skew: low indexes dominate.
func (v *vocabGen) token() string {
	zipf := func(n int) int {
		// geometric-ish: heavily favors small indexes
		k := 0
		for k < n-1 && v.rng.Intn(3) != 0 {
			k++
		}
		return k
	}
	if v.rng.Intn(2) == 0 {
		return v.hans[zipf(len(v.hans))]
	}
	return v.latin[zipf(len(v.latin))]
}

// termText builds a term of 1..4 tokens; sometimes derives it from an
// existing term to create overlap/containment.
func (v *vocabGen) termText(existing []string) string {
	if len(existing) > 0 && v.rng.Intn(4) == 0 {
		base := tokenize(existing[v.rng.Intn(len(existing))])
		if len(base) > 1 {
			// sub-phrase or super-phrase of an existing term
			if v.rng.Intn(2) == 0 {
				start := v.rng.Intn(len(base))
				end := start + 1 + v.rng.Intn(len(base)-start)
				return strings.Join(base[start:end], " ")
			}
			extra := v.token()
			return strings.Join(append(append([]string{}, base...), extra), " ")
		}
	}
	n := 1 + v.rng.Intn(4)
	parts := make([]string, n)
	for i := range parts {
		parts[i] = v.token()
	}
	return strings.Join(parts, " ")
}

var thresholdChoices = []string{"0", "0.3", "0.6", "0.60", "0.75", "0.9", "0.95", "1", "1.00"}

// makeEntry builds a random entry. With some probability the total weight is
// exactly 100 so that hits land exactly on threshold boundaries.
func (v *vocabGen) makeEntry(id string, driftToken string, drift bool) Entry {
	nTerms := 1 + v.rng.Intn(5)
	var termTexts []string
	terms := make([]Term, 0, nTerms)
	exact100 := v.rng.Intn(3) == 0
	weights := make([]int, nTerms)
	if exact100 {
		// random composition of 100 into nTerms parts, each in 1..100
		rem := 100
		for i := 0; i < nTerms; i++ {
			left := nTerms - i
			w := 1 + v.rng.Intn(rem-left+1)
			if i == nTerms-1 {
				w = rem
			}
			if w > 100 {
				w = 100
			}
			weights[i] = w
			rem -= w
		}
	} else {
		for i := range weights {
			weights[i] = 1 + v.rng.Intn(30)
		}
	}
	used := map[string]bool{}
	for i := 0; i < nTerms; i++ {
		var text string
		if drift && i == 0 {
			text = driftToken + " " + v.token()
		} else {
			text = v.termText(termTexts)
		}
		key := termKey(tokenize(text))
		if used[key] || len(tokenize(text)) == 0 || len(tokenize(text)) > maxTermTokens {
			continue
		}
		used[key] = true
		termTexts = append(termTexts, text)
		terms = append(terms, Term{Text: text, Weight: weights[i]})
	}
	if len(terms) == 0 {
		terms = append(terms, Term{Text: v.token(), Weight: 1})
	}
	th := thresholdChoices[v.rng.Intn(len(thresholdChoices))]
	if exact100 {
		// bias toward thresholds that equal a reachable hit weight
		th = fmt.Sprintf("0.%02d", 55+v.rng.Intn(45))
	}
	return Entry{ID: id, Answer: "ans-" + id, Threshold: th, Terms: terms}
}

func (v *vocabGen) message(live map[string]Entry, driftToken string, driftOn bool) string {
	n := v.rng.Intn(30)
	parts := make([]string, 0, n+4)
	for i := 0; i < n; i++ {
		parts = append(parts, v.token())
	}
	// Sometimes quote a random live entry's terms fully or partially.
	if len(live) > 0 && v.rng.Intn(3) == 0 {
		k := v.rng.Intn(len(live))
		for _, e := range live {
			if k == 0 {
				for _, t := range e.Terms {
					if v.rng.Intn(2) == 0 {
						parts = append(parts, t.Text)
					}
				}
				break
			}
			k--
		}
	}
	if driftOn && v.rng.Intn(2) == 0 {
		parts = append(parts, driftToken)
	}
	if v.rng.Intn(4) == 0 {
		parts = append(parts, "ＦＵＬＬＷＩＤＴＨ", "未登录词xyz")
	}
	return strings.Join(parts, " ")
}

func TestDifferential(t *testing.T) {
	for _, seed := range []int64{20260930, 1, 424242} {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			runDifferential(t, seed)
		})
	}
}

func runDifferential(t *testing.T, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	v := newVocabGen(rng)
	kb := New()
	live := map[string]Entry{}
	driftToken := "账单"
	driftOn := false
	idSeq := 0

	applyImport := func(batch []Entry) {
		t.Helper()
		if errs := kb.Import(batch); errs != nil {
			t.Fatalf("Import of %d entries failed: %v", len(batch), errs[:3])
		}
		for _, e := range batch {
			live[e.ID] = e
		}
	}
	check := func(round, mi int, msg string) {
		t.Helper()
		got, err := kb.Match(msg)
		if err != nil {
			t.Fatalf("round %d msg %d: Match error: %v", round, mi, err)
		}
		want := bruteMatch(live, msg)
		if got != want {
			t.Fatalf("round %d msg %d mismatch:\n msg=%q\n got=%+v\n want=%+v",
				round, mi, msg, got, want)
		}
	}

	const rounds = 40
	for round := 0; round < rounds; round++ {
		if round == rounds/2 {
			driftOn = true // 账单 starts drifting from rare to frequent
		}
		switch round % 4 {
		case 0, 1, 2: // import new + replacements
			n := 1 + rng.Intn(150)
			batch := make([]Entry, 0, n)
			usedID := map[string]bool{}
			for i := 0; i < n; i++ {
				var id string
				if len(live) > 0 && rng.Intn(5) == 0 {
					// replace an existing entry
					k := rng.Intn(len(live))
					for id = range live {
						if k == 0 {
							break
						}
						k--
					}
				} else {
					id = fmt.Sprintf("id%06d", idSeq)
					idSeq++
				}
				if usedID[id] {
					continue
				}
				usedID[id] = true
				batch = append(batch, v.makeEntry(id, driftToken, driftOn && rng.Intn(3) != 0))
			}
			applyImport(batch)
		case 3: // delete
			var ids []string
			for id := range live {
				if rng.Intn(4) == 0 {
					ids = append(ids, id)
				}
			}
			ids = append(ids, "never-existed")
			kb.Delete(ids)
			for _, id := range ids {
				delete(live, id)
			}
		}
		for mi := 0; mi < 30; mi++ {
			msg := v.message(live, driftToken, driftOn)
			check(round, mi, msg)
		}
	}
	t.Logf("final library size: %d entries", len(live))
}
