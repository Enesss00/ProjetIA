package command

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzParse throws arbitrary bytes at the command parser and asserts the
// invariants that keep the terminal safe: it never panics, always returns
// valid UTF-8 with no control/escape characters, and respects the token and
// length bounds.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"status",
		"scan net",
		"\x1b[31mred\x1b[0m",
		"isolate ws-01 extra args here and more and more",
		strings.Repeat("x", 5000),
		"patch\x00web-01\x07ssh",
		"h\u00e9llo \u202e world \ufeff",
		"   \t\t  ",
		"block 999.999.999.999",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		p, err := Parse(in)
		if err == ErrTooLong {
			if len(in) <= MaxLineBytes {
				t.Fatalf("ErrTooLong for in-bounds input of %d bytes", len(in))
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !utf8.ValidString(p.Raw) {
			t.Fatalf("Raw is not valid UTF-8")
		}
		for _, r := range p.Raw {
			if r == 0x1b || (r < 0x20 && r != ' ' && r != '\t') || r == 0x7f {
				t.Fatalf("Raw contains control/escape rune %U", r)
			}
		}
		if 1+len(p.Args) > MaxTokens {
			t.Fatalf("too many tokens: %d", 1+len(p.Args))
		}
		for _, a := range p.Args {
			if utf8.RuneCountInString(a) > MaxTokenLen {
				t.Fatalf("token too long: %d runes", utf8.RuneCountInString(a))
			}
		}
	})
}
