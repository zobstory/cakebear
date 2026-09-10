// Package runtime is linked into every binary cakebear compiles.
//
// It lives under backend/ rather than at the repo root because go:embed cannot
// traverse "..", and backend/ has to embed this source to write it into the
// generated module. Keeping it a real compiled package rather than a template
// file means `go build ./...` and CI check it like any other code -- a syntax
// error here surfaces in our tests, not in a user's build.
//
// It depends on nothing outside the standard library, and must stay that way:
// the generated module is built in a temp directory with no network access.
package runtime

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
)

// out is buffered because a program that logs in a loop would otherwise pay a
// write syscall per line. Flush runs from the generated main via defer.
var out = bufio.NewWriter(os.Stdout)

// Flush writes anything still buffered. The generated program defers this.
func Flush() { _ = out.Flush() }

// NegZero is IEEE-754 negative zero.
//
// Generated code cannot write it as a literal: Go's untyped constant
// arithmetic has no signed zero, so the source text `-0.0` folds to positive
// zero before it ever becomes a float64. Producing it requires a runtime value,
// which is what this is.
var NegZero = math.Copysign(0, -1)

// NumberToString implements JavaScript's Number-to-String conversion.
//
// This exists because Go and JavaScript disagree about how a float64 prints,
// and TypeScript's `number` is a float64. `console.log(5)` must produce "5";
// Go's %v gives "5" too, but 1e21 gives "1e+21" both places while 1e-7 gives
// Go "1e-07" against JavaScript's "1e-7", and -0 prints as "-0" in Go and "0"
// in JavaScript. Those gaps are small and would be found by a user rather than
// by us, which is the worst way to find them.
//
// See CLAUDE.md, "Semantics decisions", item 2.
func NumberToString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		// Covers -0. ECMAScript's Number::toString maps both zeroes to "0",
		// so `"" + -0` is "0". console.log does NOT go through that -- see
		// numberForDisplay.
		return "0"
	}

	abs := math.Abs(f)

	// JavaScript switches to exponential notation outside [1e-6, 1e21).
	if abs >= 1e21 || abs < 1e-6 {
		return jsExponential(f)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// jsExponential converts Go's exponential form to JavaScript's.
//
// Go zero-pads the exponent to two digits ("1e-07"); JavaScript does not
// ("1e-7"). Both use an explicit "+" for positive exponents.
func jsExponential(f float64) string {
	s := strconv.FormatFloat(f, 'e', -1, 64)

	i := strings.IndexByte(s, 'e')
	if i < 0 {
		return s
	}
	mantissa, exp := s[:i], s[i+1:]

	sign := ""
	if len(exp) > 0 && (exp[0] == '+' || exp[0] == '-') {
		sign, exp = string(exp[0]), exp[1:]
	}
	exp = strings.TrimLeft(exp, "0")
	if exp == "" {
		exp = "0"
	}
	return mantissa + "e" + sign + exp
}

// BoolToString matches JavaScript, which is also what Go produces, but going
// through a named function keeps the generated code uniform across types.
func BoolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func LogNumber(f float64) { writeLine(numberForDisplay(f)) }

// numberForDisplay is console.log's conversion, which is deliberately not
// NumberToString.
//
// ECMAScript's Number::toString maps -0 to "0", so `"" + -0` is "0". But every
// JavaScript console -- Node's util.inspect, and browsers -- prints -0 as "-0",
// because losing the sign of a zero hides a real distinction while debugging.
// cakebear follows the consoles: a developer who runs the same program under
// node and under cakec should see the same bytes.
//
// Found by the differential harness in cmd/cakec, which is exactly the class of
// divergence it exists to catch.
func numberForDisplay(f float64) string {
	if f == 0 && math.Signbit(f) {
		return "-0"
	}
	return NumberToString(f)
}
func LogString(s string) { writeLine(s) }
func LogBool(b bool)     { writeLine(BoolToString(b)) }
func LogNull()           { writeLine("null") }
func LogUndefined()      { writeLine("undefined") }

func writeLine(s string) {
	_, _ = out.WriteString(s)
	_ = out.WriteByte('\n')
}
