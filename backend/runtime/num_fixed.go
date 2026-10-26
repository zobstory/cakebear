package runtime

import (
	"math"
	"math/bits"
	"strconv"
)

// NumberToFixed is x.toFixed(digits): x written with digits digits after
// the point, rounded from the exact value of the double, a tie going up,
// away from zero. That is the one way it differs from Go's FormatFloat,
// which rounds a tie to even: (0.5).toFixed(0) is "1" and (2.5).toFixed(0)
// is "3", where FormatFloat gives "0" and "2". A value that only looks like
// a tie isn't one, since its double is a little off: 1.005 is below it, so
// (1.005).toFixed(2) is "1.00".
//
// digits goes through ToIntegerOrInfinity, so NaN is 0 and 2.9 is 2, and
// anything outside 0 to 100 is the RangeError Node throws, made at site,
// before x is looked at. Then NaN is "NaN", and from 1e21 up, the infinities
// included, the result is NumberToString's. The sign is x < 0's: -0 has
// none, but a negative that rounds to zero keeps it, so (-0.0001).toFixed(2)
// is "-0.00".
func NumberToFixed(x, digits float64, site *Site) string {
	f := math.Trunc(digits)
	if math.IsNaN(f) {
		f = 0
	}
	if f < 0 || f > 100 {
		throwError(ErrorRange, "toFixed() digits argument must be between 0 and 100", site)
	}
	abs := math.Abs(x)
	switch {
	case math.IsNaN(x):
		return "NaN"
	case abs >= 1e21:
		return NumberToString(x)
	}
	n := int(f)
	var s string
	if numFixedTie(abs, n) {
		s = numRoundUpTie(strconv.FormatFloat(abs, 'f', n+1, 64), n)
	} else {
		s = strconv.FormatFloat(abs, 'f', n, 64)
	}
	if x < 0 {
		return "-" + s
	}
	return s
}

// numFixedTie reports whether x, finite and not negative, lies exactly halfway
// between two numbers of n decimal places, the one case where rounding half
// up and FormatFloat's half to even disagree.
//
// x is m / 2^k with m odd, and then its decimal expansion has exactly k
// places, the last a 5, since m / 2^k is m·5^k / 10^k and m·5^k is odd and
// a multiple of 5. So what follows place n is exactly one half when k is
// n + 1, and otherwise it is either nothing (k <= n) or not a half.
func numFixedTie(x float64, n int) bool {
	frac, exp := math.Frexp(x) // x is frac × 2^exp, frac in [0.5, 1) holding 53 bits at most
	mant := uint64(frac * (1 << 53))
	if mant == 0 {
		return false
	}
	return 53-exp-bits.TrailingZeros64(mant) == n+1
}

// numRoundUpTie is s, a tie's exact expansion to n+1 places ending in its 5,
// rounded up to n places: the 5 goes, and the place before it gains one,
// carrying through nines, and through the point.
func numRoundUpTie(s string, n int) string {
	b := []byte(s[:len(s)-1])
	if n == 0 {
		b = b[:len(b)-1] // the point, with no places left after it
	}
	for i := len(b) - 1; i >= 0; i-- {
		switch b[i] {
		case '.':
			continue
		case '9':
			b[i] = '0'
			continue
		}
		b[i]++
		return string(b)
	}
	return "1" + string(b)
}
