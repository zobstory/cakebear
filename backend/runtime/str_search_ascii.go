package runtime

import (
	"strings"
	"unicode/utf8"
)

// A search for an ASCII pattern, made on the bytes whatever the string it
// searches holds (M5 plan D13). On code units, a string that isn't ASCII is
// copied to them first (newJSStr), the whole of it, for every call.
//
// JavaScript searches code units, and bytes give the same answers. An ASCII
// byte is never part of another character: UTF-8's multi-byte sequences,
// the WTF-8 a half is held as, and a stray byte are all bytes from 0x80 up.
// So each ASCII byte is a character of its own, one code unit long, and each
// ASCII code unit is one of them. A match of the pattern's bytes is a match
// of its code units, and the other way round, and it starts and ends between
// characters. Only the indices differ: a
// position in code units becomes a byte offset going in (unitOffset), and a
// byte offset becomes code units coming out, by counting those before it.
//
// An empty pattern stays with the code units: split and replaceAll cut it
// before every one of them, between a pair's halves too.

// asciiPattern reports whether a search for w can be made on bytes.
func asciiPattern(w string) bool { return w != "" && isASCII(w) }

// optPosition is an optional position, 0 if there is none, clamped to n.
//
// Clamped to a string's length in bytes rather than in code units, it needs
// no count: a code unit is at least a byte, so where one clamp is at the
// end the other is at or past it, and unitOffset stops at the end.
func optPosition(pos []float64, n int) int {
	if len(pos) == 0 {
		return 0
	}
	return position(pos[0], n)
}

// unitOffset is where code unit u falls in s: the byte offset of the first
// character starting at or after it, and how many code units come before
// that offset. A u at a pair's low half gives the character after the pair,
// and a u past the end gives len(s) and s's length.
func unitOffset(s string, u int) (b, at int) {
	for b < len(s) && at < u {
		if s[b] < utf8.RuneSelf {
			k := asciiPrefix(s[b:min(len(s), b+u-at)])
			b += k
			at += k
			continue
		}
		r, size := char(s, b)
		b += size
		at++
		if r >= 0x10000 {
			at++
		}
	}
	return b, at
}

// canonical reports whether s is in the form the runtime makes strings in:
// no stray byte, and no high half followed by a low one. split and replace
// return pieces of s, which on code units are rebuilt in that form, a stray
// byte as U+FFFD's three bytes and two such halves as their character. Only
// a request's URL can hold either, and a string that does is still cut on
// code units, for the same bytes as before.
func canonical(s string) bool {
	high := false
	for i := 0; i < len(s); {
		if s[i] < utf8.RuneSelf {
			i += asciiPrefix(s[i:])
			high = false
			continue
		}
		r, size := char(s, i)
		if (r == utf8.RuneError && size == 1) || (high && r >= 0xdc00 && r <= 0xdfff) {
			return false
		}
		high = r >= 0xd800 && r <= 0xdbff
		i += size
	}
	return true
}

// includesASCII is includes from code unit from.
func includesASCII(s, w string, from int) bool {
	b, _ := unitOffset(s, from)
	return strings.Contains(s[b:], w)
}

// indexASCII is StringIndexOf: the first match at or after code unit from,
// in code units, or -1. A from at a pair's low half searches from the
// character after it, since no ASCII starts at a half.
func indexASCII(s, w string, from int) int {
	b, at := unitOffset(s, from)
	i := strings.Index(s[b:], w)
	if i < 0 {
		return -1
	}
	return at + utf16Len(s[b:b+i])
}

// lastIndexASCII is lastIndexOf: the last match starting at or before code
// unit start, in code units, or -1.
func lastIndexASCII(s, w string, start int) int {
	end := len(s)
	if start < len(s) {
		// A match starts at an ASCII byte, so before the first character
		// that starts after code unit start, and ends len(w) bytes on.
		b, _ := unitOffset(s, start+1)
		end = min(end, b-1+len(w))
	}
	i := strings.LastIndex(s[:end], w)
	if i < 0 {
		return -1
	}
	return utf16Len(s[:i])
}

// startsWithASCII is startsWith at code unit start. A start at a pair's low
// half starts with no ASCII, and neither does one past the end.
func startsWithASCII(s, w string, start int) bool {
	b, at := unitOffset(s, start)
	return at == start && strings.HasPrefix(s[b:], w)
}

// endsWithASCII is endsWith at code unit end. An end at a pair's low half
// gives the offset after the pair, and no ASCII ends a pair.
func endsWithASCII(s, w string, end int) bool {
	b := len(s)
	if end < len(s) {
		b, _ = unitOffset(s, end)
	}
	return strings.HasSuffix(s[:b], w)
}

// splitASCII is split: the pieces between the separator's matches, at most
// lim of them, found in one pass. strings.Split counts the matches first,
// which costs more than the appends it saves.
func splitASCII(s, sep string, lim uint64) []string {
	var parts []string
	for {
		i := strings.Index(s, sep)
		if i < 0 {
			return append(parts, s)
		}
		if parts = append(parts, s[:i]); uint64(len(parts)) == lim {
			return parts
		}
		s = s[i+len(sep):]
	}
}

// replaceAllASCII is replaceAll: every match, none overlapping, replaced.
func replaceAllASCII(s, w, replacement string) string {
	var positions []int
	for from := 0; ; {
		i := strings.Index(s[from:], w)
		if i < 0 {
			break
		}
		positions = append(positions, from+i)
		from += i + len(w)
	}
	if positions == nil {
		return s
	}
	return replaceASCII(s, len(w), positions, parseTemplate(replacement))
}

// replaceASCII is replaceAt for a match of m ASCII bytes at each of the byte
// offsets positions. The pieces of s it keeps, and $` and $' of it, start
// and end between characters, and a strBuilder joins them, a high half at
// the end of one to a low half at the start of the next, as their code units
// would pair.
func replaceASCII(s string, m int, positions []int, t template) string {
	size := float64(len(s) - m*len(positions))
	for _, pos := range positions {
		size += t.size(pos, m, len(s))
	}
	if size > stringLimit { // a code unit is at least a byte
		checkLength(replacedLength(s, m, positions, t))
	}
	b := strBuilder{b: make([]byte, 0, int(size))}
	end := 0
	for _, pos := range positions {
		b.str(s[end:pos])
		t.writeBytes(&b, s, pos, m)
		end = pos + m
	}
	b.str(s[end:])
	return b.string()
}

// replacedLength is what replaceASCII makes, in code units, counted only
// when its bytes say it could be past V8's limit.
func replacedLength(s string, m int, positions []int, t template) float64 {
	n := utf16Len(s)
	units := float64(n - m*len(positions))
	at, prev := 0, 0
	for _, pos := range positions {
		at += utf16Len(s[prev:pos])
		prev = pos
		units += t.length(at, m, n)
	}
	return units
}

// size is length in bytes: what the template makes of a match of m bytes at
// byte pos, in a string of n bytes.
func (t *template) size(pos, m, n int) float64 {
	return float64(t.bytes) + float64(t.matched)*float64(m) +
		float64(t.before)*float64(pos) + float64(t.after)*float64(n-pos-m)
}

// writeBytes is write, for a match of m bytes at byte pos.
func (t *template) writeBytes(b *strBuilder, s string, pos, m int) {
	for _, p := range t.parts {
		switch p.kind {
		case 0:
			b.str(p.text)
		case '&':
			b.str(s[pos : pos+m])
		case '`':
			b.str(s[:pos])
		default:
			b.str(s[pos+m:])
		}
	}
}
