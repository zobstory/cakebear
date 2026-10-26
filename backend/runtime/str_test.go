package runtime

import (
	"math"
	"slices"
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
	if got := StrRepeat(lowHalf+highHalf, 2, nil); got != lowHalf+"😀"+highHalf {
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

// The byte paths and the code-unit paths must agree: an ASCII pattern
// searched for as bytes, and the same search on code units, from every
// position, in strings holding characters of every width, halves, stray
// bytes, and two halves that pair as separate WTF-8, as a request's URL can.
// The goldens can hold neither of the last two: Node never makes them.
func TestStrASCIIPathsAgreeWithUnits(t *testing.T) {
	t.Parallel()

	units := func(s string) jsStr { return jsStr{s: s, units: toUnits(s)} }
	strs := []string{
		"", "a", "ab", "abcabc", "aaa", "hello world", "a,b,,c,", "é,x", "日本,x,", "😀x😀", "x" + highHalf + "x",
		lowHalf + "x" + highHalf, highHalf + "," + lowHalf, "\xffx\xff", "\xed\xa0x", "x\xf0\x9f", "aaaaaaaé,aaaaaaaaa😀,x",
		"x," + highHalf + lowHalf + ",x",
	}
	for _, s := range strs {
		v := units(s)
		n := v.len()
		for _, w := range []string{"a", "x", ",", "ab", "aaaa", ",x", "x,"} {
			u := units(w)
			m := u.len()
			for from := 0; from <= len(s)+1; from++ {
				i := min(from, n)
				if got, want := indexASCII(s, w, from), indexOf(v, u, i); got != want {
					t.Errorf("indexOf(%q, %q, %d) = %d as bytes, %d as units", s, w, from, got, want)
				}
				if got, want := includesASCII(s, w, from), indexOf(v, u, i) >= 0; got != want {
					t.Errorf("includes(%q, %q, %d) = %v as bytes, %v as units", s, w, from, got, want)
				}
				last := -1
				for j := min(i, n-m); j >= 0 && last < 0; j-- {
					if equalAt(v, u, j) {
						last = j
					}
				}
				if got := lastIndexASCII(s, w, from); got != last {
					t.Errorf("lastIndexOf(%q, %q, %d) = %d as bytes, %d as units", s, w, from, got, last)
				}
				if got, want := startsWithASCII(s, w, from), i+m <= n && equalAt(v, u, i); got != want {
					t.Errorf("startsWith(%q, %q, %d) = %v as bytes, %v as units", s, w, from, got, want)
				}
				if got, want := endsWithASCII(s, w, from), i >= m && equalAt(v, u, i-m); got != want {
					t.Errorf("endsWith(%q, %q, %d) = %v as bytes, %v as units", s, w, from, got, want)
				}
			}
			var parts []string
			var positions []int
			i := 0
			for j := indexOf(v, u, 0); j >= 0; j = indexOf(v, u, i) {
				parts, positions = append(parts, v.sub(i, j)), append(positions, j)
				i = j + m
			}
			if parts = append(parts, v.sub(i, n)); !slices.Equal(StrSplit(s, w).items, parts) {
				t.Errorf("split(%q, %q) = %q, want %q, as by units", s, w, StrSplit(s, w).items, parts)
			}
			for _, r := range []string{"-", "$&$`$'", lowHalf + "$`" + highHalf} {
				want := s
				if positions != nil {
					want = replaceAt(v, m, positions, parseTemplate(r))
				}
				if got := StrReplaceAll(s, w, r); got != want {
					t.Errorf("replaceAll(%q, %q, %q) = %q, want %q, as by units", s, w, r, got, want)
				}
			}
		}
		for to := 0; to <= n; to++ {
			if b, at := unitOffset(s, to); utf16Len(s[:b]) != at || at < to || at > to+1 {
				t.Errorf("unitOffset(%q, %d) = %d, %d", s, to, b, at)
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

// Each throws Node's error, verbatim (M6 plan D9); a string longer than V8
// makes is one, however it would be made.
func TestStrThrowsMatchNode(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		do   func()
		want string // "" means it must not throw
	}{
		{"repeat -1", func() { StrRepeat("ab", -1, nil) }, "RangeError: Invalid count value: -1"},
		{"repeat -1.5", func() { StrRepeat("ab", -1.5, nil) }, "RangeError: Invalid count value: -1.5"},
		{"repeat Infinity", func() { StrRepeat("", math.Inf(1), nil) }, "RangeError: Invalid count value: Infinity"},
		{"repeat -0.5", func() { StrRepeat("ab", -0.5, nil) }, ""},
		{"repeat NaN", func() { StrRepeat("ab", math.NaN(), nil) }, ""},
		{"repeat too long", func() { StrRepeat("ab", 1<<28, nil) },
			"RangeError: Invalid string length"},
		{"empty repeat, long", func() { StrRepeat("", 1e9, nil) }, ""},
		{"padStart Infinity", func() { StrPadStart("a", math.Inf(1)) },
			"RangeError: Invalid string length"},
		{"padStart Infinity, no fill", func() { StrPadStart("a", math.Inf(1), "") }, ""},
		{"replaceAll too long", func() { StrReplaceAll(strings.Repeat("a", 1<<20), "a", strings.Repeat("$&", 600)) },
			"RangeError: Invalid string length"},
		// Counted in code units once the bytes, 577765376 and 805355520, say
		// it could be too long; $` is as long as the units before each match.
		{"replaceAll too long, not ASCII", func() { StrReplaceAll(strings.Repeat("éa", 1<<19), "a", strings.Repeat("$&", 1100)) },
			"RangeError: Invalid string length"},
		{"replaceAll of $` too long, not ASCII", func() { StrReplaceAll(strings.Repeat("éa", 1<<13), "a", strings.Repeat("$`", 8)) },
			"RangeError: Invalid string length"},
		{"at the limit", func() { checkLength(stringLimit, nil) }, ""},
		{"past the limit", func() { checkLength(stringLimit+1, nil) }, "RangeError: Invalid string length"},
	} {
		if got := throws(c.do); got != c.want {
			t.Errorf("%s: threw %q, want %q", c.name, got, c.want)
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
