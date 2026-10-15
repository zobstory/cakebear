package runtime

import (
	"math"
	"strings"
	"testing"
)

// The halves of 😀 (U+1F600, the pair D83D DE00) as the runtime holds them.
const (
	highHalf = "\xed\xa0\xbd"
	lowHalf  = "\xed\xb8\x80"
)

// Cutting a pair leaves its halves as WTF-8, and joining them makes the
// four bytes of the character again, so the result is == to the literal.
func TestStrHalvesRoundTrip(t *testing.T) {
	t.Parallel()

	if got := StrSlice("😀", 0, 1); got != highHalf {
		t.Errorf("slice(0, 1) = % x, want % x", got, highHalf)
	}
	if got := StrCharAt("a😀", 2); got != lowHalf {
		t.Errorf("charAt(2) = % x, want % x", got, lowHalf)
	}
	for _, parts := range [][]string{
		{highHalf, lowHalf},
		{highHalf, "", "", lowHalf},
		{StrSlice("😀", 0, 1), StrSlice("😀", 1)},
		StrSplit("😀", "").items,
	} {
		if got := StrConcat(parts...); got != "😀" {
			t.Errorf("concat of % x = % x, want the four bytes of 😀", parts, got)
		}
	}
	if got := StrConcat(lowHalf, highHalf); got != lowHalf+highHalf {
		t.Errorf("a low half then a high one must stay apart, got % x", got)
	}
	if got := StrReplace(highHalf+"-", "-", lowHalf); got != "😀" {
		t.Errorf("replace across the seam = % x, want 😀", got)
	}
	if got := StrRepeat(lowHalf+highHalf, 2); got != lowHalf+"😀"+highHalf {
		t.Errorf("repeat across the seam = % x", got)
	}
	if got := StrPadEnd(highHalf, 2, lowHalf); got != "😀" {
		t.Errorf("padEnd across the seam = % x, want 😀", got)
	}
}

// A half is one code unit, wherever a length is counted.
func TestStrHalfIsOneUnit(t *testing.T) {
	t.Parallel()

	for s, want := range map[string]int{
		"": 0, "abc": 3, "é": 1, "日本": 2, "😀": 2, highHalf: 1, "a" + lowHalf + "b": 3, highHalf + lowHalf: 2, "\xff": 1,
	} {
		if got := StrLength(s); got != float64(want) {
			t.Errorf("length of % x = %v, want %d", s, got, want)
		}
		if got := utf16Len(s); got != want {
			t.Errorf("utf16Len of % x = %d, want %d", s, got, want)
		}
	}
}

// The ASCII paths and the code-unit paths must agree: the same strings,
// searched and cut once as bytes and once as units.
func TestStrASCIIPathsAgreeWithUnits(t *testing.T) {
	t.Parallel()

	units := func(s string) jsStr { return jsStr{s: s, units: toUnits(s)} }
	strs := []string{"", "a", "ab", "abcabc", "aaa", "hello world", "a,b,,c,"}
	for _, s := range strs {
		for _, w := range strs {
			for from := 0; from <= len(s); from++ {
				for _, pair := range [][2]jsStr{{units(s), newJSStr(w)}, {newJSStr(s), units(w)}, {units(s), units(w)}} {
					if got, want := indexOf(pair[0], pair[1], from), indexOf(newJSStr(s), newJSStr(w), from); got != want {
						t.Errorf("indexOf(%q, %q, %d) = %d as units, %d as bytes", s, w, from, got, want)
					}
				}
				for to := from; to <= len(s); to++ {
					if got := units(s).sub(from, to); got != s[from:to] {
						t.Errorf("%q.sub(%d, %d) = %q as units", s, from, to, got)
					}
				}
			}
		}
	}
}

// UTF-16 order differs from byte order where the BMP above U+E000 meets a
// character outside it, and where a half meets a pair it starts.
func TestStrCompareIsUTF16Order(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		a, b string
		want int
	}{
		{"\uFFFF", "😀", 1}, // bytes say -1
		{"\uE000", "\U00010000", 1},
		{"\uD7FF", "😀", -1},
		{highHalf + "\uFFFF", "😀", 1}, // bytes say -1
		{highHalf, "😀", -1},
		{"a", "b", -1},
		{"ab", "a", 1},
		{"é", "é", 0},
		{highHalf + lowHalf, "😀", 0}, // the same code units
	} {
		if got := StrCompare(c.a, c.b); got != c.want {
			t.Errorf("StrCompare(% x, % x) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := StrCompare(c.b, c.a); got != -c.want {
			t.Errorf("StrCompare(% x, % x) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

// Each panic carries Node's error, verbatim, and says what happened.
func TestStrPanicsMatchNode(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		do   func()
		want string // "" means it must not panic
	}{
		{"repeat -1", func() { StrRepeat("ab", -1) }, "cakebear: repeat can't repeat a string -1 times (Node throws RangeError: Invalid count value: -1)"},
		{"repeat -1.5", func() { StrRepeat("ab", -1.5) }, "cakebear: repeat can't repeat a string -1.5 times (Node throws RangeError: Invalid count value: -1.5)"},
		{"repeat Infinity", func() { StrRepeat("", math.Inf(1)) }, "cakebear: repeat can't repeat a string Infinity times (Node throws RangeError: Invalid count value: Infinity)"},
		{"repeat -0.5", func() { StrRepeat("ab", -0.5) }, ""},
		{"repeat NaN", func() { StrRepeat("ab", math.NaN()) }, ""},
		{"repeat too long", func() { StrRepeat("ab", 1<<28) },
			"cakebear: the string would be 536870912 code units long, past V8's limit of 536870888 (Node throws RangeError: Invalid string length)"},
		{"empty repeat, long", func() { StrRepeat("", 1e9) }, ""},
		{"padStart Infinity", func() { StrPadStart("a", math.Inf(1)) },
			"cakebear: the string would be 9007199254740991 code units long, past V8's limit of 536870888 (Node throws RangeError: Invalid string length)"},
		{"padStart Infinity, no fill", func() { StrPadStart("a", math.Inf(1), "") }, ""},
		{"replaceAll too long", func() { StrReplaceAll(strings.Repeat("a", 1<<20), "a", strings.Repeat("$&", 600)) },
			"cakebear: the string would be 629145600 code units long, past V8's limit of 536870888 (Node throws RangeError: Invalid string length)"},
		{"at the limit", func() { checkLength(stringLimit) }, ""},
		{"past the limit", func() { checkLength(stringLimit + 1) }, "(Node throws RangeError: Invalid string length)"},
	} {
		got := panicked(c.do)
		switch {
		case c.want == "" && got != "":
			t.Errorf("%s: panicked with %q", c.name, got)
		case c.want != "" && !strings.HasSuffix(got, c.want):
			t.Errorf("%s: panic = %q, want %q", c.name, got, c.want)
		}
	}
}

// String() of an extension type prints as console.log prints the value.
func TestIntToStringMatchesConsoleLog(t *testing.T) {
	t.Parallel()

	for _, v := range []int64{0, 1, -1, math.MaxInt32, math.MinInt32, math.MaxInt64, math.MinInt64, 1 << 53, -(1 << 53) - 1} {
		if got, want := IntToString(v), Inspect(v); got != want {
			t.Errorf("IntToString(%d) = %q, inspect prints %q", v, got, want)
		}
		if v >= math.MinInt32 && v <= math.MaxInt32 {
			if got, want := IntToString(v), Inspect(int32(v)); got != want {
				t.Errorf("IntToString(%d) = %q, inspect prints the i32 as %q", v, got, want)
			}
		}
	}
	for _, v := range []uint64{0, 1, math.MaxUint32, math.MaxUint64, 1 << 63} {
		if got, want := UintToString(v), Inspect(v); got != want {
			t.Errorf("UintToString(%d) = %q, inspect prints %q", v, got, want)
		}
		if v <= math.MaxUint32 {
			if got, want := UintToString(v), Inspect(uint32(v)); got != want {
				t.Errorf("UintToString(%d) = %q, inspect prints the u32 as %q", v, got, want)
			}
		}
	}
}

// An ASCII string's case goes through the strings package, which is right
// only because ASCII's case is the same everywhere; a sigma needs context.
func TestStrCaseKeyOutputs(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ in, upper, lower string }{
		{"Hello", "HELLO", "hello"},
		{"ß", "SS", "ß"},
		{"ﬁ", "FI", "ﬁ"},
		{"İ", "İ", "i\u0307"},
		{"ΑΣ", "ΑΣ", "ας"},
		{"ΑΣΑ", "ΑΣΑ", "ασα"},
		{"Σ", "Σ", "σ"},
		{"Α'Σ", "Α'Σ", "α'ς"},
		{"\u0345Σ", "\u0399Σ", "\u0345σ"}, // both cased and ignorable: ignorable
		{highHalf + "Σ", highHalf + "Σ", highHalf + "σ"},
	} {
		if got := StrToUpperCase(c.in); got != c.upper {
			t.Errorf("%q.toUpperCase() = %q, want %q", c.in, got, c.upper)
		}
		if got := StrToLowerCase(c.in); got != c.lower {
			t.Errorf("%q.toLowerCase() = %q, want %q", c.in, got, c.lower)
		}
	}
}
