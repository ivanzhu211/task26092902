package kbmatch

// bruteMatch is the reference implementation: it evaluates every entry
// exactly, per the spec, and returns the best passing one.
func bruteMatch(model map[string]Entry, text string) (Result, bool) {
	tokens := tokenize(text)
	var best Result
	found := false
	for id, e := range model {
		thr, ok := parseThreshold(e.Threshold)
		if !ok {
			continue
		}
		m := 60
		if thr > m {
			m = thr
		}
		total := 0
		hit := 0
		for _, t := range e.Terms {
			total += t.Weight
			if containsSeq(tokens, tokenize(t.Text)) {
				hit += t.Weight
			}
		}
		need := (total*m + 99) / 100
		if hit < need {
			continue
		}
		if !found || hit*best.TotalWeight > best.HitWeight*total ||
			(hit*best.TotalWeight == best.HitWeight*total && id < best.ID) {
			best = Result{ID: id, HitWeight: hit, TotalWeight: total}
			found = true
		}
	}
	return best, found
}

// containsSeq reports whether sub appears contiguously in tokens.
func containsSeq(tokens, sub []string) bool {
	if len(sub) == 0 || len(sub) > len(tokens) {
		return false
	}
	for i := 0; i+len(sub) <= len(tokens); i++ {
		match := true
		for j := range sub {
			if tokens[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
