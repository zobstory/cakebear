package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// stringsProgram covers the M4 strings step (~/Desktop/cakebear/m4-strings-plan.md):
//   - template literals of strings, numbers and booleans, String() and
//     toString(), and toFixed's rounding, ties up;
//   - every method lowered, on ASCII, BMP and astral strings, with UTF-16
//     indices, negative, fractional, absent and past-the-end positions;
//   - a surrogate pair cut by slice or split and joined again, by +, by a
//     template and by join;
//   - full case mapping and final sigma, JavaScript's whitespace for trim,
//     and the $ patterns of a string replacement;
//   - comparison in UTF-16 order, where U+FFFF sorts above an astral
//     character;
//   - Number(), parseInt, parseFloat, the predicates and the URI functions.
//
// Node 24.18 ran this same file and printed nodeStrings, the same in 5 of 5
// runs.
const stringsProgram = `export {};
interface Item { readonly name: string; readonly price: number }
const items: readonly Item[] = [{ name: "green tea", price: 4.5 }, { name: "Straße cake", price: 2.25 }];
for (const { name, price } of items) {
  console.log(` + "`" + `${name.toUpperCase()}: $${price.toFixed(2)} (${name.length} chars)` + "`" + `);
}
const n: number = 3.14159;
const flag: boolean = false;
console.log("templates", ` + "`" + `a${n}b` + "`" + `, ` + "`" + `${flag}` + "`" + `, ` + "`" + `${1 / 3}` + "`" + `, ` + "`" + `${-0}` + "`" + `, ` + "`" + `${1e21}` + "`" + `, ` + "`" + `plain` + "`" + `);
console.log("String", String(n), String(flag), String(-0), String(), n.toString(), flag.toString());
console.log("toFixed", (0.5).toFixed(0), (2.5).toFixed(0), (1.25).toFixed(1), (1.005).toFixed(2), (-0.0001).toFixed(2), (-0).toFixed(2), (1e21).toFixed(2), n.toFixed());
const emoji: string = "a😀b";
console.log("length", emoji.length, "".length, "日本".length, emoji.charAt(1).length, emoji.charCodeAt(1), emoji.charCodeAt(9));
console.log("search", emoji.includes("😀"), emoji.indexOf("b"), emoji.lastIndexOf("a"), emoji.startsWith("😀", 1), emoji.endsWith("a", 1), "abcabc".indexOf("c", 3), "abc".includes("", 99));
console.log("slice", emoji.slice(1, 3), emoji.slice(-1), "hello".slice(1), "hello".slice(), "hello".slice(3, 1), "hello".substring(3, 1), "hello".substring(-2), "hello".slice(1.7, 3.2));
const half1: string = emoji.slice(0, 2);
const half2: string = emoji.slice(2);
console.log("halves", half1 + half2 === emoji, ` + "`" + `${half1}${half2}` + "`" + ` === emoji, [half1, half2].join("") === emoji, emoji.split("").join("") === emoji);
console.log("case", "Straße".toUpperCase(), "ΑΣ ΟΔΟΣ".toLowerCase(), "İ".toLowerCase().length, "ﬁ".toUpperCase(), "MiXeD".toLowerCase());
const spaced: string = "\u00A0\uFEFF x \u0085\u180E";
console.log("trim", JSON.stringify(spaced.trim()), JSON.stringify(spaced.trimStart()), JSON.stringify(spaced.trimEnd()), "  y  ".trim());
console.log("split", "a,b,,c".split(","), "a,b,c".split(",", 2), "abc".split(""), emoji.split(""), "".split(","), "abc".split("", 0));
console.log("replace", "aXbX".replace("X", "[$&]"), "aXbX".replaceAll("X", "$$"), "abc".replace("b", "$` + "`" + `|$'"), "abc".replace("b", "$1"), "abc".replace("", "-"), "aaa".replaceAll("a", "b"));
console.log("pad", "7".padStart(3, "0"), "ab".padEnd(5, "xy"), "abc".padStart(2), "x".padStart(4), "ab".repeat(3), "".repeat(5));
console.log("compare", "￿" < "😀", "😀" < "￿", "a" < "b", "B" < "a", "abc" >= "abd", "日" > "本");
console.log("Number", Number("42"), Number(" 0x10 "), Number(""), Number("1e3"), Number("12px"), Number("-Infinity"), Number("0b101"));
console.log("parse", parseInt("08"), parseInt("ff", 16), parseInt("0x1A"), parseInt("  -12.9e3"), parseInt("z", 37), parseFloat("3.5kg"), parseFloat(".5e1x"), parseFloat("Infinityy"));
console.log("predicates", Number.isFinite(n), Number.isNaN(Number("x")), Number.isInteger(4), Number.isInteger(4.5), isNaN(n), isFinite(1 / 0));
console.log("uri", encodeURIComponent("a b&c=d/é😀"), decodeURIComponent("a%20b%C3%A9"), encodeURIComponent(5), encodeURIComponent(true));
`

const nodeStrings = "GREEN TEA: $4.50 (9 chars)\nSTRASSE CAKE: $2.25 (11 chars)\ntemplates a3.14159b false 0.3333333333333333 0 1e+21 plain\nString 3.14159 false 0  3.14159 false\ntoFixed 1 3 1.3 1.00 -0.00 0.00 1e+21 3\nlength 4 0 2 1 55357 NaN\nsearch true 3 0 true true 5 true\nslice \U0001f600 b ello hello  el hello el\nhalves true true true true\ncase STRASSE \u03b1\u03c2 \u03bf\u03b4\u03bf\u03c2 2 FI mixed\ntrim \"x \u0085\u180e\" \"x \u0085\u180e\" \"\u00a0\ufeff x \u0085\u180e\" y\nsplit [ 'a', 'b', '', 'c' ] [ 'a', 'b' ] [ 'a', 'b', 'c' ] [ 'a', '\\ud83d', '\\ude00', 'b' ] [ '' ] []\nreplace a[X]bX a$b$ aa|cc a$1c -abc bbb\npad 007 abxyx abc    x ababab \ncompare false true true true false false\nNumber 42 16 0 1000 NaN -Infinity 5\nparse 8 255 26 -12 NaN 3.5 5 Infinity\npredicates true true true false false false\nuri a%20b%26c%3Dd%2F%C3%A9%F0%9F%98%80 a b\u00e9 5 true\n"

func TestStringsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "strings", stringsProgram), nodeStrings, 20)
}

// TestStringBuiltinsPanicAsNodeThrows: what Node throws for, cakebear panics
// for, with Node's message (exit 2, where Node exits 1), until M6 brings
// errors.
func TestStringBuiltinsPanicAsNodeThrows(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, expr, want string }{
		{"repeat a negative count", "\"ab\".repeat(-1)", "RangeError: Invalid count value: -1"},
		{"toFixed past 100 digits", "(1).toFixed(101)", "RangeError: toFixed() digits argument must be between 0 and 100"},
		{"decode a malformed escape", "decodeURIComponent(\"%E0%A4%A\")", "URIError: URI malformed"},
		{"encode a lone surrogate", "encodeURIComponent(\"\\uD800\")", "URIError: URI malformed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bin := buildProgram(t, "throws", "export {};\nconsole.log(\"before\");\nconsole.log("+tt.expr+");\n")
			var stdout, stderr strings.Builder
			cmd := exec.Command(bin)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 2 {
				t.Fatalf("%v, want exit 2; stderr:\n%s", err, stderr.String())
			}
			if got := strings.ReplaceAll(stderr.String(), "\n\t", "\n"); !strings.Contains(got, tt.want) {
				t.Errorf("stderr is\n%s\nwant it to hold Node's message %q", stderr.String(), tt.want)
			}
			if stdout.String() != "before\n" {
				t.Errorf("stdout = %q, want only what ran before the panic", stdout.String())
			}
		})
	}
}
