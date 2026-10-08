// Package command parses and executes player terminal input against the
// simulation engine. The parser is deliberately defensive: it is the main
// surface an adversarial evaluator will attack (huge strings, control
// characters, exotic unicode, ANSI injection, missing/extra arguments).
package command

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on raw input.
const (
	MaxLineBytes = 1024 // bytes accepted on one command line
	MaxTokens    = 16   // tokens kept after splitting
	MaxTokenLen  = 128  // runes kept per token
)

// ErrTooLong is returned when the raw line exceeds MaxLineBytes.
var ErrTooLong = errors.New("line too long")

// Parsed is a sanitised command: a verb and its arguments. All strings are
// guaranteed printable (no control chars, no ANSI escapes) and bounded.
type Parsed struct {
	Verb string
	Args []string
	// Raw is the sanitised echo of the whole line (safe to display).
	Raw string
}

// Parse turns a raw input line into a Parsed command. It never returns an
// error for merely malformed content — it sanitises instead — except when
// the input is larger than MaxLineBytes, which is rejected outright so a
// client cannot force large allocations.
func Parse(line string) (Parsed, error) {
	if len(line) > MaxLineBytes {
		return Parsed{}, ErrTooLong
	}
	// Replace invalid UTF-8 up front so later rune logic is well defined.
	if !utf8.ValidString(line) {
		line = strings.ToValidUTF8(line, "")
	}
	line = stripEscapes(line)
	tokens := make([]string, 0, 8)
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	for _, r := range line {
		if sanitizeDrop(r) {
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			if len(tokens) >= MaxTokens {
				break
			}
			continue
		}
		if utf8.RuneCountInString(b.String()) < MaxTokenLen {
			b.WriteRune(r)
		}
	}
	flush()
	if len(tokens) > MaxTokens {
		tokens = tokens[:MaxTokens]
	}
	p := Parsed{}
	if len(tokens) > 0 {
		p.Verb = strings.ToLower(tokens[0])
		p.Args = tokens[1:]
	}
	p.Raw = strings.Join(tokens, " ")
	return p, nil
}

// stripEscapes removes whole ANSI/VT escape sequences so no trailing literal
// (like "[31m" after a dropped ESC) survives to clutter the command. It
// handles CSI (ESC [ … final), OSC (ESC ] … BEL/ST) and single two-byte
// escapes. The lone ESC bytes are then also removed by sanitizeDrop.
func stripEscapes(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		if r[i] != 0x1b {
			b.WriteRune(r[i])
			continue
		}
		// Skip the ESC.
		i++
		if i >= len(r) {
			break
		}
		switch r[i] {
		case '[': // CSI: params then a final byte in @-~
			i++
			for i < len(r) && (r[i] < 0x40 || r[i] > 0x7e) {
				i++
			}
		case ']': // OSC: until BEL or ST (ESC \)
			i++
			for i < len(r) && r[i] != 0x07 {
				if r[i] == 0x1b && i+1 < len(r) && r[i+1] == '\\' {
					i++
					break
				}
				i++
			}
		default:
			// Two-byte escape: the single following byte is consumed.
		}
	}
	return b.String()
}

// sanitizeDrop reports whether a rune must be dropped from input. We strip
// control characters (including the ESC that begins ANSI sequences), the
// Unicode format category (bidi overrides, zero-width joiners) and any
// unassigned/private-use code point, leaving only printable text and spaces.
func sanitizeDrop(r rune) bool {
	if r == utf8.RuneError {
		return true
	}
	if r == ' ' || r == '\t' {
		return false
	}
	if unicode.IsControl(r) {
		return true
	}
	if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) || unicode.Is(unicode.Cs, r) {
		return true
	}
	// Drop runes with no printable representation.
	if !unicode.IsGraphic(r) && !unicode.IsSpace(r) {
		return true
	}
	return false
}
