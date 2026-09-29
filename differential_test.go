package kbmatch

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

// genEnv holds the shared vocabulary for the differential test.
type genEnv struct {
	hot       []string   // very frequent tokens
	cold      []string   // rare tokens
	templates [][]string // phrase templates; terms are slices of these
}

func newGenEnv() *genEnv {
	env := &genEnv{
		hot: []string{"请", "问", "我", "的", "是", "ab", "a", "ok"},
	}
	for i := 0; i < 200; i++ {
		if i%2 == 0 {
			env.cold = append(env.cold, string(rune(0x4e00+i*7)))
		} else {
			env.cold = append(env.cold, "w"+strconv.Itoa(i))
		}
	}
	for i := 0; i < 12; i++ {
		n := 3 + i%6
		tpl := make([]string, n)
		for j := range tpl {
			if j%3 == 0 {
				tpl[j] = env.hot[(i+j)%len(env.hot)]
			} else {
				tpl[j] = env.cold[(i*5+j*11)%len(env.cold)]
			}
		}
		env.templates = append(env.templates, tpl)
	}
	return env
}

// genTerm builds one term; drift adds the drifting hot phrase "账单".
func (env *genEnv) genTerm(rng *rand.Rand, drift bool) string {
	var tokens []string
	switch rng.Intn(5) {
	case 0, 1: // single token, usually hot
		if drift && rng.Intn(3) == 0 {
			tokens = []string{"账", "单"}
		} else if rng.Intn(4) > 0 {
			tokens = []string{env.hot[rng.Intn(len(env.hot))]}
		} else {
			tokens = []string{env.cold[rng.Intn(len(env.cold))]}
		}
	case 2: // contiguous slice of a template (mutually containing phrases)
		tpl := env.templates[rng.Intn(len(env.templates))]
		start := rng.Intn(len(tpl))
		length := 1 + rng.Intn(len(tpl)-start)
		tokens = append(tokens, tpl[start:start+length]...)
	case 3: // repeated token
		tok := env.hot[rng.Intn(len(env.hot))]
		if rng.Intn(2) == 0 {
			tok = env.cold[rng.Intn(len(env.cold))]
		}
		for i, n := 0, 2+rng.Intn(3); i < n; i++ {
			tokens = append(tokens, tok)
		}
	default: // random short sequence
		for i, n := 0, 1+rng.Intn(4); i < n; i++ {
			if rng.Intn(2) == 0 {
				tokens = append(tokens, env.hot[rng.Intn(len(env.hot))])
			} else {
				tokens = append(tokens, env.cold[rng.Intn(len(env.cold))])
			}
		}
	}
	return strings.Join(tokens, " ")
}

func (env *genEnv) genEntry(rng *rand.Rand, id string, drift bool) Entry {
	thresholds := []string{"0.6", "0.60", "0.5", "0.75", "0.9", "0.05", "1.0"}
	thr := thresholds[rng.Intn(len(thresholds))]
	if rng.Intn(4) == 0 {
		thr = fmt.Sprintf("0.%02d", rng.Intn(100))
	}
	e := Entry{ID: id, Threshold: thr}
	seen := map[string]bool{}
	for n := 1 + rng.Intn(6); len(e.Terms) < n; {
		text := env.genTerm(rng, drift)
		tokens := tokenize(text)
		if len(tokens) == 0 || len(tokens) > 8 {
			continue
		}
		key := strings.Join(tokens, " ")
		if seen[key] {
			continue
		}
		seen[key] = true
		weight := 1 + rng.Intn(10)
		if rng.Intn(5) == 0 {
			weight = 1 + rng.Intn(100)
		}
		e.Terms = append(e.Terms, Term{Text: text, Weight: weight})
	}
	return e
}

func (env *genEnv) genMessage(rng *rand.Rand, drift bool) string {
	seps := []string{" ", "，", "。", "！", " "}
	var tokens []string
	for i, n := 0, 3+rng.Intn(13); i < n; i++ {
		r := rng.Intn(10)
		switch {
		case drift && r == 0:
			tokens = append(tokens, "账", "单")
		case r < 6:
			tokens = append(tokens, env.hot[rng.Intn(len(env.hot))])
		case r < 8:
			tpl := env.templates[rng.Intn(len(env.templates))]
			tokens = append(tokens, tpl...)
		default:
			tokens = append(tokens, env.cold[rng.Intn(len(env.cold))])
		}
	}
	var sb strings.Builder
	for i, tok := range tokens {
		if i > 0 {
			sb.WriteString(seps[rng.Intn(len(seps))])
		}
		sb.WriteString(tok)
	}
	return sb.String()
}

func runDifferential(t *testing.T, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	env := newGenEnv()
	eng := New()
	model := map[string]Entry{}
	var live []string
	idCounter := 0
	const iterations = 40
	for iter := 0; iter < iterations; iter++ {
		drift := iter >= iterations/2 // second half: "账单" drifts hot
		// one import batch: mostly new ids, some replacements
		batchSize := 40 + rng.Intn(80)
		batch := make([]Entry, 0, batchSize)
		for i := 0; i < batchSize; i++ {
			if len(live) > 0 && rng.Intn(5) == 0 {
				id := live[rng.Intn(len(live))]
				ent := env.genEntry(rng, id, drift)
				if errs := eng.Import([]Entry{ent}); len(errs) == 0 {
					model[id] = ent
				}
				continue
			}
			idCounter++
			batch = append(batch, env.genEntry(rng, "e"+fmt.Sprintf("%08d", idCounter), drift))
		}
		if len(batch) > 0 {
			if errs := eng.Import(batch); len(errs) == 0 {
				for _, ent := range batch {
					model[ent.ID] = ent
					live = append(live, ent.ID)
				}
			}
		}
		// occasional deletes
		if len(live) > 10 && rng.Intn(3) == 0 {
			n := 1 + rng.Intn(10)
			var del []string
			for i := 0; i < n; i++ {
				idx := rng.Intn(len(live))
				del = append(del, live[idx])
				delete(model, live[idx])
				live = append(live[:idx], live[idx+1:]...)
			}
			eng.Delete(del)
		}
		// compare against brute force on random messages
		for m := 0; m < 12; m++ {
			msg := env.genMessage(rng, drift)
			gotRes, gotOK, err := eng.Match(msg)
			if err != nil {
				t.Fatalf("iter %d: Match error: %v", iter, err)
			}
			wantRes, wantOK := bruteMatch(model, msg)
			if gotOK != wantOK || gotRes != wantRes {
				t.Fatalf("iter %d msg %q: engine=(%+v,%v) brute=(%+v,%v)",
					iter, msg, gotRes, gotOK, wantRes, wantOK)
			}
		}
	}
}

func TestDifferential(t *testing.T) {
	for _, seed := range []int64{1, 2} {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			runDifferential(t, seed)
		})
	}
}
