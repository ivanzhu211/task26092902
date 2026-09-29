package kbengine

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Term is a single weighted term of an entry.
type Term struct {
	Text   string
	Weight int
}

// Entry is one knowledge base record.
type Entry struct {
	ID        string
	Answer    string
	Threshold string
	Terms     []Term
}

// RowError describes one validation failure in an Import batch.
type RowError struct {
	Line   int // 0-based index within the batch; -1 for batch-level errors
	Field  string
	Reason string
}

// MatchResult is the outcome of a Match call.
type MatchResult struct {
	ID          string
	HitWeight   int
	TotalWeight int
	// Found is true if a qualifying entry was found.
	Found bool
}

const (
	maxBatchSize   = 1000
	maxTerms       = 20
	maxTermTokens  = 8
	maxWeight      = 100
	maxTextRunes   = 2000
	minEffectiveTh = 60 // max(0.6, threshold) in hundredths
)

// idxTerm is a tokenized term: its key joins tokens with NUL.
type idxTerm struct {
	key    string
	weight int
}

// entry is the internal, fully parsed form of an Entry.
type entry struct {
	id          string
	answer      string
	terms       []idxTerm // all terms, tokenized
	prefix      []string  // rare-first prefix term keys present in the index
	totalWeight int
	reqHit      int // minimal hit weight that qualifies: ceil(m*total/100)
}

// KB is an in-memory knowledge base safe for concurrent use.
//
// The inverted index maps term keys to posting sets, but only the rare-first
// "prefix" terms of each entry are indexed: the prefix is the minimal set of
// an entry's rarest terms whose removal leaves a suffix weight below the
// entry's required hit weight. Any qualifying entry must match at least one
// prefix term, so Match only needs to verify candidates gathered from posting
// lists of terms present in the message; entries that merely contain a
// high-frequency term but cannot qualify are never touched.
type KB struct {
	mu    sync.RWMutex
	byID  map[string]*entry
	df    map[string]int                 // term key -> number of entries containing it
	index map[string]map[*entry]struct{} // term key -> posting set (prefix terms only)
}

// New returns an empty knowledge base.
func New() *KB {
	return &KB{
		byID:  make(map[string]*entry),
		df:    make(map[string]int),
		index: make(map[string]map[*entry]struct{}),
	}
}

func termKey(tokens []string) string { return strings.Join(tokens, "\x00") }

// validateEntry checks one entry and returns its parsed form on success.
func validateEntry(line int, e Entry) (*entry, []RowError) {
	var errs []RowError
	add := func(field, reason string) {
		errs = append(errs, RowError{Line: line, Field: field, Reason: reason})
	}
	if e.ID == "" {
		add("id", "id must be a non-empty string")
	}
	th, err := parseThreshold(e.Threshold)
	if err != nil {
		add("threshold", err.Error())
	}
	if len(e.Terms) == 0 || len(e.Terms) > maxTerms {
		add("terms", fmt.Sprintf("terms count %d out of range 1..%d", len(e.Terms), maxTerms))
	}
	terms := make([]idxTerm, 0, len(e.Terms))
	seen := make(map[string]int, len(e.Terms))
	total := 0
	for i, t := range e.Terms {
		if t.Weight < 1 || t.Weight > maxWeight {
			add(fmt.Sprintf("terms[%d].weight", i),
				fmt.Sprintf("weight %d out of range 1..%d", t.Weight, maxWeight))
		}
		toks := tokenize(t.Text)
		if len(toks) == 0 {
			add(fmt.Sprintf("terms[%d].text", i), "term tokenizes to zero tokens")
			continue
		}
		if len(toks) > maxTermTokens {
			add(fmt.Sprintf("terms[%d].text", i),
				fmt.Sprintf("term has %d tokens, max %d", len(toks), maxTermTokens))
			continue
		}
		key := termKey(toks)
		if prev, dup := seen[key]; dup {
			add(fmt.Sprintf("terms[%d].text", i),
				fmt.Sprintf("duplicate of terms[%d] after tokenization", prev))
			continue
		}
		seen[key] = i
		terms = append(terms, idxTerm{key: key, weight: t.Weight})
		total += t.Weight
	}
	if len(errs) > 0 {
		return nil, errs
	}
	m := th
	if m < minEffectiveTh {
		m = minEffectiveTh
	}
	return &entry{
		id:          e.ID,
		answer:      e.Answer,
		terms:       terms,
		totalWeight: total,
		reqHit:      (m*total + 99) / 100,
	}, nil
}

// Import validates and atomically applies a batch of entries (add or replace
// by ID). On any validation error the whole batch is rejected and all row
// errors are returned.
func (kb *KB) Import(entries []Entry) []RowError {
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > maxBatchSize {
		return []RowError{{Line: -1, Field: "batch",
			Reason: fmt.Sprintf("batch size %d exceeds %d", len(entries), maxBatchSize)}}
	}
	parsed := make([]*entry, len(entries))
	var errs []RowError
	seenID := make(map[string]int, len(entries))
	for i, e := range entries {
		if prev, dup := seenID[e.ID]; dup && e.ID != "" {
			errs = append(errs, RowError{Line: i, Field: "id",
				Reason: fmt.Sprintf("duplicate id in batch, first at line %d", prev)})
			continue
		}
		seenID[e.ID] = i
		p, rowErrs := validateEntry(i, e)
		if len(rowErrs) > 0 {
			errs = append(errs, rowErrs...)
			continue
		}
		parsed[i] = p
	}
	if len(errs) > 0 {
		return errs
	}

	kb.mu.Lock()
	defer kb.mu.Unlock()

	// Remove entries being replaced so df reflects the pre-batch state.
	for _, p := range parsed {
		if old, ok := kb.byID[p.id]; ok {
			kb.removeLocked(old)
		}
	}
	// Effective document frequencies include this batch, so that a term
	// becoming frequent right now is already ordered as frequent.
	batchDf := make(map[string]int, len(parsed))
	for _, p := range parsed {
		for _, t := range p.terms {
			batchDf[t.key]++
		}
	}
	for _, p := range parsed {
		p.prefix = choosePrefix(p.terms, p.reqHit, func(key string) int {
			return kb.df[key] + batchDf[key]
		})
		kb.addLocked(p)
	}
	return nil
}

// choosePrefix picks the minimal rare-first prefix whose suffix weight is
// below reqHit. Terms are ordered by ascending effective document frequency.
func choosePrefix(terms []idxTerm, reqHit int, effDf func(string) int) []string {
	order := make([]int, len(terms))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		ka, kb := terms[order[a]].key, terms[order[b]].key
		da, db := effDf(ka), effDf(kb)
		if da != db {
			return da < db
		}
		return ka < kb
	})
	suffix := 0
	for _, i := range order {
		suffix += terms[i].weight
	}
	prefix := make([]string, 0, len(terms))
	for _, i := range order {
		if suffix < reqHit {
			break
		}
		prefix = append(prefix, terms[i].key)
		suffix -= terms[i].weight
	}
	return prefix
}

// addLocked inserts an entry into byID, df and the prefix index.
func (kb *KB) addLocked(e *entry) {
	kb.byID[e.id] = e
	for _, t := range e.terms {
		kb.df[t.key]++
	}
	for _, key := range e.prefix {
		pl, ok := kb.index[key]
		if !ok {
			pl = make(map[*entry]struct{})
			kb.index[key] = pl
		}
		pl[e] = struct{}{}
	}
}

// removeLocked deletes an entry from byID, df and the prefix index.
func (kb *KB) removeLocked(e *entry) {
	delete(kb.byID, e.id)
	for _, t := range e.terms {
		if kb.df[t.key]--; kb.df[t.key] <= 0 {
			delete(kb.df, t.key)
		}
	}
	for _, key := range e.prefix {
		if pl, ok := kb.index[key]; ok {
			delete(pl, e)
			if len(pl) == 0 {
				delete(kb.index, key)
			}
		}
	}
}

// Delete removes the given IDs, ignoring unknown ones.
func (kb *KB) Delete(ids []string) {
	if len(ids) == 0 {
		return
	}
	kb.mu.Lock()
	defer kb.mu.Unlock()
	for _, id := range ids {
		if e, ok := kb.byID[id]; ok {
			kb.removeLocked(e)
		}
	}
}

// Match finds the highest-scoring entry at or above its effective threshold.
// Score comparisons use exact integer arithmetic; ties go to the bytewise
// smallest ID.
func (kb *KB) Match(text string) (MatchResult, error) {
	if utf8.RuneCountInString(text) > maxTextRunes {
		return MatchResult{}, fmt.Errorf("text exceeds %d runes", maxTextRunes)
	}
	tokens := tokenize(text)
	ngrams := make(map[string]struct{}, len(tokens)*4)
	for n := 1; n <= maxTermTokens && n <= len(tokens); n++ {
		for i := 0; i+n <= len(tokens); i++ {
			ngrams[termKey(tokens[i:i+n])] = struct{}{}
		}
	}

	kb.mu.RLock()
	defer kb.mu.RUnlock()

	candidates := make(map[*entry]struct{})
	for key := range ngrams {
		if pl, ok := kb.index[key]; ok {
			for e := range pl {
				candidates[e] = struct{}{}
			}
		}
	}
	var best *entry
	bestHit := 0
	for e := range candidates {
		hit := 0
		for _, t := range e.terms {
			if _, ok := ngrams[t.key]; ok {
				hit += t.weight
			}
		}
		if hit < e.reqHit {
			continue
		}
		if best == nil || better(hit, e, bestHit, best) {
			best, bestHit = e, hit
		}
	}
	if best == nil {
		return MatchResult{}, nil
	}
	return MatchResult{
		ID:          best.id,
		HitWeight:   bestHit,
		TotalWeight: best.totalWeight,
		Found:       true,
	}, nil
}

// better reports whether candidate (hitA, a) beats (hitB, b): higher exact
// score, or equal score with bytewise smaller id.
func better(hitA int, a *entry, hitB int, b *entry) bool {
	lhs := int64(hitA) * int64(b.totalWeight)
	rhs := int64(hitB) * int64(a.totalWeight)
	if lhs != rhs {
		return lhs > rhs
	}
	return a.id < b.id
}
