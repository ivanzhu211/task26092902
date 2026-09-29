package kbengine

import "fmt"

// parseThreshold parses a decimal string with at most two fraction digits in
// the range 0.00..1.00 and returns it as integer hundredths (0..100).
// Accepted forms: "0", "0.6", "0.60", "1", "1.0", "1.00".
func parseThreshold(s string) (int, error) {
	err := fmt.Errorf("invalid threshold %q: want a decimal in 0.00..1.00 with at most two fraction digits", s)
	if len(s) == 0 || len(s) > 4 {
		return 0, err
	}
	intPart := s
	fracPart := ""
	hasDot := false
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			intPart = s[:i]
			fracPart = s[i+1:]
			hasDot = true
			break
		}
	}
	if hasDot && len(fracPart) == 0 {
		return 0, err
	}
	if len(intPart) != 1 || intPart[0] < '0' || intPart[0] > '9' {
		return 0, err
	}
	if len(fracPart) > 2 {
		return 0, err
	}
	value := int(intPart[0]-'0') * 100
	for i := 0; i < len(fracPart); i++ {
		c := fracPart[i]
		if c < '0' || c > '9' {
			return 0, err
		}
		if i == 0 {
			value += int(c-'0') * 10
		} else {
			value += int(c - '0')
		}
	}
	if value > 100 {
		return 0, err
	}
	return value, nil
}
