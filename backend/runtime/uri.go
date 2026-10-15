package runtime

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// EncodeURIComponent is encodeURIComponent(s): every code point but
// A–Z a–z 0–9 - _ . ! ~ * ' ( ) as %XX of each of its UTF-8 bytes, in upper
// case. A string's bytes are its UTF-8 already, so they are escaped as they
// are, but for two halves of a pair, which JavaScript reads as the one code
// point they make. A lone surrogate has no UTF-8, and panics where Node
// throws a URIError.
//
// A byte that is neither UTF-8 nor a surrogate's three bytes has no
// JavaScript meaning, and nothing lowered makes one; it is U+FFFD, as Go
// decodes it.
func EncodeURIComponent(s string) string {
	i := 0
	for i < len(s) && uriUnreserved(s[i]) {
		i++
	}
	if i == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 2*(len(s)-i))
	b.WriteString(s[:i])
	for i < len(s) {
		c := s[i]
		switch {
		case uriUnreserved(c):
			b.WriteByte(c)
			i++
			continue
		case c < utf8.RuneSelf:
			uriEscape(&b, c)
			i++
			continue
		}
		if u, ok := surrogateAt(s, i); ok {
			low, pair := surrogateAt(s, i+3)
			if u >= 0xdc00 || !pair || low < 0xdc00 {
				panic("cakebear: encodeURIComponent cannot encode the lone surrogate U+" + strings.ToUpper(strconv.FormatInt(int64(u), 16)) + " (Node throws URIError: URI malformed)")
			}
			var buf [4]byte
			for _, c := range buf[:utf8.EncodeRune(buf[:], 0x10000+(u-0xd800)<<10+(low-0xdc00))] {
				uriEscape(&b, c)
			}
			i += 6
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		var buf [4]byte
		for _, c := range buf[:utf8.EncodeRune(buf[:], r)] {
			uriEscape(&b, c)
		}
		i += size
	}
	return b.String()
}

// uriUnreserved is whether encodeURIComponent leaves c as it is.
func uriUnreserved(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')':
		return true
	}
	return false
}

const uriHex = "0123456789ABCDEF"

func uriEscape(b *strings.Builder, c byte) {
	b.WriteByte('%')
	b.WriteByte(uriHex[c>>4])
	b.WriteByte(uriHex[c&0xf])
}

// DecodeURIComponent is decodeURIComponent(s): each %XX, in either case,
// is a byte, and each run of them a byte of ASCII or one UTF-8 sequence,
// which must be complete and valid: no overlong form, no surrogate and
// nothing past U+10FFFF. Anything else panics where Node throws a URIError.
// Everything but an escape is kept as it is, a lone surrogate included.
// Nothing is reserved, so %25 is "%" and %2F is "/".
func DecodeURIComponent(s string) string {
	i := strings.IndexByte(s, '%')
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i >= 0 {
		b.WriteString(s[:i])
		s = s[i:]
		lead, ok := uriOctet(s, 0)
		if !ok {
			uriMalformed(s, 3)
		}
		// The bytes of the sequence lead begins. A continuation byte, or one
		// no UTF-8 starts with, is one byte alone, which isn't valid either.
		n := 1
		switch {
		case lead&0xe0 == 0xc0:
			n = 2
		case lead&0xf0 == 0xe0:
			n = 3
		case lead&0xf8 == 0xf0:
			n = 4
		}
		var seq [4]byte
		seq[0] = lead
		for j := 1; j < n; j++ {
			c, ok := uriOctet(s, 3*j)
			if !ok {
				uriMalformed(s, 3*j+3)
			}
			seq[j] = c
		}
		if !utf8.Valid(seq[:n]) {
			uriMalformed(s, 3*n)
		}
		b.Write(seq[:n])
		s = s[3*n:]
		i = strings.IndexByte(s, '%')
	}
	b.WriteString(s)
	return b.String()
}

// uriOctet is the byte the escape %XX at s[i:] stands for, if there is one.
func uriOctet(s string, i int) (byte, bool) {
	if i+2 >= len(s) || s[i] != '%' {
		return 0, false
	}
	hi, lo := numDigit(s[i+1]), numDigit(s[i+2])
	if hi >= 16 || lo >= 16 {
		return 0, false
	}
	return byte(hi<<4 | lo), true
}

// uriMalformed panics for the escapes at the start of s, which it quotes
// up to its first n bytes, and the rest of the character the cut is in.
func uriMalformed(s string, n int) {
	for n < len(s) && !utf8.RuneStart(s[n]) {
		n++
	}
	panic("cakebear: decodeURIComponent cannot decode " + strconv.Quote(s[:min(n, len(s))]) + " (Node throws URIError: URI malformed)")
}
