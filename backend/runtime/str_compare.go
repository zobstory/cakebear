package runtime

import (
	"cmp"
	"unicode/utf8"
)

// StrCompare is the order of `<`, `>`, `<=` and `>=` on strings: -1, 0 or 1
// as a is before, equal to or after b in UTF-16 code unit order (M4 strings
// plan S7).
//
// For two canonical strings, byte order is code point order, and that is
// code unit order except where a character outside the BMP, whose first
// unit is a high half from U+D800, meets one from U+E000 to U+FFFF:
// "\uFFFF" < "😀" is false in JavaScript, and its bytes say otherwise. A
// lone half meeting a pair that starts with the same half differs too. So
// past the bytes the two share, the strings are compared a unit at a time,
// which the first unit nearly always settles.
func StrCompare(a, b string) int {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	if i == n {
		return cmp.Compare(len(a), len(b)) // one is a prefix of the other
	}
	if a[i] < utf8.RuneSelf || b[i] < utf8.RuneSelf {
		return cmp.Compare(int(a[i]), int(b[i]))
	}
	// Back up to the start of the character the bytes differ in, which the
	// shared bytes make the same place in both.
	for k := 0; k < utf8.UTFMax-1 && i > 0 && !utf8.RuneStart(a[i]); k++ {
		i--
	}
	x, y := unitReader{s: a, i: i}, unitReader{s: b, i: i}
	for {
		u, okA := x.next()
		v, okB := y.next()
		switch {
		case !okA || !okB:
			return cmp.Compare(boolInt(okA), boolInt(okB))
		case u != v:
			return cmp.Compare(int(u), int(v))
		}
	}
}

// A unitReader reads a string's code units in order from byte i.
type unitReader struct {
	s   string
	i   int
	low uint16 // the low half still to come of a pair just read, or 0
}

func (r *unitReader) next() (uint16, bool) {
	if r.low != 0 {
		u := r.low
		r.low = 0
		return u, true
	}
	if r.i >= len(r.s) {
		return 0, false
	}
	c, size := char(r.s, r.i)
	r.i += size
	if c >= 0x10000 {
		c -= 0x10000
		r.low = uint16(0xdc00 + c&0x3ff)
		return uint16(0xd800 + c>>10), true
	}
	return uint16(c), true
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
