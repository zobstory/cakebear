package runtime

import (
	"math"
	"math/bits"
	"strconv"
	"unicode/utf8"
)

// The number grammars of ECMA-262, as V8 13.6 (Node 24.18) reads them:
// Number(s) is StringToNumber, which the whole string must match, and
// parseInt and parseFloat read the longest prefix they can. None of them
// reads a numeric separator (1_000), and none is Go's strconv, which takes
// "inf", "nan", "0x1p-2" and underscores.

// numSpace is JavaScript's WhiteSpace and LineTerminator, which the parsers
// skip: Unicode's Zs, the ASCII controls TAB to CR, U+FEFF, and U+2028 and
// U+2029. Go's unicode.IsSpace differs on three: it takes U+0085 and
// U+180E, and not U+FEFF. Measured on Node 24.18, every code unit.
func numSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// numTrimStart is s without its leading whitespace. A lone surrogate's
// bytes decode as an error, which is no space, so it ends the run.
func numTrimStart(s string) string {
	for i := 0; i < len(s); {
		if c := s[i]; c < utf8.RuneSelf {
			if c != ' ' && (c < '\t' || c > '\r') {
				return s[i:]
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !numSpace(r) {
			return s[i:]
		}
		i += size
	}
	return ""
}

// numTrimEnd is s without its trailing whitespace.
func numTrimEnd(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if !numSpace(r) {
			break
		}
		s = s[:len(s)-size]
	}
	return s
}

// numDigit is c's value as a digit of radix 36 or less, or 36 for a byte
// that is a digit of none.
func numDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return 36
}

// StringToNumber is Number(s). After the whitespace is trimmed, the empty
// string is 0, a 0x, 0o or 0b prefix (either case, and no sign) takes hex,
// octal or binary digits, and otherwise the rest must be a decimal literal:
// a sign, then Infinity or digits with a point and an exponent, each
// optional. Anything else is NaN.
func StringToNumber(s string) float64 {
	s = numTrimEnd(numTrimStart(s))
	if s == "" {
		return 0
	}
	if len(s) > 2 && s[0] == '0' {
		shift := uint(0)
		switch s[1] | 0x20 {
		case 'x':
			shift = 4
		case 'o':
			shift = 3
		case 'b':
			shift = 1
		}
		if shift != 0 {
			v, n := numPow2Digits(s[2:], shift)
			if n != len(s)-2 {
				return math.NaN()
			}
			return v
		}
	}
	if v, n := numScanDecimal(s); n == len(s) {
		return v
	}
	return math.NaN()
}

// ParseFloat is parseFloat(s): the longest decimal literal after the
// leading whitespace, as StringToNumber reads one, or NaN if there's none.
// It reads no prefix, so "0x10" is 0, and "" is NaN.
func ParseFloat(s string) float64 {
	v, n := numScanDecimal(numTrimStart(s))
	if n == 0 {
		return math.NaN()
	}
	return v
}

// ParseInt is parseInt(s) or parseInt(s, radix). The radix goes through
// ToInt32, so it wraps modulo 2^32, and NaN and the infinities are 0. 0, or
// none, is 10, or 16 after a 0x prefix; 16 takes the prefix too, and any
// other radix outside 2 to 36 is NaN. After the whitespace and a sign it
// reads the longest run of the radix's digits, and NaN if there is none.
//
// As in V8, a radix of 10 or a power of two gives the exact value rounded
// to the nearest double; any other accumulates in doubles, which the
// specification allows, and loses precision past about 2^53 (see
// numGenericDigits).
func ParseInt(s string, radix ...float64) float64 {
	r := 0
	if len(radix) > 0 {
		r = int(numToInt32(radix[0]))
	}
	if r != 0 && (r < 2 || r > 36) {
		return math.NaN()
	}
	s = numTrimStart(s)
	negative := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		negative = s[0] == '-'
		s = s[1:]
	}
	if r == 0 || r == 16 {
		if len(s) >= 2 && s[0] == '0' && s[1]|0x20 == 'x' {
			s, r = s[2:], 16
		} else if r == 0 {
			r = 10
		}
	}
	var v float64
	var n int
	switch {
	case r == 10:
		n = numDigitRun(s, 10)
		v, _ = strconv.ParseFloat(s[:n], 64) // only digits, so at worst out of range: ±Inf
	case r&(r-1) == 0:
		v, n = numPow2Digits(s, uint(bits.TrailingZeros(uint(r))))
	default:
		v, n = numGenericDigits(s, r)
	}
	if n == 0 {
		return math.NaN()
	}
	if negative {
		return -v
	}
	return v
}

// numToInt32 is ToInt32: NaN and the infinities are 0, and anything else is
// truncated, then wrapped modulo 2^32 into int32's range.
func numToInt32(x float64) int32 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	return int32(uint32(int64(math.Mod(math.Trunc(x), 1<<32))))
}

// numDigitRun is how many bytes at the start of s are digits of radix.
func numDigitRun(s string, radix int) int {
	n := 0
	for n < len(s) && numDigit(s[n]) < radix {
		n++
	}
	return n
}

// numPow2Digits reads the digits of radix 1<<shift (2, 4, 8, 16 or 32) at
// the start of s, and gives their value and how many bytes it read. It is
// V8's InternalStringToIntDouble: exact while the value fits in 53 bits,
// then the bits that don't fit decide the rounding, to the nearest double
// and a tie to even, as for a decimal.
func numPow2Digits(s string, shift uint) (float64, int) {
	radix := 1 << shift
	i := 0
	for i < len(s) && s[i] == '0' {
		i++
	}
	var number int64
	for ; i < len(s); i++ {
		d := numDigit(s[i])
		if d >= radix {
			break
		}
		number = number<<shift | int64(d)
		overflow := number >> 53
		if overflow == 0 {
			continue
		}
		dropped := bits.Len64(uint64(overflow))
		droppedBits := number & (1<<dropped - 1)
		number >>= dropped
		exponent := dropped
		zeroTail := true
		for i++; i < len(s) && numDigit(s[i]) < radix; i++ {
			zeroTail = zeroTail && s[i] == '0'
			exponent += int(shift)
		}
		middle := int64(1) << (dropped - 1)
		if droppedBits > middle || droppedBits == middle && (number&1 != 0 || !zeroTail) {
			number++ // a carry to 2^53 is still exact, so V8's renormalising is no change
		}
		return math.Ldexp(float64(number), exponent), i
	}
	return float64(number), i
}

// numGenericDigits reads the digits of a radix that is neither 10 nor a
// power of two, as V8's NumberParseIntHelper::HandleGenericCase does: in
// parts whose multiplier stays within 32 bits, each folded into a double
// result as result*multiplier + part. Leading zeros are skipped first, as
// V8 skips them, since where the parts start decides how the result
// rounds.
//
// The fold is one fused multiply-add, rounded once. That is what Node 24.18
// does on arm64, where clang fuses V8's C++ expression, and the goldens are
// recorded there; a build without FMA rounds twice, and differs in the last
// bit past about 2^53. math.FMA gives the same answer on every platform.
func numGenericDigits(s string, radix int) (float64, int) {
	const maxMultiplier = math.MaxUint32 / 36
	result := 0.0
	i := 0
	for i < len(s) && s[i] == '0' {
		i++
	}
	for {
		part, multiplier := uint32(0), uint32(1)
		done := true
		for ; i < len(s); i++ {
			d := numDigit(s[i])
			if d >= radix {
				break
			}
			m := multiplier * uint32(radix)
			if m > maxMultiplier {
				done = false
				break
			}
			part = part*uint32(radix) + uint32(d)
			multiplier = m
		}
		result = math.FMA(result, float64(multiplier), float64(part))
		if done {
			return result, i
		}
	}
}

// numScanDecimal reads the longest StrDecimalLiteral at the start of s: a
// sign, then Infinity, or digits with an optional point and fraction, or a
// point and a fraction, then an optional exponent, which counts only with a
// digit in it. It gives the value and how many bytes it read, 0 for none.
func numScanDecimal(s string) (float64, int) {
	i := 0
	negative := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		negative = s[i] == '-'
		i++
	}
	sign := 1.0
	if negative {
		sign = -1
	}
	if len(s)-i >= 8 && s[i:i+8] == "Infinity" {
		return math.Inf(int(sign)), i + 8
	}
	intStart := i
	i += numDigitRun(s[i:], 10)
	whole := s[intStart:i]
	fraction := ""
	if i < len(s) && s[i] == '.' {
		fraction = s[i+1 : i+1+numDigitRun(s[i+1:], 10)]
		i += 1 + len(fraction)
	}
	if whole == "" && fraction == "" {
		return 0, 0 // a point alone is no literal
	}
	exponent := 0
	if i < len(s) && s[i]|0x20 == 'e' {
		j := i + 1
		expSign := 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			if s[j] == '-' {
				expSign = -1
			}
			j++
		}
		if n := numDigitRun(s[j:], 10); n > 0 {
			for _, c := range []byte(s[j : j+n]) {
				// Saturated: past this, the value is 0 or infinite for
				// any string that fits in memory.
				exponent = min(exponent*10+int(c-'0'), 1e9)
			}
			exponent *= expSign
			i = j + n
		}
	}
	return sign * numDecimalValue(whole, fraction, exponent), i
}

// numDecimalValue is whole.fraction × 10^exponent, rounded to the nearest
// double. It hands strconv a form whose exponent is small, 0.DIGITS×10^p
// with the leading zeros gone, because strconv's own exponent saturates at
// a few digits, which a long enough run of zeros could make matter.
func numDecimalValue(whole, fraction string, exponent int) float64 {
	digits := whole + fraction
	lead := 0
	for lead < len(digits) && digits[lead] == '0' {
		lead++
	}
	digits = digits[lead:]
	if digits == "" {
		return 0
	}
	// The value is in [10^(p-1), 10^p).
	p := len(whole) - lead + exponent
	switch {
	case p > 310:
		return math.Inf(1)
	case p < -323:
		return 0
	}
	v, _ := strconv.ParseFloat("0."+digits+"e"+strconv.Itoa(p), 64) // the grammar is checked: at worst ±Inf
	return v
}

// NumberIsFinite is Number.isFinite(x), and isFinite(x) of a number.
func NumberIsFinite(x float64) bool { return !math.IsInf(x, 0) && !math.IsNaN(x) }

// NumberIsNaN is Number.isNaN(x), and isNaN(x) of a number.
func NumberIsNaN(x float64) bool { return math.IsNaN(x) }

// NumberIsInteger is Number.isInteger(x): finite, with no fraction. -0 is
// one.
func NumberIsInteger(x float64) bool { return NumberIsFinite(x) && math.Trunc(x) == x }
