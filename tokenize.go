package kbengine

import "unicode"

// tokenize splits text into tokens per the shared tokenization rules:
// fold full-width ASCII (U+FF01..U+FF5E) to half-width, lowercase each
// character, then take maximal runs of Unicode L/M/Nd characters, with each
// Han ideograph as its own single-character token.
func tokenize(text string) []string {
	var tokens []string
	var run []rune
	flush := func() {
		if len(run) > 0 {
			tokens = append(tokens, string(run))
			run = run[:0]
		}
	}
	for _, r := range text {
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		r = unicode.ToLower(r)
		switch {
		case unicode.Is(unicode.Han, r):
			flush()
			tokens = append(tokens, string(r))
		case unicode.IsLetter(r) || unicode.Is(unicode.M, r) || unicode.IsDigit(r):
			run = append(run, r)
		default:
			flush()
		}
	}
	flush()
	return tokens
}
