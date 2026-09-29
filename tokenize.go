package kbmatch

import "unicode"

// tokenize splits s into tokens per the shared tokenization rules:
// fullwidth ASCII (U+FF01..U+FF5E) is folded to halfwidth, every character
// is lowercased, and a token is a maximal run of Unicode L, M or Nd
// characters, except that each Han ideograph forms its own token.
func tokenize(s string) []string {
	var tokens []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			tokens = append(tokens, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		r = unicode.ToLower(r)
		switch {
		case unicode.Is(unicode.Han, r):
			flush()
			tokens = append(tokens, string(r))
		case unicode.IsLetter(r) || unicode.Is(unicode.M, r) || unicode.Is(unicode.Nd, r):
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return tokens
}
