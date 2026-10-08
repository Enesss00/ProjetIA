package command

import (
	"strings"
	"testing"
)

func TestParseStripsControlAndANSI(t *testing.T) {
	cases := []struct {
		in   string
		verb string
	}{
		{"status", "status"},
		{"  SCAN   net  ", "scan"},
		{"\x1b[31mstatus\x1b[0m", "status"}, // ANSI injection
		{"sta\x00tus", "status"},            // NUL
		{"scan\u202eweb", "scanweb"},        // bidi override removed
		{"\t\tlogs\t--tail", "logs"},        // tabs
		{"héllo", "héllo"},                  // legit unicode kept
		{"", ""},                            // empty
		{"   ", ""},                         // whitespace only
	}
	for _, c := range cases {
		p, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q) err: %v", c.in, err)
		}
		if p.Verb != c.verb {
			t.Errorf("Parse(%q).Verb = %q, want %q", c.in, p.Verb, c.verb)
		}
		if strings.ContainsRune(p.Raw, '\x1b') || strings.ContainsRune(p.Raw, 0) {
			t.Errorf("Parse(%q).Raw still has control chars: %q", c.in, p.Raw)
		}
	}
}

func TestParseRejectsHugeLine(t *testing.T) {
	_, err := Parse(strings.Repeat("a", MaxLineBytes+1))
	if err != ErrTooLong {
		t.Fatalf("expected ErrTooLong, got %v", err)
	}
}

func TestParseBoundsTokens(t *testing.T) {
	line := strings.Repeat("x ", 100)
	p, _ := Parse(line)
	if len(p.Args)+1 > MaxTokens {
		t.Fatalf("tokens not bounded: %d", len(p.Args)+1)
	}
	long := strings.Repeat("z", 1000)
	p2, _ := Parse("scan " + long)
	if n := len([]rune(p2.Args[0])); n > MaxTokenLen {
		t.Fatalf("token length not bounded: %d", n)
	}
}
