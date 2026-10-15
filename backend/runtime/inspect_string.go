package runtime

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Strings inside what console.log inspects (formatPrimitive and strEscape in
// inspect.js). A string at the top level of console.log prints as it is
// (Format); these are the ones inside an object or an array.

// str is formatPrimitive for a string: cut at 10,000 UTF-16 units (R13), and
// a long one split after each newline, a quoted piece per line (R12).
//
// inspect.js also requires a string to split to be longer than 16 units.
// That can't bind: within the depth limit a string is indented at most 6, so
// the length it must exceed here is at least 70.
func (c *inspector) str(s string) string {
	trailer, lone := "", ""
	length := utf16Len(s)
	if length > maxStringLength {
		remaining := length - maxStringLength
		s, lone = cutUTF16(s, maxStringLength)
		trailer = fmt.Sprintf("... %d more character%s", remaining, plural(remaining))
		length = maxStringLength
	}
	if length > breakLength-c.indent-4 {
		lines := splitAfterNewlines(s)
		for i, line := range lines {
			suffix := ""
			if i == len(lines)-1 {
				suffix = lone
			}
			lines[i] = quoteWith(line, suffix)
		}
		return strings.Join(lines, " +\n"+strings.Repeat(" ", c.indent+2)) + trailer
	}
	return quoteWith(s, lone) + trailer
}

// cutUTF16 is s.slice(0, n) in UTF-16 units. A cut through a surrogate pair
// leaves JavaScript the pair's first half, a lone surrogate, which no Go
// string can hold; it is returned as the escape Node prints it as.
func cutUTF16(s string, n int) (prefix, lone string) {
	units := 0
	for i, r := range s {
		size := 1
		if r >= 0x10000 {
			size = 2
		}
		if units+size > n {
			if units < n { // one unit left: the high surrogate
				lone = fmt.Sprintf(`\u%x`, 0xd800+((r-0x10000)>>10))
			}
			return s[:i], lone
		}
		units += size
	}
	return s, ""
}

// splitAfterNewlines is split(/(?<=\n)/): each piece ends in its newline,
// and a last piece without one follows if there is text after the last.
func splitAfterNewlines(s string) []string {
	var lines []string
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 || i == len(s)-1 {
			return append(lines, s)
		}
		lines = append(lines, s[:i+1])
		s = s[i+1:]
	}
}

// quote is strEscape: s in quotes, escaped (R2, R3).
func quote(s string) string { return quoteWith(s, "") }

// quoteWith quotes s with suffix, already escaped, before the closing quote.
//
// The quote is `'`, unless s holds one: then `"` if s holds none of those,
// else a backtick if s holds none and no `${`, else `'` again with each `'`
// escaped. Control characters, the C1 range and the backslash are escaped;
// everything else, U+00A0 and emoji included, is written as it is.
func quoteWith(s, suffix string) string {
	q := byte('\'')
	if strings.Contains(s, "'") {
		switch {
		case !strings.Contains(s, `"`):
			q = '"'
		case !strings.Contains(s, "`") && !strings.Contains(s, "${"):
			q = '`'
		}
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte(q)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\'' && q == '\'':
			b.WriteString(`\'`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			b.WriteString(controlEscape(r))
		default:
			b.WriteString(s[i : i+size]) // the bytes as they are, valid or not
		}
		i += size
	}
	b.WriteString(suffix)
	b.WriteByte(q)
	return b.String()
}

// controlEscape is inspect.js's meta table for a control character: the
// short escapes it names, and `\xHH` in upper case for the rest, U+000B
// included.
func controlEscape(r rune) string {
	switch r {
	case '\b':
		return `\b`
	case '\t':
		return `\t`
	case '\n':
		return `\n`
	case '\f':
		return `\f`
	case '\r':
		return `\r`
	}
	return fmt.Sprintf(`\x%02X`, r)
}
