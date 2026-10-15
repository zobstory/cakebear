package runtime

import (
	"strings"
	"unicode/utf8"
)

// Strings in JSON, and the lone surrogates a string can hold.
//
// A JavaScript string is UTF-16 code units, any of them, so it can hold half
// a surrogate pair on its own. UTF-8 can't, and a Go string literal writes
// one, `"\uD800"`, as U+FFFD. The scanner keeps it instead as the three
// bytes UTF-8 would make of its code point (WTF-8), lowering takes that text
// as the literal's value, and the backend quotes it byte for byte, so that is
// how a lone surrogate reaches the runtime. JSON.stringify escapes one, so
// the encoder has to recognise it.

const hexDigits = "0123456789abcdef"

// appendJSONString is QuoteJSONString: s in double quotes, with `"`, `\`
// and the code units below U+0020 escaped, by their short forms where they
// have one and otherwise as \u00xx, and a lone surrogate as \udxxx. The hex
// is lower case. Everything else is written as it is, U+2028, U+2029, DEL
// and the C1 controls included.
//
// Two halves in a row, a high one then a low one, are a pair to JavaScript,
// however they met: `"\uD83D" + "\uDE00"` is "😀". So they are written as
// the character they make. A byte that is neither UTF-8 nor a half isn't a
// string the runtime makes; it is written as U+FFFD.
func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	start := 0 // s[start:i] is still to be written as it is
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			b = append(b, s[start:i]...)
			b = appendJSONEscape(b, c)
			i++
			start = i
			continue
		}
		unit, ok := surrogateAt(s, i)
		if ok {
			b = append(b, s[start:i]...)
			if low, ok := surrogateAt(s, i+3); ok && unit < 0xdc00 && low >= 0xdc00 {
				b = utf8.AppendRune(b, 0x10000+(unit-0xd800)<<10+(low-0xdc00))
				i += 6
			} else {
				b = append(b, '\\', 'u', 'd', hexDigits[unit>>8&0xf], hexDigits[unit>>4&0xf], hexDigits[unit&0xf])
				i += 3
			}
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b = append(b, s[start:i]...)
			b = utf8.AppendRune(b, utf8.RuneError)
			i++
			start = i
			continue
		}
		i += size
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}

// appendJSONEscape writes the escape for an ASCII byte that needs one.
func appendJSONEscape(b []byte, c byte) []byte {
	switch c {
	case '"':
		return append(b, '\\', '"')
	case '\\':
		return append(b, '\\', '\\')
	case '\b':
		return append(b, '\\', 'b')
	case '\f':
		return append(b, '\\', 'f')
	case '\n':
		return append(b, '\\', 'n')
	case '\r':
		return append(b, '\\', 'r')
	case '\t':
		return append(b, '\\', 't')
	}
	return append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
}

// surrogateAt reports whether s holds a lone surrogate's three bytes at i,
// and which code unit it is: U+D800 to U+DFFF, as UTF-8 would encode them
// if it could, which is 0xED then 0xA0–0xBF then a continuation byte.
func surrogateAt(s string, i int) (rune, bool) {
	if i+2 >= len(s) || s[i] != 0xed || s[i+1] < 0xa0 || s[i+1] > 0xbf || s[i+2]&0xc0 != 0x80 {
		return 0, false
	}
	return 0xd000 | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f), true
}

// JSONGapString is the gap for a string space: its first 10 UTF-16 code
// units, or none for "". A cut through a pair keeps its high half, a lone
// surrogate, which JSON.stringify writes into the output as it is, gaps
// being unescaped.
//
// V8 writes the gap as a C string, so it ends at a NUL: under Node, a space
// of "a\x00b" indents by "a", and one of "\x00" by nothing, though each entry
// still has a line of its own.
func JSONGapString(space string) JSONGap {
	if space == "" {
		return JSONGap{}
	}
	indent := firstUnits(space, 10)
	if i := strings.IndexByte(indent, 0); i >= 0 {
		indent = indent[:i]
	}
	return JSONGap{indent: indent, lines: true}
}

// firstUnits is s.slice(0, n), in UTF-16 code units.
func firstUnits(s string, n int) string {
	units := 0
	for i := 0; i < len(s); {
		if units == n {
			return s[:i]
		}
		size := 3
		if _, ok := surrogateAt(s, i); !ok {
			var r rune
			r, size = utf8.DecodeRuneInString(s[i:])
			if r >= 0x10000 {
				if units == n-1 {
					high := 0xd800 + (r-0x10000)>>10
					return s[:i] + string([]byte{0xed, byte(0x80 | high>>6&0x3f), byte(0x80 | high&0x3f)})
				}
				units++
			}
		}
		units++
		i += size
	}
	return s
}
