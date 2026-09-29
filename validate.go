package kbmatch

import (
	"fmt"
	"strings"
)

// parseThreshold parses a decimal threshold string with at most two
// fractional digits and value in [0.00, 1.00] into hundredths (0..100).
func parseThreshold(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	intPart, fracPart, hasFrac := s, "", false
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart, hasFrac = s[:i], s[i+1:], true
	}
	if intPart == "" || !allDigits(intPart) {
		return 0, false
	}
	if hasFrac && (len(fracPart) == 0 || len(fracPart) > 2 || !allDigits(fracPart)) {
		return 0, false
	}
	value := 0
	for _, c := range intPart {
		value = value*10 + int(c-'0')
		if value > 1 {
			return 0, false
		}
	}
	hundredths := value * 100
	if hasFrac {
		frac := 0
		for _, c := range fracPart {
			frac = frac*10 + int(c-'0')
		}
		if len(fracPart) == 1 {
			frac *= 10
		}
		hundredths += frac
	}
	if hundredths > 100 {
		return 0, false
	}
	return hundredths, true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// termKey joins a token sequence into a unique map key.
func termKey(tokens []string) string {
	return strings.Join(tokens, "\x00")
}

// validatedEntry is an Entry after successful validation.
type validatedEntry struct {
	id    string
	total int
	need  int
	terms []validatedTerm
}

type validatedTerm struct {
	key    string
	weight int
}

// validateEntry checks a single entry and returns its normalized form.
// seenIDs holds ids already present earlier in the same batch.
func validateEntry(src *Entry, line int, seenIDs map[string]bool, errs *[]RowError) *validatedEntry {
	fail := func(field, reason string) {
		*errs = append(*errs, RowError{Line: line, Field: field, Reason: reason})
	}
	ok := true
	if src.ID == "" {
		fail("id", "empty id")
		ok = false
	} else if seenIDs[src.ID] {
		fail("id", "duplicate id in batch")
		ok = false
	}
	thr, thrOK := parseThreshold(src.Threshold)
	if !thrOK {
		fail("threshold", "invalid threshold")
		ok = false
	}
	if len(src.Terms) < 1 || len(src.Terms) > 20 {
		fail("terms", "term count out of range [1,20]")
		ok = false
	}
	ve := &validatedEntry{id: src.ID}
	seenTerms := make(map[string]bool, len(src.Terms))
	for i, t := range src.Terms {
		field := fmt.Sprintf("terms[%d]", i)
		if t.Weight < 1 || t.Weight > 100 {
			fail(field+".weight", "weight out of range [1,100]")
			ok = false
		}
		tokens := tokenize(t.Text)
		if len(tokens) == 0 {
			fail(field, "term has no tokens")
			ok = false
			continue
		}
		if len(tokens) > 8 {
			fail(field, "term exceeds 8 tokens")
			ok = false
			continue
		}
		key := termKey(tokens)
		if seenTerms[key] {
			fail(field, "duplicate term")
			ok = false
			continue
		}
		seenTerms[key] = true
		ve.terms = append(ve.terms, validatedTerm{key: key, weight: t.Weight})
		ve.total += t.Weight
	}
	if !ok {
		return nil
	}
	m := 60
	if thr > m {
		m = thr
	}
	ve.need = (ve.total*m + 99) / 100
	return ve
}
