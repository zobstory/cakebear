package runtime

import (
	"strings"
	"unicode/utf8"
)

// Percent-encoding and the application/x-www-form-urlencoded format, as the
// URL Standard gives them (https://url.spec.whatwg.org/#percent-encoded-bytes,
// #application/x-www-form-urlencoded), which Node 24.18's URL and
// URLSearchParams follow (S1 plan, R38, R42, R43).

// encodeSet is one of the URL Standard's percent-encode sets, as a bit: an
// ASCII code point is in a set when its entry in encodeTable has the bit.
// Every set holds every code point above U+007E.
type encodeSet uint8

const (
	setC0           encodeSet = 1 << iota // the C0 control percent-encode set
	setFragment                           // fragment
	setQuery                              // query
	setSpecialQuery                       // special-query
	setPath                               // path
	setUserinfo                           // userinfo
	setComponent                          // component
	setForm                               // application/x-www-form-urlencoded
)

// encodeTable is each ASCII code point's sets. Each set is another plus
// some code points, so a code point added to one is added to those built on
// it too.
var encodeTable = func() (t [128]encodeSet) {
	add := func(set encodeSet, chars string) {
		for i := range len(chars) {
			t[chars[i]] |= set
		}
	}
	for c := range 0x20 {
		t[c] |= setC0
	}
	t[0x7f] |= setC0
	for c := range t {
		if t[c]&setC0 != 0 {
			t[c] |= setFragment | setQuery
		}
	}
	add(setFragment, " \"<>`")
	add(setQuery, " \"#<>")
	for c := range t {
		if t[c]&setQuery != 0 {
			t[c] |= setSpecialQuery | setPath
		}
	}
	add(setSpecialQuery, "'")
	add(setPath, "?^`{}")
	for c := range t {
		if t[c]&setPath != 0 {
			t[c] |= setUserinfo
		}
	}
	add(setUserinfo, "/:;=@[\\]^|")
	for c := range t {
		if t[c]&setUserinfo != 0 {
			t[c] |= setComponent
		}
	}
	add(setComponent, "$%&+,")
	for c := range t {
		if t[c]&setComponent != 0 {
			t[c] |= setForm
		}
	}
	add(setForm, "!'()~")
	return t
}()

// inSet reports whether code point r is in set.
func inSet(r rune, set encodeSet) bool {
	return r >= utf8.RuneSelf || encodeTable[r]&set != 0
}

const upperHex = "0123456789ABCDEF"

// appendEncoded is the UTF-8 percent-encoding of r in set: r itself if it
// isn't in the set, and otherwise each of its UTF-8 bytes as %XX, in upper
// case.
func appendEncoded(b []byte, r rune, set encodeSet) []byte {
	if !inSet(r, set) {
		return utf8.AppendRune(b, r)
	}
	var buf [utf8.UTFMax]byte
	for _, c := range buf[:utf8.EncodeRune(buf[:], r)] {
		b = append(b, '%', upperHex[c>>4], upperHex[c&0xf])
	}
	return b
}

// encodeString is appendEncoded of each code point of s, a USVString.
func encodeString(s string, set encodeSet) string {
	i := 0
	for i < len(s) && !inSet(rune(s[i]), set) {
		i++
	}
	if i == len(s) {
		return s
	}
	b := make([]byte, 0, len(s)+8)
	b = append(b, s[:i]...)
	for _, r := range s[i:] {
		b = appendEncoded(b, r, set)
	}
	return string(b)
}

// usv is s as a USVString, which every URL and URLSearchParams operation
// converts its strings to (the IDL's conversion, and Node's toWellFormed):
// a lone surrogate becomes U+FFFD, and two halves of a pair the character
// they make, as wellFormed (str_output.go) writes them. A stray byte, which
// nothing the runtime makes holds, is a U+FFFD of its own, as char reads
// it.
func usv(s string) string {
	if strings.IndexByte(s, 0xed) < 0 && utf8.ValidString(s) {
		return s
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, size := char(s, i)
		i += size
		if r >= 0xd800 && r <= 0xdfff {
			if low, ok := surrogateAt(s, i); ok && r < 0xdc00 && low >= 0xdc00 {
				r = 0x10000 + (r-0xd800)<<10 + (low - 0xdc00)
				i += 3
			} else {
				r = utf8.RuneError
			}
		}
		b = utf8.AppendRune(b, r)
	}
	return string(b)
}

func isASCIIHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func hexValue(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c >= 'a':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// percentDecode is the URL Standard's percent-decode of bytes: each %XX is
// the byte it names, and a % not followed by two hex digits stays as it is.
func percentDecode(s string) []byte {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != '%' || i+2 >= len(s) || !isASCIIHex(s[i+1]) || !isASCIIHex(s[i+2]) {
			b = append(b, c)
			continue
		}
		b = append(b, hexValue(s[i+1])<<4|hexValue(s[i+2]))
		i += 2
	}
	return b
}

// utf8DecodeWithoutBOM is the Encoding Standard's UTF-8 decode without BOM:
// a leading BOM is kept, and each ill-formed sequence, as far as it is the
// start of a well-formed one, is one U+FFFD. Go's decoder makes a U+FFFD of
// every byte of a truncated sequence instead: "\xE6\x97" is one here and two
// there.
func utf8DecodeWithoutBOM(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	out := make([]byte, 0, len(b)+8)
	for i := 0; i < len(b); {
		c := b[i]
		if c < utf8.RuneSelf {
			out = append(out, c)
			i++
			continue
		}
		need, lo, hi := 0, byte(0x80), byte(0xbf)
		switch {
		case c >= 0xc2 && c <= 0xdf:
			need = 1
		case c >= 0xe0 && c <= 0xef:
			need = 2
			if c == 0xe0 {
				lo = 0xa0
			} else if c == 0xed {
				hi = 0x9f
			}
		case c >= 0xf0 && c <= 0xf4:
			need = 3
			if c == 0xf0 {
				lo = 0x90
			} else if c == 0xf4 {
				hi = 0x8f
			}
		default:
			out = utf8.AppendRune(out, utf8.RuneError)
			i++
			continue
		}
		j := i + 1
		for ; j <= i+need; j++ {
			if j >= len(b) || b[j] < lo || b[j] > hi {
				break
			}
			lo, hi = 0x80, 0xbf
		}
		if j == i+need+1 {
			out = append(out, b[i:j]...)
		} else {
			out = utf8.AppendRune(out, utf8.RuneError)
		}
		i = j
	}
	return string(out)
}

// paramPair is one name and value of a URLSearchParams' list.
type paramPair struct{ name, value string }

// parseForm is the application/x-www-form-urlencoded string parser: the
// input split on `&`, empty pieces dropped, each split at its first `=`
// (all name when it has none), `+` a space, then percent-decoded and
// decoded as UTF-8.
func parseForm(s string) []paramPair {
	var list []paramPair
	for len(s) > 0 {
		piece := s
		if i := strings.IndexByte(s, '&'); i >= 0 {
			piece, s = s[:i], s[i+1:]
		} else {
			s = ""
		}
		if piece == "" {
			continue
		}
		name, value, _ := strings.Cut(piece, "=")
		list = append(list, paramPair{formDecode(name), formDecode(value)})
	}
	return list
}

func formDecode(s string) string {
	if strings.IndexByte(s, '+') < 0 && strings.IndexByte(s, '%') < 0 {
		return s // a USVString, so already well-formed UTF-8
	}
	return utf8DecodeWithoutBOM(percentDecode(strings.ReplaceAll(s, "+", " ")))
}

// serializeForm is the application/x-www-form-urlencoded serializer: each
// pair as name=value, joined by `&`, each percent-encoded in the form's
// set, a space as `+`.
func serializeForm(list []paramPair) string {
	var b []byte
	for i, p := range list {
		if i > 0 {
			b = append(b, '&')
		}
		b = appendForm(b, p.name)
		b = append(b, '=')
		b = appendForm(b, p.value)
	}
	return string(b)
}

func appendForm(b []byte, s string) []byte {
	for i := 0; i < len(s); {
		if c := s[i]; c < utf8.RuneSelf {
			if c == ' ' {
				b = append(b, '+')
			} else {
				b = appendEncoded(b, rune(c), setForm)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b = appendEncoded(b, r, setForm)
		i += size
	}
	return b
}
