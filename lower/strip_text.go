package lower

import (
	"strings"
	"unicode/utf8"
)

// The characters stripping writes, in UTF-8: U+00A0, U+2002, U+FEFF, and the
// line terminators past ASCII, U+2028 and U+2029.
const (
	nbsp   = "\xC2\xA0"
	enSp   = "\xE2\x80\x82"
	zwnbsp = "\xEF\xBB\xBF"
	lsPs   = "\xE2\x80\xA8\xE2\x80\xA9"
)

// lineBreak reports whether text holds an ECMAScript line terminator.
func lineBreak(text string) bool {
	return strings.ContainsAny(text, "\n\r"+lsPs)
}

// lines applies the blanks and writes, and splits the text at the file's
// line starts. A BOM, which Node drops, goes too.
func (s *stripper) lines() []string {
	out := []byte(s.text)
	for _, b := range s.blanks {
		blankText(out, s.text, b[0], b[1])
	}
	for _, w := range s.writes {
		copy(out[w.pos:], w.text)
	}
	starts := s.file.ECMALineMap()
	lines := make([]string, len(starts))
	for i, start := range starts {
		end := len(out)
		if i+1 < len(starts) {
			end = terminatorStart(s.text, int(starts[i+1]))
		}
		lines[i] = string(out[start:end])
	}
	lines[0] = strings.TrimPrefix(lines[0], zwnbsp)
	return lines
}

// terminatorStart is where the line terminator ending at next starts.
func terminatorStart(text string, next int) int {
	switch text[next-1] {
	case '\n':
		if next >= 2 && text[next-2] == '\r' {
			return next - 2
		}
		return next - 1
	case '\r':
		return next - 1
	}
	return next - len(lsPs)/2 // U+2028 or U+2029
}

// blankText writes over text[start:end] in out as Node does. Whitespace and
// line terminators stay, and every other character becomes whitespace of the
// same UTF-8 length, which is also its UTF-16 length, so neither a byte nor a
// column moves: a space for one byte, U+00A0 for two, U+2002 for three, and
// a space and U+FEFF for four.
func blankText(out []byte, text string, start, end int) {
	for i := start; i < end; {
		r, size := utf8.DecodeRuneInString(text[i:])
		if !keptInBlank(r) {
			copy(out[i:], [...]string{" ", nbsp, enSp, " " + zwnbsp}[size-1])
		}
		i += size
	}
}

// keptInBlank is ECMAScript's whitespace and line terminators, which Node's
// blanking leaves as they are: tab, VT, FF, space, U+FEFF and the Unicode
// space separators, U+00A0, U+1680, U+2000 to U+200A, U+202F, U+205F and
// U+3000.
func keptInBlank(r rune) bool {
	switch r {
	case '\t', '\v', '\f', ' ', 0xA0, 0x1680, 0x202F, 0x205F, 0x3000, 0xFEFF, '\n', '\r', 0x2028, 0x2029:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}
