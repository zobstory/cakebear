package runtime

import (
	"strings"
	"unicode/utf8"
)

// Strings on their way out of the program, to stdout or over HTTP, where
// Node encodes them as UTF-8.

// wellFormed is s as Node writes a string to a stream or a response body:
// UTF-8, each lone surrogate as U+FFFD, and two halves that pair as the
// character they make. A string with no 0xED byte holds no half, which is
// nearly every string, and is returned as it is.
func wellFormed(s string) string {
	i := strings.IndexByte(s, 0xed)
	if i < 0 {
		return s
	}
	var b []byte
	start := 0
	for i < len(s) {
		unit, ok := surrogateAt(s, i)
		if !ok {
			i++
			continue
		}
		if b == nil {
			b = make([]byte, 0, len(s))
		}
		b = append(b, s[start:i]...)
		if low, ok := surrogateAt(s, i+3); ok && unit < 0xdc00 && low >= 0xdc00 {
			b = utf8.AppendRune(b, 0x10000+(unit-0xd800)<<10+(low-0xdc00))
			i += 6
		} else {
			b = utf8.AppendRune(b, utf8.RuneError)
			i += 3
		}
		start = i
	}
	if b == nil {
		return s
	}
	return string(append(b, s[start:]...))
}

// isHTTPToken is Node's checkIsHttpToken, which a header name must pass:
// one or more of the letters, the digits and !#$%&'*+-.^_`|~.
func isHTTPToken(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// validHeaderValue is Node's checkInvalidHeaderChar, inverted: every code
// unit is a tab, printable ASCII, or from U+0080 to U+00FF. Node sends those
// as UTF-8, as net/http does. Anything else, a lone surrogate or a character
// past U+00FF included, is a TypeError.
func validHeaderValue(value string) bool {
	for i := 0; i < len(value); {
		r, size := char(value, i)
		if r != '\t' && (r < 0x20 || r == 0x7f || r > 0xff) {
			return false
		}
		i += size
	}
	return true
}
