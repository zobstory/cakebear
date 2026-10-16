package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// arithProgram is %, and arithmetic on cakebear's numeric types, which is a
// number's arithmetic except inside the type's own conversion. Node 24.18 ran
// it with a shim for the conversions, as JavaScript would write them
// (`i32 = x => x | 0`, `u32 = x => x >>> 0`, `f32 = Math.fround`), and
// printed nodeArith. Before, % was refused, `const x: number = a / b` printed
// 3 for 7 and 2, the sum of two large i32s printed wrapped, a comparison of
// one with a number was refused, and f32(1.0000000596046448) was
// 1.0000001192092896, where Math.fround gives 1. The last lines hold the
// edges of what is native: i32(a / b + a / b) is 7, not 6, since a quotient
// below the argument isn't truncated, and f32(p * q + r) rounds once, where
// float32 arithmetic would round the product first and give
// 0.956297755241394. mulAdds holds the four forms Go may fuse into one
// multiply-add on arm64, each with operands where fusing changes the last
// bit: every product was emitted bare from P4 on, so the first printed
// -0.1964400794061956, where Node prints -0.19644007940619557.
const arithProgram = "export {};\nconst inf: number = 1 / 0;\nconst xs: number[] = [7, -7, 7.5, -7.5, 0, -0, 0.1, 1e308, 5e-324, inf, -inf, 0 / 0];\nconst ys: number[] = [2, -2, 0.3, 0, -0, inf, 0 / 0, 5e-324];\nfor (const x of xs) {\n  const row: number[] = [];\n  for (const y of ys) {\n    row.push(x % y);\n  }\n  console.log(row);\n}\nlet n: number = 17;\nn %= 5;\nconsole.log(n, 5.5 % 2, -5.5 % 2, 0.1 % 0.01, 1 % 0, -1 % inf);\nconst a: i32 = i32(7);\nconst b: i32 = i32(2);\nconst z: i32 = i32(0);\nconst big: i32 = i32(2147483647);\nconst x: number = a / b;\nconsole.log(x, a / b, -a / b, big + big, big * big, a % b, -big - big, a / z, a % z);\nconsole.log(a < b, big + big > big, a + 1.5, 0.5 * a, `${a / b}`, a === b, a !== b);\nconsole.log(i32(a / b), i32(-a / b), i32(big + big), i32(big + i32(1)), i32(-i32(-2147483648)), i32(a % b), i32(-a % b));\nconsole.log(i32(a * b + big), i32((a + b) / b), i32(-(a + b)));\nconst u: u32 = u32(4294967295);\nconsole.log(u + u, u32(u + u), u32(u + u32(1)), u32(i32(-1)), u / u32(2), u32(u / u32(2)), u32(u % u32(10)));\nconst f: f32 = f32(0.1);\nconst g: f32 = f32(0.2);\nconsole.log(f + g, f32(f + g), f * g, f32(f * g), f % g, f32(f % g), f32(-f));\nconsole.log(f32(1.0000000596046448), f32(16777217));\nconst minI: i32 = i32(-2147483648);\nconsole.log(i32(a / b + a / b), -minI, i32(-minI));\nconst p: f32 = f32(0.9103208184242249);\nconst q: f32 = f32(0.7621980309486389);\nconst r: f32 = f32(0.26245298981666565);\nconsole.log(f32(p * q + r), f32(p * q), p * q + r);\nfunction mulAdds(t: number[]): number[] {\n  return [t[0] * t[1] + t[2], t[0] * t[1] - t[2], t[2] - t[0] * t[1], -(t[0] * t[1]) - t[2]];\n}\nconsole.log(mulAdds([0.6764556596678251, 0.4613862177205994, -0.5085473976760264]));\nconsole.log(mulAdds([0.4421393690874835, 0.9313193434016267, 0.4015746755466586]));\nconsole.log(mulAdds([0.7985452988329941, 0.13566127705616127, 0.1300282455886671]));\nconsole.log(mulAdds([0.9225309633477059, 0.8382583877000379, 0.5059358910124774]));\n"

const nodeArith = "[ 1, 1, 0.10000000000000026, NaN, NaN, 7, NaN, 0 ]\n[ -1, -1, -0.10000000000000026, NaN, NaN, -7, NaN, -0 ]\n[ 1.5, 1.5, 2.7755575615628914e-16, NaN, NaN, 7.5, NaN, 0 ]\n[ -1.5, -1.5, -2.7755575615628914e-16, NaN, NaN, -7.5, NaN, -0 ]\n[\n    0, 0,   0, NaN,\n  NaN, 0, NaN,   0\n]\n[\n   -0, -0,  -0, NaN,\n  NaN, -0, NaN,  -0\n]\n[\n  0.1, 0.1, 0.1, NaN,\n  NaN, 0.1, NaN,   0\n]\n[ 0, 0, 0.06138802622817707, NaN, NaN, 1e+308, NaN, 0 ]\n[\n  5e-324, 5e-324,\n  5e-324,    NaN,\n     NaN, 5e-324,\n     NaN,      0\n]\n[\n  NaN, NaN, NaN,\n  NaN, NaN, NaN,\n  NaN, NaN\n]\n[\n  NaN, NaN, NaN,\n  NaN, NaN, NaN,\n  NaN, NaN\n]\n[\n  NaN, NaN, NaN,\n  NaN, NaN, NaN,\n  NaN, NaN\n]\n2 1.5 -1.5 3.469446951953614e-18 NaN -1\n3.5 3.5 -3.5 4294967294 4611686014132420600 1 -4294967294 Infinity NaN\nfalse true 8.5 3.5 3.5 false true\n3 -3 -2 -2147483648 -2147483648 1 -1\n-2147483635 4 -9\n8589934590 4294967294 0 4294967295 2147483647.5 2147483647 5\n0.30000000447034836 0.30000001192092896 0.020000000596046452 0.020000001415610313 0.10000000149011612 0.10000000149011612 -0.10000000149011612\n1 16777216\n7 2147483648 -2147483648\n0.9562976956367493 0.693844735622406 0.9562977251511633\n[\n  -0.19644007940619557,\n  0.8206547159458573,\n  -0.8206547159458573,\n  0.19644007940619557\n]\n[\n  0.8133476224572233,\n  0.010198271363906053,\n  -0.010198271363906053,\n  -0.8133476224572233\n]\n[\n  0.238359920615545,\n  -0.021696570561789197,\n  0.021696570561789197,\n  -0.238359920615545\n]\n[\n  1.279255208951688,\n  0.26738342692673334,\n  -0.26738342692673334,\n  -1.279255208951688\n]\n"

func TestArithmeticMatchesNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "arith", arithProgram), nodeArith, 3)
}

// TestNativeIntegerArithmetic: inside its own conversion, arithmetic on an
// integer type is the type's, exactly, where a number would round: an i32
// product is Math.imul's (Node's `(a * b) | 0` rounds the product first, and
// gives 0 here), and an i64 is exact past 2^53, as BigInt.asIntN(64, …)
// gives. i64(b + b) printed 18014398509481984 after PR 46, which took every
// conversion's argument through a float64; a literal of 2^62 was
// 4611686018427388000; and i32(i32(2147483647) + i32(1)) failed go build, a
// constant overflow, rather than wrap.
func TestNativeIntegerArithmetic(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"i32", "export {};\nconst big: i32 = i32(2147483647);\nconsole.log(i32(big * big), i32(i32(2147483647) + i32(1)), i32(-i32(-2147483648)));\n", "1 -2147483648 -2147483648\n"},
		{"i64", "export {};\nconst a: i64 = i64(4503599627370496);\nconst b: i64 = i64(i64(a + a) + i64(1));\nconst m: i64 = i64(4611686018427387904);\nconsole.log(i64(b + b), i64(m + i64(1)), i64(m * i64(2)), i64(-m));\n", "18014398509481986 4611686018427387905 -9223372036854775808 -4611686018427387904\n"},
		{"across types", "export {};\nconst huge: u64 = u64(18446744073709549568);\nconsole.log(huge, i64(huge), u32(i32(-1)), i32(huge));\n", "18446744073709549568 -2048 4294967295 -2048\n"},
	} {
		matchesNode(t, buildProgram(t, "native", tt.src), tt.want, 1)
	}
}

// TestIntegerDivisionByZeroPanics: / or % by zero inside an integer
// conversion panics saying what the number would have been, and the program
// exits 2, as a Go panic does. Outside one it is a number's: Infinity, NaN.
func TestIntegerDivisionByZeroPanics(t *testing.T) {
	t.Parallel()

	const decls = "const a: i32 = i32(7);\nconst z: i32 = i32(0);\nconst w: u64 = u64(0);\n"
	for _, tt := range []struct {
		name, src, want string
	}{
		{"i32 /", decls + "console.log(i32(a / z));\n", "cakebear: i32(a / b) with b zero: as a number it is Infinity, or NaN for 0 / 0, which i32 can't hold"},
		{"i32 %", decls + "console.log(i32(a % z));\n", "cakebear: i32(a % b) with b zero: as a number it is NaN, which i32 can't hold"},
		{"u64 /", decls + "console.log(u64(w / w));\n", "cakebear: u64(a / b) with b zero"},
	} {
		bin := buildProgram(t, "byzero", tt.src)
		var stderr strings.Builder
		cmd := exec.Command(bin)
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(stderr.String(), tt.want) {
			t.Errorf("%s: %v, stderr\n%s\nwant exit 2 and %q", tt.name, err, stderr.String(), tt.want)
		}
	}
}
