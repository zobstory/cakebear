package runtime

// JSON.parse (S1 plan D3), written to V8's json-parser.cc as s1-node
// measured it (R26-R29), from the lead's spike (spikes/s1-go/json, 0 of
// 9,674 recorded inputs differing). The input is a string as the runtime
// holds one, WTF-8; a message's positions are UTF-16 units, worked out only
// when a parse fails (json_parse_errors.go).

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

type jsonToken uint8

const (
	jtEOS jsonToken = iota
	jtString
	jtNumber
	jtLBrace
	jtRBrace
	jtLBrack
	jtRBrack
	jtTrue
	jtFalse
	jtNull
	jtWS
	jtColon
	jtComma
	jtIllegal
)

func jsonTokenOf(c byte) jsonToken {
	switch c {
	case '"':
		return jtString
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return jtNumber
	case '{':
		return jtLBrace
	case '}':
		return jtRBrace
	case '[':
		return jtLBrack
	case ']':
		return jtRBrack
	case 't':
		return jtTrue
	case 'f':
		return jtFalse
	case 'n':
		return jtNull
	case ' ', '\t', '\n', '\r':
		return jtWS
	case ':':
		return jtColon
	case ',':
		return jtComma
	}
	// Bytes from 0x80 up start a character past Latin-1 or one in
	// U+0080-U+00FF; V8 reads both as ILLEGAL (its one-char table maps
	// U+0080-U+00FF to ILLEGAL too).
	return jtIllegal
}

// The messages V8 gives explicitly; "" means the jsonToken decides (lookUp).
const (
	jmNone                = ""
	jmPropNameOrRBrace    = "Expected property name or '}'"
	jmColon               = "Expected ':' after property name"
	jmCommaOrRBrace       = "Expected ',' or '}' after property value"
	jmCommaOrRBrack       = "Expected ',' or ']' after array element"
	jmDoubleQuotedName    = "Expected double-quoted property name"
	jmUnterminatedString  = "Unterminated string"
	jmBadEscape           = "Bad escaped character"
	jmBadUnicodeEscape    = "Bad Unicode escape"
	jmBadControl          = "Bad control character in string literal"
	jmNoNumberAfterMinus  = "No number after minus sign"
	jmUnterminatedFrac    = "Unterminated fractional number"
	jmExponentMissing     = "Exponent part is missing a number"
	jmNonWhitespaceAfter  = "Unexpected non-whitespace character after JSON"
	jmUnexpectedNumber    = "Unexpected number"
	jmUnexpectedString    = "Unexpected string"
	jmUnexpectedEndOfJSON = "Unexpected end of JSON input"
)

// jsonFailure is a parse's SyntaxError on its way out of the parser.
type jsonFailure struct{ err *Error }

type jsonParser struct {
	s string
	i int
}

func (p *jsonParser) fail(tok jsonToken, explicit string) {
	panic(jsonFailure{jsonSyntaxError(p.s, p.i, tok, explicit)})
}

// failAtChar is V8's ReportUnexpectedCharacter: the jsonToken is the character's.
func (p *jsonParser) failAtChar() {
	if p.i >= len(p.s) {
		p.fail(jtEOS, jmNone)
	}
	p.fail(jsonTokenOf(p.s[p.i]), jmNone)
}

func (p *jsonParser) peek() jsonToken {
	if p.i >= len(p.s) {
		return jtEOS
	}
	return jsonTokenOf(p.s[p.i])
}

func (p *jsonParser) skipWS() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonParser) check(t jsonToken) bool {
	p.skipWS()
	if p.peek() == t {
		p.i++
		return true
	}
	return false
}

func (p *jsonParser) expectNext(t jsonToken, msg string) {
	p.skipWS()
	p.expect(t, msg)
}

func (p *jsonParser) expect(t jsonToken, msg string) {
	if got := p.peek(); got != t {
		p.fail(got, msg)
	}
	p.i++
}

// JSONParse is `JSON.parse(s)` with no reviver: the value, as an unknown
// holds one, or V8's SyntaxError thrown, which points into s (R30).
func JSONParse(s string) any {
	v, err := parseJSON(s)
	if err != nil {
		panic(&Thrown{V: err, Site: err.site})
	}
	return v
}

func parseJSON(s string) (v any, err *Error) {
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(jsonFailure)
			if !ok {
				panic(r)
			}
			v, err = nil, f.err
		}
	}()
	p := &jsonParser{s: s}
	v = p.value()
	p.skipWS()
	if p.i < len(p.s) {
		p.fail(p.peek(), jmNonWhitespaceAfter)
	}
	return v, nil
}

func (p *jsonParser) value() any {
	p.skipWS()
	switch p.peek() {
	case jtString:
		p.i++
		return p.str()
	case jtNumber:
		return p.number()
	case jtLBrace:
		p.i++
		o := &jsonObject{}
		if p.check(jtRBrace) {
			return o.value()
		}
		p.expectNext(jtString, jmPropNameOrRBrace)
		k := p.str()
		p.expectNext(jtColon, jmColon)
		for {
			o.set(k, p.value())
			if p.check(jtComma) {
				p.expectNext(jtString, jmDoubleQuotedName)
				k = p.str()
				p.expectNext(jtColon, jmColon)
				continue
			}
			p.expect(jtRBrace, jmCommaOrRBrace)
			o.order()
			return o.value()
		}
	case jtLBrack:
		p.i++
		a := []any{}
		if p.check(jtRBrack) {
			return NewArray(a...)
		}
		for {
			a = append(a, p.value())
			if p.check(jtComma) {
				continue
			}
			p.expect(jtRBrack, jmCommaOrRBrack)
			return NewArray(a...)
		}
	case jtTrue:
		p.literal("true")
		return true
	case jtFalse:
		p.literal("false")
		return false
	case jtNull:
		p.literal("null")
		return Null
	}
	p.failAtChar()
	panic("unreachable")
}

// literal is V8's ScanLiteral: the first character matched already; a
// mismatch is the character's jsonToken, and running out is the end of input.
func (p *jsonParser) literal(lit string) {
	if strings.HasPrefix(p.s[p.i:], lit) {
		p.i += len(lit)
		return
	}
	p.i++
	for j := 1; j < len(lit); j++ {
		if p.i >= len(p.s) {
			p.fail(jtEOS, jmNone)
		}
		if p.s[p.i] != lit[j] {
			p.failAtChar()
		}
		p.i++
	}
	p.fail(jtEOS, jmNone)
}

func jsonDigit(c byte) bool { return c >= '0' && c <= '9' }

// numberPart is V8's IsNumberPart: what may continue a number.
func jsonNumberPart(c byte) bool {
	return jsonDigit(c) || c == '.' || c == 'e' || c == 'E' || c == '-' || c == '+'
}

func (p *jsonParser) cur() int {
	if p.i >= len(p.s) {
		return -1
	}
	return int(p.s[p.i])
}

func (p *jsonParser) number() any { return p.numberF() }

func (p *jsonParser) numberF() float64 {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	if p.cur() == '0' {
		p.i++
		if c := p.cur(); c >= 0 && jsonNumberPart(byte(c)) && jsonDigit(byte(c)) {
			p.fail(jtNumber, jmNone)
		}
	} else {
		d := p.i
		for c := p.cur(); c >= 0 && jsonDigit(byte(c)); c = p.cur() {
			p.i++
		}
		if d == p.i {
			p.fail(jtIllegal, jmNoNumberAfterMinus)
		}
	}
	if p.cur() == '.' {
		p.i++
		if c := p.cur(); c < 0 || !jsonDigit(byte(c)) {
			p.fail(jtIllegal, jmUnterminatedFrac)
		}
		for c := p.cur(); c >= 0 && jsonDigit(byte(c)); c = p.cur() {
			p.i++
		}
	}
	if c := p.cur(); c == 'e' || c == 'E' {
		p.i++
		if c := p.cur(); c == '-' || c == '+' {
			p.i++
		}
		if c := p.cur(); c < 0 || !jsonDigit(byte(c)) {
			p.fail(jtIllegal, jmExponentMissing)
		}
		for c := p.cur(); c >= 0 && jsonDigit(byte(c)); c = p.cur() {
			p.i++
		}
	}
	// The slice is JSON's number grammar, which ParseFloat reads exactly,
	// correctly rounded; a range error is ±Inf or 0, as V8 gives.
	f, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil && !jsonRange(err) {
		panic("number grammar let through " + p.s[start:p.i])
	}
	return f
}

func jsonRange(err error) bool { return errors.Is(err, strconv.ErrRange) }

func jsonHex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// str scans a string's body, after its opening quote, to its closing quote.
func (p *jsonParser) str() string {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' {
			s := p.s[start:p.i]
			p.i++
			return s
		}
		if c == '\\' || c < 0x20 {
			break
		}
		p.i++
	}
	// The slow path: escapes, built in UTF-16 units so a pair written as
	// two escapes joins, as in V8.
	var b strings.Builder
	b.WriteString(p.s[start:p.i])
	var pending rune = -1 // a high surrogate waiting for its low half
	flush := func() {
		if pending >= 0 {
			jsonWriteWTF8(&b, pending)
			pending = -1
		}
	}
	for {
		if p.i >= len(p.s) {
			p.fail(jtIllegal, jmUnterminatedString)
		}
		c := p.s[p.i]
		switch {
		case c == '"':
			flush()
			p.i++
			return b.String()
		case c < 0x20:
			p.fail(jtIllegal, jmBadControl)
		case c != '\\':
			flush()
			r, n := utf8.DecodeRuneInString(p.s[p.i:])
			if r == utf8.RuneError && n == 1 {
				b.WriteByte(c) // WTF-8 surrogate bytes, or raw bytes, kept as they are
			} else {
				b.WriteString(p.s[p.i : p.i+n])
			}
			p.i += n
			continue
		}
		p.i++ // the backslash
		if p.i >= len(p.s) {
			p.fail(jtEOS, jmNone) // measured: "\\ at the end is the end of input
		}
		e := p.s[p.i]
		var r rune
		switch e {
		case '"', '\\', '/':
			r = rune(e)
		case 'b':
			r = '\b'
		case 'f':
			r = '\f'
		case 'n':
			r = '\n'
		case 'r':
			r = '\r'
		case 't':
			r = '\t'
		case 'u':
			v := 0
			for range 4 {
				p.i++
				if p.i >= len(p.s) {
					p.fail(jtIllegal, jmBadUnicodeEscape)
				}
				h := jsonHex(p.s[p.i])
				if h < 0 {
					p.fail(jtIllegal, jmBadUnicodeEscape)
				}
				v = v<<4 | h
			}
			r = rune(v)
		default:
			if e >= 0xC4 { // a character past U+00FF: measured, the jsonToken's own message
				p.fail(jtIllegal, jmNone)
			}
			p.fail(jtIllegal, jmBadEscape)
		}
		p.i++
		switch {
		case r >= 0xDC00 && r <= 0xDFFF && pending >= 0:
			b.WriteRune(0x10000 + (pending-0xD800)<<10 + (r - 0xDC00))
			pending = -1
		case r >= 0xD800 && r <= 0xDBFF:
			flush()
			pending = r
		default:
			flush()
			if r >= 0xDC00 && r <= 0xDFFF {
				jsonWriteWTF8(&b, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
}

// writeWTF8 writes a lone surrogate as WTF-8, as the runtime stores one.
func jsonWriteWTF8(b *strings.Builder, r rune) {
	b.WriteByte(0xE0 | byte(r>>12))
	b.WriteByte(0x80 | byte(r>>6)&0x3F)
	b.WriteByte(0x80 | byte(r)&0x3F)
}
