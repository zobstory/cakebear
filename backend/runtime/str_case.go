package runtime

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// toUpperCase and toLowerCase (M4 strings plan S6). V8 maps case through
// ICU, with Unicode's full mappings: "ß" is "SS" in upper case, "ﬁ" is "FI",
// and "İ" in lower case is two code points. Go's unicode.ToUpper and
// unicode.ToLower map one rune to one rune. str_case_table.go, generated
// from Node, lists every code point whose mapping differs from Go's; the
// rest go through Go's.
//
// One mapping depends on context. A capital sigma is "ς", the final form,
// where it ends a word: a cased letter comes before it, skipping
// case-ignorable characters such as an apostrophe or a combining mark, and
// none comes after it. Otherwise it is "σ".

// StrToUpperCase is `s.toUpperCase()`.
func StrToUpperCase(s string) string {
	if isASCII(s) {
		return strings.ToUpper(s)
	}
	return mapCase(s, false)
}

// StrToLowerCase is `s.toLowerCase()`.
func StrToLowerCase(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	return mapCase(s, true)
}

const capitalSigma = 0x3a3

func mapCase(s string, lower bool) string {
	table, goMap := caseUpper, unicode.ToUpper
	if lower {
		table, goMap = caseLower, unicode.ToLower
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, size := char(s, i)
		switch {
		case r < utf8.RuneSelf:
			b = append(b, byte(goMap(r)))
		case size != utf8.RuneLen(r):
			// A half, whose RuneLen is -1, or a stray byte, decoded as the
			// three-byte U+FFFD: neither has a case.
			b = append(b, s[i:i+size]...)
		case r == capitalSigma && lower:
			if finalSigma(s, i, i+size) {
				b = utf8.AppendRune(b, 0x3c2)
			} else {
				b = utf8.AppendRune(b, 0x3c3)
			}
		default:
			if m, ok := table[r]; ok {
				b = append(b, m...)
			} else {
				b = utf8.AppendRune(b, goMap(r))
			}
		}
		i += size
	}
	return string(b)
}

// finalSigma is Unicode's Final_Sigma condition for the sigma at
// s[start:end], as ICU reads it: skipping case-ignorable characters, a
// cased letter before it and none after. A character that is both cased
// and case-ignorable, such as U+0345, counts as ignorable.
func finalSigma(s string, start, end int) bool {
	before := false
	for j := start; j > 0; {
		r, size := lastChar(s, j)
		j -= size
		if !inRanges(caseIgnorable, r) {
			before = inRanges(cased, r)
			break
		}
	}
	if !before {
		return false
	}
	for j := end; j < len(s); {
		r, size := char(s, j)
		j += size
		if !inRanges(caseIgnorable, r) {
			return !inRanges(cased, r)
		}
	}
	return true
}

// caseRange is an inclusive range of code points, as the width tables' are,
// so inRanges (width.go) searches both.
type caseRange = widthRange
