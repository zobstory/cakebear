package runtime

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// The string methods that add to a string or take from its ends: padStart,
// padEnd, repeat and the trims (M4 strings plan S6).

// StrPadStart is `s.padStart(maxLength, fill?)`.
func StrPadStart(s string, maxLength float64, fill ...string) string {
	return strPad(s, maxLength, fill, true)
}

// StrPadEnd is `s.padEnd(maxLength, fill?)`.
func StrPadEnd(s string, maxLength float64, fill ...string) string {
	return strPad(s, maxLength, fill, false)
}

// strPad is StringPad: fill, " " if there is none, repeated and cut to make s
// maxLength code units long. The cut may leave half of a pair.
func strPad(s string, maxLength float64, fill []string, atStart bool) string {
	n := utf16Len(s)
	target := min(toInteger(maxLength), 1<<53-1) // ToLength
	if !(target > float64(n)) {
		return s
	}
	filler := " "
	if len(fill) > 0 {
		filler = fill[0]
	}
	if filler == "" {
		return s
	}
	checkLength(target)
	f, fillLen := newJSStr(filler), int(target)-n
	b := strBuilder{b: make([]byte, 0, len(s)+fillLen)}
	if !atStart {
		b.str(s)
	}
	for k := 0; k < fillLen; k += f.len() {
		b.sub(f, 0, min(f.len(), fillLen-k))
	}
	if atStart {
		b.str(s)
	}
	return b.string()
}

// StrRepeat is `s.repeat(count)`. A negative or infinite count is a
// RangeError in Node, and a panic here.
func StrRepeat(s string, count float64) string {
	k := toInteger(count)
	if k < 0 || math.IsInf(k, 1) {
		panic(fmt.Sprintf("cakebear: repeat can't repeat a string %s times (Node throws RangeError: Invalid count value: %s)",
			NumberToString(count), NumberToString(count)))
	}
	if k == 0 || s == "" {
		return ""
	}
	checkLength(float64(utf16Len(s)) * k)
	if !endsHigh(s) || !startsLow(s) {
		return strings.Repeat(s, int(k))
	}
	b := strBuilder{b: make([]byte, 0, len(s)*int(k))}
	for range int(k) {
		b.str(s) // each copy's low half joins the last one's high half
	}
	return b.string()
}

// isJSSpace reports whether r is in JavaScript's WhiteSpace or
// LineTerminator, which trim removes. It isn't Go's unicode.IsSpace: U+FEFF
// is in it, and U+0085 and U+180E are not.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// StrTrim is `s.trim()`.
func StrTrim(s string) string { return StrTrimEnd(StrTrimStart(s)) }

// StrTrimStart is `s.trimStart()`. Every space is valid UTF-8, so a half or a
// stray byte, which decodes as U+FFFD, ends the trim like any other
// character.
func StrTrimStart(s string) string {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isJSSpace(r) {
			break
		}
		i += size
	}
	return s[i:]
}

// StrTrimEnd is `s.trimEnd()`.
func StrTrimEnd(s string) string {
	j := len(s)
	for j > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:j])
		if !isJSSpace(r) {
			break
		}
		j -= size
	}
	return s[:j]
}
