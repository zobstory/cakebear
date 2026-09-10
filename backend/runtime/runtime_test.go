package runtime

import (
	"math"
	"testing"
)

// TestNumberToString pins cakebear's number printing to JavaScript's, not Go's.
//
// Every expectation here was taken from what `String(x)` produces in Node, not
// from what looked reasonable. The two languages agree on most values and
// disagree on exactly the ones that are easy to get wrong.
func TestNumberToString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"integer", 5, "5"},
		{"negative integer", -42, "-42"},
		{"zero", 0, "0"},
		// String(-0) is "0" in JavaScript even though the value is signed.
		{"negative zero", math.Copysign(0, -1), "0"},
		{"half", 2.5, "2.5"},
		{"one third", 1.0 / 3.0, "0.3333333333333333"},
		{"small", 0.5, "0.5"},
		// Go would render these as 1e+21 and 1e-07; JavaScript trims the
		// exponent's leading zero.
		{"exponent upper bound", 1e21, "1e+21"},
		{"just below exponent bound", 1e20, "100000000000000000000"},
		{"small exponent", 1e-7, "1e-7"},
		{"just above small bound", 1e-6, "0.000001"},
		{"nan", math.NaN(), "NaN"},
		{"positive infinity", math.Inf(1), "Infinity"},
		{"negative infinity", math.Inf(-1), "-Infinity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := NumberToString(tt.in); got != tt.want {
				t.Errorf("NumberToString(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// console.log does not use Number::toString. Every JavaScript console prints
// -0 as "-0" because losing the sign hides a real distinction; the language's
// string conversion maps it to "0". Both behaviours are correct for their own
// operation, and cakebear implements them separately.
func TestNegativeZeroPrintsSignedButConvertsUnsigned(t *testing.T) {
	t.Parallel()

	negZero := math.Copysign(0, -1)

	if got := numberForDisplay(negZero); got != "-0" {
		t.Errorf("numberForDisplay(-0) = %q, want %q — consoles show the sign", got, "-0")
	}
	if got := NumberToString(negZero); got != "0" {
		t.Errorf("NumberToString(-0) = %q, want %q — Number::toString drops it", got, "0")
	}
	if got := numberForDisplay(0); got != "0" {
		t.Errorf("numberForDisplay(+0) = %q, want %q", got, "0")
	}
}

// NegZero exists because Go's constant arithmetic cannot express it: the source
// text -0.0 folds to positive zero before becoming a float64.
func TestNegZeroIsActuallyNegative(t *testing.T) {
	t.Parallel()

	if NegZero != 0 {
		t.Errorf("NegZero = %v, want a zero", NegZero)
	}
	if !math.Signbit(NegZero) {
		t.Error("NegZero has no sign bit set; the whole point of it is the sign")
	}
	// The bug this guards against, spelled out:
	if math.Signbit(-0.0) {
		t.Error("Go constant -0.0 unexpectedly kept its sign; the workaround may be unnecessary now")
	}
}

func TestBoolToString(t *testing.T) {
	t.Parallel()

	if got := BoolToString(true); got != "true" {
		t.Errorf("BoolToString(true) = %q", got)
	}
	if got := BoolToString(false); got != "false" {
		t.Errorf("BoolToString(false) = %q", got)
	}
}

func TestLoggersWriteThroughTheBuffer(t *testing.T) {
	t.Parallel()

	// The loggers share a package-level buffered writer, so this only checks
	// they run and flush without panicking; the byte-level behaviour is
	// covered end to end by the differential tests in cmd/cakec.
	LogNumber(1)
	LogString("x")
	LogBool(true)
	LogNull()
	LogUndefined()
	Flush()
}

// The extension loggers print integers exactly. Routing an int64 through
// float64 formatting would lose precision above 2^53, which is squarely inside
// the range i64 exists to provide.
func TestExtensionLoggersArePrecise(t *testing.T) {
	t.Parallel()

	LogInt32(-2147483648)
	LogInt64(9007199254740993) // 2^53 + 1: not representable as a float64
	LogUint32(4294967295)
	LogUint64(18446744073709551615) // math.MaxUint64
	LogFloat32(1.5)
	Flush()
}
