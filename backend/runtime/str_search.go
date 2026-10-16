package runtime

import (
	"math"
	"strings"
)

// The string methods that search one string for another (M4 strings plan
// S6), each as ECMA-262 writes it, on code units. A search can match half
// of a pair: "😀".indexOf("\uD83D") is 0. A search for an ASCII pattern is
// made on bytes instead (str_search_ascii.go).

// indexOf is StringIndexOf: the first index at or after from where w
// occurs in v, or -1. An empty w occurs at from, if from is in the string.
func indexOf(v, w jsStr, from int) int {
	n, m := v.len(), w.len()
	if m == 0 {
		if from <= n {
			return from
		}
		return -1
	}
	for i := from; i+m <= n; i++ {
		if equalAt(v, w, i) {
			return i
		}
	}
	return -1
}

// equalAt reports whether w occurs in v at i, where it fits.
func equalAt(v, w jsStr, i int) bool {
	for k := range w.len() {
		if v.at(i+k) != w.at(k) {
			return false
		}
	}
	return true
}

// StrIncludes is `s.includes(search, pos?)`.
func StrIncludes(s, search string, pos ...float64) bool {
	if asciiPattern(search) {
		return includesASCII(s, search, optPosition(pos, len(s)))
	}
	return StrIndexOf(s, search, pos...) >= 0
}

// StrIndexOf is `s.indexOf(search, pos?)`.
func StrIndexOf(s, search string, pos ...float64) float64 {
	if asciiPattern(search) {
		return float64(indexASCII(s, search, optPosition(pos, len(s))))
	}
	v, w := newJSStr(s), newJSStr(search)
	start := 0
	if len(pos) > 0 {
		start = position(pos[0], v.len())
	}
	return float64(indexOf(v, w, start))
}

// StrLastIndexOf is `s.lastIndexOf(search, pos?)`. A missing pos, or NaN,
// searches from the end.
func StrLastIndexOf(s, search string, pos ...float64) float64 {
	if asciiPattern(search) {
		start := len(s)
		if len(pos) > 0 && !math.IsNaN(pos[0]) {
			start = position(pos[0], len(s))
		}
		return float64(lastIndexASCII(s, search, start))
	}
	v, w := newJSStr(s), newJSStr(search)
	n, m := v.len(), w.len()
	start := n
	if len(pos) > 0 && !math.IsNaN(pos[0]) {
		start = position(pos[0], n)
	}
	for i := min(start, n-m); i >= 0; i-- {
		if equalAt(v, w, i) {
			return float64(i)
		}
	}
	return -1
}

// StrStartsWith is `s.startsWith(search, pos?)`.
func StrStartsWith(s, search string, pos ...float64) bool {
	if asciiPattern(search) {
		return startsWithASCII(s, search, optPosition(pos, len(s)))
	}
	v, w := newJSStr(s), newJSStr(search)
	start := 0
	if len(pos) > 0 {
		start = position(pos[0], v.len())
	}
	return start+w.len() <= v.len() && equalAt(v, w, start)
}

// StrEndsWith is `s.endsWith(search, end?)`.
func StrEndsWith(s, search string, end ...float64) bool {
	if asciiPattern(search) {
		e := len(s)
		if len(end) > 0 {
			e = position(end[0], len(s))
		}
		return endsWithASCII(s, search, e)
	}
	v, w := newJSStr(s), newJSStr(search)
	e := v.len()
	if len(end) > 0 {
		e = position(end[0], v.len())
	}
	start := e - w.len()
	return start >= 0 && equalAt(v, w, start)
}

// toUint32 is ToUint32, split's limit: NaN and the infinities are 0, and
// anything else is truncated and taken modulo 2^32, so -1 is 4294967295.
func toUint32(f float64) uint32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return uint32(int64(math.Mod(math.Trunc(f), 1<<32)))
}

// StrSplit is `s.split(separator, limit?)` with a string separator. An empty
// separator splits s into its code units, a pair into its two halves.
func StrSplit(s, separator string, limit ...float64) *Array[string] {
	lim := uint64(math.MaxUint32)
	if len(limit) > 0 {
		lim = uint64(toUint32(limit[0]))
	}
	if lim == 0 {
		return NewArray[string]()
	}
	if asciiPattern(separator) && canonical(s) {
		return NewArray(splitASCII(s, separator, lim)...)
	}
	v, sep := newJSStr(s), newJSStr(separator)
	n, m := v.len(), sep.len()
	if m == 0 {
		parts := make([]string, min(uint64(n), lim))
		for i := range parts {
			parts[i] = v.sub(i, i+1)
		}
		return NewArray(parts...)
	}
	if n == 0 {
		return NewArray(s)
	}
	var parts []string
	i := 0
	for j := indexOf(v, sep, 0); j >= 0; j = indexOf(v, sep, i) {
		parts = append(parts, v.sub(i, j))
		if uint64(len(parts)) == lim {
			return NewArray(parts...)
		}
		i = j + m
	}
	return NewArray(append(parts, v.sub(i, n))...)
}

// A template is a replacement string cut at the patterns GetSubstitution
// replaces: $$, $&, $` and $'. A string pattern has no captures, so $1 and
// $<name> are text, as is a $ before anything else.
type template struct {
	parts                  []templatePart
	units, bytes           int // the text parts' length, in code units and in bytes
	matched, before, after int // how many $&, $` and $' it holds
}

// A templatePart is text, or, by kind, the pattern it stands for: & for $&,
// ` for $` and the apostrophe for $'.
type templatePart struct {
	text string
	kind byte
}

func parseTemplate(r string) template {
	var t template
	start := 0
	for i := 0; i+1 < len(r); i++ {
		if r[i] != '$' {
			continue
		}
		switch c := r[i+1]; c {
		case '$':
			t.text(r[start : i+1]) // keeps one $
		case '&', '`', '\'':
			t.text(r[start:i])
			t.parts = append(t.parts, templatePart{kind: c})
			switch c {
			case '&':
				t.matched++
			case '`':
				t.before++
			default:
				t.after++
			}
		default:
			continue
		}
		start = i + 2
		i++
	}
	t.text(r[start:])
	return t
}

func (t *template) text(s string) {
	if s != "" {
		t.parts = append(t.parts, templatePart{text: s})
		t.units += utf16Len(s)
		t.bytes += len(s)
	}
}

// length is how long the template makes a match of m units at pos, in a
// string of n.
func (t *template) length(pos, m, n int) float64 {
	return float64(t.units) + float64(t.matched)*float64(m) +
		float64(t.before)*float64(pos) + float64(t.after)*float64(n-pos-m)
}

func (t *template) write(b *strBuilder, v jsStr, pos, m int) {
	for _, p := range t.parts {
		switch p.kind {
		case 0:
			b.str(p.text)
		case '&':
			b.sub(v, pos, pos+m)
		case '`':
			b.sub(v, 0, pos)
		default:
			b.sub(v, pos+m, v.len())
		}
	}
}

// StrReplace is `s.replace(pattern, replacement)` with a string pattern: the
// first match replaced.
func StrReplace(s, pattern, replacement string) string {
	if asciiPattern(pattern) && canonical(s) {
		pos := strings.Index(s, pattern)
		if pos < 0 {
			return s
		}
		return replaceASCII(s, len(pattern), []int{pos}, parseTemplate(replacement))
	}
	v, w := newJSStr(s), newJSStr(pattern)
	pos := indexOf(v, w, 0)
	if pos < 0 {
		return s
	}
	return replaceAt(v, w.len(), []int{pos}, parseTemplate(replacement))
}

// StrReplaceAll is `s.replaceAll(pattern, replacement)` with a string
// pattern. An empty one matches before every code unit and at the end.
func StrReplaceAll(s, pattern, replacement string) string {
	if asciiPattern(pattern) && canonical(s) {
		return replaceAllASCII(s, pattern, replacement)
	}
	v, w := newJSStr(s), newJSStr(pattern)
	m := w.len()
	var positions []int
	for pos := indexOf(v, w, 0); pos >= 0; pos = indexOf(v, w, pos+max(m, 1)) {
		positions = append(positions, pos)
	}
	if positions == nil {
		return s
	}
	return replaceAt(v, m, positions, parseTemplate(replacement))
}

// replaceAt replaces the matches of m units at each of positions, after
// checking the result's length.
func replaceAt(v jsStr, m int, positions []int, t template) string {
	n := v.len()
	units := float64(n - m*len(positions))
	for _, pos := range positions {
		units += t.length(pos, m, n)
	}
	checkLength(units)
	b := strBuilder{b: make([]byte, 0, len(v.s)+len(positions))}
	end := 0
	for _, pos := range positions {
		b.sub(v, end, pos)
		t.write(&b, v, pos, m)
		end = pos + m
	}
	b.sub(v, end, n)
	return b.string()
}
