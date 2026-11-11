package runtime

// JSON.parse's SyntaxErrors (S1 plan D3): V8's messages (s1-node R29), built
// in UTF-16 units from the WTF-8 source, and where the report points, which
// is into the data, not the program (R30).

import (
	"strconv"
	"strings"
)

// units decodes WTF-8 to UTF-16 code units, and maps each byte offset that
// starts a character to the unit index it starts at.
func jsonUnits(s string) (u []uint16, at func(byteOff int) int) {
	starts := make([]int, 0, len(s)+1)
	offs := make([]int, 0, len(s)+1)
	for i := 0; i < len(s); {
		starts = append(starts, i)
		offs = append(offs, len(u))
		c := s[i]
		switch {
		case c < 0x80:
			u = append(u, uint16(c))
			i++
		case c >= 0xF0 && i+3 < len(s):
			r := rune(c&0x07)<<18 | rune(s[i+1]&0x3F)<<12 | rune(s[i+2]&0x3F)<<6 | rune(s[i+3]&0x3F)
			r -= 0x10000
			u = append(u, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			i += 4
		case c >= 0xE0 && i+2 < len(s):
			u = append(u, uint16(rune(c&0x0F)<<12|rune(s[i+1]&0x3F)<<6|rune(s[i+2]&0x3F)))
			i += 3
		case c >= 0xC0 && i+1 < len(s):
			u = append(u, uint16(rune(c&0x1F)<<6|rune(s[i+1]&0x3F)))
			i += 2
		default:
			u = append(u, 0xFFFD)
			i++
		}
	}
	starts = append(starts, len(s))
	offs = append(offs, len(u))
	return u, func(b int) int {
		lo, hi := 0, len(starts)-1
		for lo < hi { // the first start >= b
			m := (lo + hi) / 2
			if starts[m] < b {
				lo = m + 1
			} else {
				hi = m
			}
		}
		return offs[lo]
	}
}

// fromUnits encodes units as WTF-8: pairs as their character, a lone
// surrogate as itself.
func jsonFromUnits(u []uint16) string {
	var b strings.Builder
	for i := 0; i < len(u); i++ {
		c := rune(u[i])
		if c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF {
			b.WriteRune(0x10000 + (c-0xD800)<<10 + (rune(u[i+1]) - 0xDC00))
			i++
			continue
		}
		if c >= 0xD800 && c <= 0xDFFF {
			jsonWriteWTF8(&b, c)
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// jsonSyntaxError is the SyntaxError V8 throws for src at byteOff, made
// with the site its report points at: the line of the data holding it.
func jsonSyntaxError(src string, byteOff int, tok jsonToken, explicit string) *Error {
	u, at := jsonUnits(src)
	pos := at(byteOff)
	msg := jsonMessage(src, u, pos, tok, explicit)
	return NewError(ErrorSyntax, msg, jsonDataSite(u, pos, tok == jtEOS && explicit == jmNone))
}

// jsonMessage is V8's ReportUnexpectedToken: an explicit message is
// positioned; otherwise the token decides (LookUpErrorMessageForJsonToken).
func jsonMessage(src string, u []uint16, pos int, tok jsonToken, explicit string) string {
	positioned := func(what string) string {
		line, col := jsonLocation(u, pos)
		in := " in JSON"
		if what == jmNonWhitespaceAfter {
			in = "" // V8's template for it says "after JSON at position"
		}
		return what + in + " at position " + strconv.Itoa(pos) +
			" (line " + strconv.Itoa(line) + " column " + strconv.Itoa(col) + ")"
	}
	if explicit != jmNone {
		return positioned(explicit)
	}
	switch tok {
	case jtEOS:
		return jmUnexpectedEndOfJSON
	case jtNumber:
		return positioned(jmUnexpectedNumber)
	case jtString:
		return positioned(jmUnexpectedString)
	}
	switch src {
	case "[object Object]", "undefined", "Infinity", "NaN":
		return `"` + src + `" is not valid JSON`
	}
	t := jsonFromUnits(u[pos : pos+1])
	n := len(u)
	const minForContext, ctx = 21, 10
	switch {
	case n < minForContext:
		return "Unexpected token '" + t + "', \"" + src + "\" is not valid JSON"
	case pos < ctx:
		return "Unexpected token '" + t + "', \"" + jsonFromUnits(u[:pos+ctx]) + "\"... is not valid JSON"
	case pos < n-ctx:
		return "Unexpected token '" + t + "', ...\"" + jsonFromUnits(u[pos-ctx:pos+ctx]) + "\"... is not valid JSON"
	default:
		return "Unexpected token '" + t + "', ...\"" + jsonFromUnits(u[pos-ctx:]) + "\" is not valid JSON"
	}
}

// location is V8's CalculateFileLocation: CRLF is one break, CR or LF alone
// one each; the column counts units from the last break, from 1.
func jsonLocation(u []uint16, pos int) (line, col int) {
	line, last := 1, 0
	for i := 0; i < pos; i++ {
		if u[i] == '\r' && i < pos-1 && u[i+1] == '\n' {
			i++
		}
		if u[i] == '\r' || u[i] == '\n' {
			line++
			last = i + 1
		}
	}
	return line, 1 + pos - last
}

// jsonDataSite is where the report of a parse's SyntaxError points (R30):
// `<anonymous_script>:L`, the data's line L whole, without its CR, and a
// caret at the column, or none for the end of the input.
func jsonDataSite(u []uint16, pos int, atEnd bool) *Site {
	line, col := jsonLocation(u, pos)
	start := pos - (col - 1)
	end := start
	for end < len(u) && u[end] != '\n' && u[end] != '\r' {
		end++
	}
	site := &Site{File: anonymousScript, Line: line, Col: col, Text: jsonFromUnits(u[start:end]), Data: true}
	if atEnd {
		site.Col = 0
	}
	return site
}
