package runtime

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Strings (M4 strings plan S1, S5): a TypeScript string is a Go string, and
// every length and index here is in what JavaScript counts, UTF-16 code
// units (CLAUDE.md, semantics decision 1).
//
// A JavaScript string can hold half a surrogate pair on its own. The runtime
// holds one as WTF-8, the three bytes UTF-8 would make of its code point
// (json_string.go), and keeps every string it makes in WTF-8's canonical
// form: a high half followed by a low one is never two halves but the
// four-byte character they make. Cutting a pair in two leaves two halves,
// and joining them again (StrConcat) makes the character again, so `===`,
// which is Go's `==`, agrees with Node.
//
// A byte that is neither UTF-8 nor a half isn't something the runtime makes,
// though a request's URL can hold one. It counts as one code unit, U+FFFD,
// which is what a function that rebuilds the string writes for it.
//
// Most strings are ASCII, where a byte is a code unit, so each function
// checks for that first and works on the bytes. A search for an ASCII
// pattern works on the bytes whatever the string holds (str_search_ascii.go).

// stringLimit is V8's longest string, in code units (String::kMaxLength).
// Making a longer one throws RangeError: Invalid string length.
const stringLimit = 1<<29 - 24

// checkLength panics, as Node throws, when a string being made would be
// longer than V8 allows. It runs before the string is built, so a repeat of
// a billion doesn't allocate first.
func checkLength(units float64) {
	if units > stringLimit {
		panic(fmt.Sprintf("cakebear: the string would be %s code units long, past V8's limit of %d (Node throws RangeError: Invalid string length)",
			NumberToString(units), stringLimit))
	}
}

// isASCII reports whether every byte of s is below 0x80.
func isASCII(s string) bool { return asciiPrefix(s) == len(s) }

// asciiPrefix is how many bytes below 0x80 s starts with, read eight at a
// time.
func asciiPrefix(s string) int {
	i := 0
	for ; i+8 <= len(s); i += 8 {
		w := uint64(s[i]) | uint64(s[i+1])<<8 | uint64(s[i+2])<<16 | uint64(s[i+3])<<24 |
			uint64(s[i+4])<<32 | uint64(s[i+5])<<40 | uint64(s[i+6])<<48 | uint64(s[i+7])<<56
		if w&0x8080808080808080 != 0 {
			break
		}
	}
	for i < len(s) && s[i] < utf8.RuneSelf {
		i++
	}
	return i
}

// char decodes the character at s[i]: a code point, or a half's code unit,
// and its length in bytes. A stray byte is U+FFFD, one byte long.
func char(s string, i int) (rune, int) {
	if c := s[i]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	if u, ok := surrogateAt(s, i); ok {
		return u, 3
	}
	return utf8.DecodeRuneInString(s[i:])
}

// lastChar is char for the character ending at s[j].
func lastChar(s string, j int) (rune, int) {
	if j >= 3 {
		if u, ok := half(s[j-3], s[j-2], s[j-1]); ok {
			return u, 3
		}
	}
	return utf8.DecodeLastRuneInString(s[:j])
}

// half is the code unit a lone surrogate's three bytes encode, if they are
// one (surrogateAt, for bytes).
func half(b0, b1, b2 byte) (rune, bool) {
	if b0 != 0xed || b1 < 0xa0 || b1 > 0xbf || b2&0xc0 != 0x80 {
		return 0, false
	}
	return 0xd000 | rune(b1&0x3f)<<6 | rune(b2&0x3f), true
}

func appendHalf(b []byte, u rune) []byte {
	return append(b, 0xed, byte(0x80|u>>6&0x3f), byte(0x80|u&0x3f))
}

// utf16Len is a string's length as JavaScript counts it: a character
// outside the BMP is two units, and a half, like any other character, one.
// A run of ASCII is a unit a byte, and is skipped eight bytes at a time.
func utf16Len(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] < utf8.RuneSelf {
			k := asciiPrefix(s[i:])
			i += k
			n += k
			continue
		}
		r, size := char(s, i)
		i += size
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}

// toUnits is s's UTF-16 code units.
func toUnits(s string) []uint16 {
	u := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, size := char(s, i)
		i += size
		if r >= 0x10000 {
			r -= 0x10000
			u = append(u, uint16(0xd800+r>>10), uint16(0xdc00+r&0x3ff))
			continue
		}
		u = append(u, uint16(r))
	}
	return u
}

// jsStr is a string as JavaScript indexes it. units is nil for an ASCII
// string, whose bytes are its code units.
type jsStr struct {
	s     string
	units []uint16
}

func newJSStr(s string) jsStr {
	if isASCII(s) {
		return jsStr{s: s}
	}
	return jsStr{s: s, units: toUnits(s)}
}

func (v jsStr) len() int {
	if v.units == nil {
		return len(v.s)
	}
	return len(v.units)
}

func (v jsStr) at(i int) uint16 {
	if v.units == nil {
		return uint16(v.s[i])
	}
	return v.units[i]
}

// sub is the substring of code units from..to.
func (v jsStr) sub(from, to int) string {
	if v.units == nil {
		return v.s[from:to]
	}
	var b strBuilder
	b.sub(v, from, to)
	return b.string()
}

// strBuilder makes a string from pieces and keeps it canonical: a low half
// appended after a high one joins it, as the two code units would pair in
// JavaScript.
type strBuilder struct{ b []byte }

func (b *strBuilder) str(s string) {
	if len(s) >= 3 {
		if low, ok := half(s[0], s[1], s[2]); ok && low >= 0xdc00 && b.joinLow(low) {
			s = s[3:]
		}
	}
	b.b = append(b.b, s...)
}

func (b *strBuilder) unit(u uint16) {
	switch r := rune(u); {
	case r < utf8.RuneSelf:
		b.b = append(b.b, byte(r))
	case r >= 0xdc00 && r <= 0xdfff:
		if !b.joinLow(r) {
			b.b = appendHalf(b.b, r)
		}
	case r >= 0xd800 && r <= 0xdbff:
		b.b = appendHalf(b.b, r)
	default:
		b.b = utf8.AppendRune(b.b, r)
	}
}

// sub appends v's code units from..to.
func (b *strBuilder) sub(v jsStr, from, to int) {
	if v.units == nil {
		b.b = append(b.b, v.s[from:to]...)
		return
	}
	for _, u := range v.units[from:to] {
		b.unit(u)
	}
}

// joinLow replaces a high half at the end of b with the character it makes
// with low, if b ends in one.
func (b *strBuilder) joinLow(low rune) bool {
	n := len(b.b)
	if n < 3 {
		return false
	}
	high, ok := half(b.b[n-3], b.b[n-2], b.b[n-1])
	if !ok || high >= 0xdc00 {
		return false
	}
	b.b = utf8.AppendRune(b.b[:n-3], 0x10000+(high-0xd800)<<10+(low-0xdc00))
	return true
}

func (b *strBuilder) string() string { return string(b.b) }

// endsHigh and startsLow report whether s ends in a high half or starts
// with a low one, the two sides of a seam that joins.
func endsHigh(s string) bool {
	n := len(s)
	u, ok := rune(0), false
	if n >= 3 {
		u, ok = half(s[n-3], s[n-2], s[n-1])
	}
	return ok && u < 0xdc00
}

func startsLow(s string) bool {
	u, ok := surrogateAt(s, 0)
	return ok && u >= 0xdc00
}

// StrConcat is `+` on strings, and a template literal's pieces: the strings
// joined, with a high half at the end of one and a low half at the start of
// the next joined into their character (M4 strings plan S8).
func StrConcat(parts ...string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	if total > stringLimit { // a code unit is at least a byte
		units := 0
		for _, p := range parts {
			units += utf16Len(p)
		}
		checkLength(float64(units))
	}
	return joinStrings(parts, "")
}

// joinStrings is strings.Join, but a high half at the end of one piece, an
// element or the separator, joins a low half at the start of the next, as
// the code units would pair in JavaScript. Pieces with no halves at their
// ends, which is nearly all of them, go through strings.Join.
func joinStrings(items []string, sep string) string {
	if !joinsHalves(items, sep) {
		return strings.Join(items, sep)
	}
	n := len(sep) * (len(items) - 1)
	for _, s := range items {
		n += len(s)
	}
	b := strBuilder{b: make([]byte, 0, n)}
	for i, s := range items {
		if i > 0 {
			b.str(sep)
		}
		b.str(s)
	}
	return b.string()
}

// joinsHalves reports whether joining items with sep puts a high half
// before a low one anywhere, an empty piece between them or not.
func joinsHalves(items []string, sep string) bool {
	sepLow, sepHigh := startsLow(sep), endsHigh(sep)
	high := false
	for i, s := range items {
		if i > 0 && sep != "" {
			if high && sepLow {
				return true
			}
			high = sepHigh
		}
		if s != "" {
			if high && startsLow(s) {
				return true
			}
			high = endsHigh(s)
		}
	}
	return false
}

// toInteger is ToIntegerOrInfinity: NaN is 0, and anything else is
// truncated toward zero, with -0 as 0.
func toInteger(f float64) float64 {
	if math.IsNaN(f) {
		return 0
	}
	return math.Trunc(f) + 0
}

// position is ToIntegerOrInfinity(pos) clamped to 0..n: where includes,
// indexOf and startsWith start, and where substring cuts.
func position(pos float64, n int) int {
	switch {
	case !(pos > 0): // NaN, either zero, a negative
		return 0
	case pos >= float64(n):
		return n
	}
	return int(pos)
}

// relative is slice's index, where a negative one counts back from the end.
func relative(pos float64, n int) int {
	if t := math.Trunc(pos); t < 0 { // -0.5 truncates to -0, which is 0
		pos = t + float64(n)
	}
	return position(pos, n)
}

// StrLength is `s.length`.
func StrLength(s string) float64 { return float64(utf16Len(s)) }

// StrCharAt is `s.charAt(pos)`: the code unit at pos, which may be half a
// pair, or "" where there is none.
func StrCharAt(s string, pos float64) string {
	i := toInteger(pos)
	if i < 0 || i >= float64(len(s)) { // past the bytes is past the units
		return ""
	}
	if n := int(i); isASCII(s[:n+1]) {
		return s[n : n+1]
	}
	u, ok := unitAt(s, int(i))
	if !ok {
		return ""
	}
	var b strBuilder
	b.unit(u)
	return b.string()
}

// StrCharCodeAt is `s.charCodeAt(pos)`: the code unit at pos, or NaN where
// there is none.
func StrCharCodeAt(s string, pos float64) float64 {
	i := toInteger(pos)
	if i < 0 || i >= float64(len(s)) {
		return math.NaN()
	}
	u, ok := unitAt(s, int(i))
	if !ok {
		return math.NaN()
	}
	return float64(u)
}

// unitAt is the code unit at index i, found by walking s.
func unitAt(s string, i int) (uint16, bool) {
	n := 0
	for j := 0; j < len(s); {
		if s[j] < utf8.RuneSelf {
			if n == i {
				return uint16(s[j]), true
			}
			j++
			n++
			continue
		}
		r, size := char(s, j)
		j += size
		if r >= 0x10000 {
			r -= 0x10000
			if n == i {
				return uint16(0xd800 + r>>10), true
			}
			if n+1 == i {
				return uint16(0xdc00 + r&0x3ff), true
			}
			n += 2
			continue
		}
		if n == i {
			return uint16(r), true
		}
		n++
	}
	return 0, false
}

// StrSlice is `s.slice(start?, end?)`.
func StrSlice(s string, args ...float64) string {
	v := newJSStr(s)
	n := v.len()
	from, to := 0, n
	if len(args) > 0 {
		from = relative(args[0], n)
	}
	if len(args) > 1 {
		to = relative(args[1], n)
	}
	if from >= to {
		return ""
	}
	return v.sub(from, to)
}

// StrSubstring is `s.substring(start, end?)`, which clamps both ends to the
// string and swaps them if they are the wrong way round.
func StrSubstring(s string, args ...float64) string {
	v := newJSStr(s)
	n := v.len()
	start, end := 0, n
	if len(args) > 0 {
		start = position(args[0], n)
	}
	if len(args) > 1 {
		end = position(args[1], n)
	}
	return v.sub(min(start, end), max(start, end))
}

// IntToString is String() of an i32 or i64, as console.log prints one.
func IntToString(v int64) string { return strconv.FormatInt(v, 10) }

// UintToString is String() of a u32 or u64.
func UintToString(v uint64) string { return strconv.FormatUint(v, 10) }
