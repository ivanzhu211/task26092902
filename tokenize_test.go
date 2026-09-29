package kbengine

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  \t\n，。！", nil},
		{"hello", []string{"hello"}},
		{"Hello World", []string{"hello", "world"}},
		{"ＡＢＣ１２３", []string{"abc123"}}, // full-width folded, L+Nd one run
		{"Ｈｅｌｌｏ，Ｗｏｒｌｄ！", []string{"hello", "world"}}, // full-width punct is separator
		{"你好", []string{"你", "好"}},                   // each Han char its own token
		{"abc你def", []string{"abc", "你", "def"}},
		{"请问一下", []string{"请", "问", "一", "下"}},
		{"go语言1.5版本", []string{"go", "语", "言", "1", "5", "版", "本"}},
		{"éclair", []string{"éclair"}}, // combining mark (M) joins run
		{"カタカナ", []string{"カタカナ"}},       // kana: L, one run
		{"user_123", []string{"user", "123"}},
		{"ßẞ", []string{"ßß"}}, // lowercase of ẞ is ß
	}
	for _, c := range cases {
		got := tokenize(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("tokenize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseThreshold(t *testing.T) {
	ok := map[string]int{
		"0": 0, "0.0": 0, "0.00": 0,
		"0.6": 60, "0.60": 60, "0.06": 6,
		"0.75": 75, "0.99": 99,
		"1": 100, "1.0": 100, "1.00": 100,
	}
	for in, want := range ok {
		got, err := parseThreshold(in)
		if err != nil || got != want {
			t.Errorf("parseThreshold(%q) = %d, %v; want %d, nil", in, got, err, want)
		}
	}
	bad := []string{"", ".", ".5", "0.", "0.555", "1.01", "1.5", "2", "-0.5",
		"00.5", "abc", "0,5", " 0.5", "0.5 ", "10", "0.123"}
	for _, in := range bad {
		if v, err := parseThreshold(in); err == nil {
			t.Errorf("parseThreshold(%q) = %d, nil; want error", in, v)
		}
	}
}
