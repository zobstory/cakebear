package runtime

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// accPieces are what the tests append: ASCII, multi-byte characters, a
// surrogate pair, its halves alone (WTF-8, as the scanner stores "\uD83D"),
// and long pieces that cross accMin on their own.
var accPieces = []string{
	"", "a", "<url>", "é", "日本", "😀", "\xed\xa0\xbd", "\xed\xb8\x80", "-",
	strings.Repeat("x", 100), strings.Repeat("y", accMin), strings.Repeat("ü", 70),
}

// TestAccMatchesStrConcat: every result is StrConcat's, through appends to
// the newest string, to an older one (an alias), to a string from elsewhere
// (a reset), and across a split surrogate pair; and Length is StrLength's.
func TestAccMatchesStrConcat(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewPCG(1, 13))
	for run := range 200 {
		var a StrAcc
		s := ""
		var older []string
		for step := range 300 {
			switch k := r.IntN(20); {
			case k == 0:
				s = accPieces[r.IntN(len(accPieces))] // another write to the binding
			case k == 1 && len(older) > 0:
				s = older[r.IntN(len(older))] // an alias read back
			}
			n := 1 + r.IntN(3)
			parts := make([]string, n)
			for i := range parts {
				parts[i] = accPieces[r.IntN(len(accPieces))]
			}
			want := StrConcat(append([]string{s}, parts...)...)
			got := a.Append(s, parts...)
			if got != want {
				t.Fatalf("run %d step %d: Append(%q, %q) = %q, want %q", run, step, s, parts, got, want)
			}
			if gl, wl := a.Length(got), StrLength(got); gl != wl {
				t.Fatalf("run %d step %d: Length = %v, StrLength = %v", run, step, gl, wl)
			}
			older = append(older, got)
			s = got
		}
	}
}

// TestAccNeverChangesAString: a string the accumulator handed out keeps its
// bytes whatever is appended after it, to it, to the newest string, or to an
// older one, since appends write only past the newest string's end.
func TestAccNeverChangesAString(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewPCG(2, 29))
	var a StrAcc
	type kept struct{ s, copied string }
	var all []kept
	s := strings.Repeat("k", accMin)
	for range 2000 {
		if len(all) > 0 && r.IntN(8) == 0 {
			s = all[r.IntN(len(all))].s // append to an older string
		}
		s = a.Append(s, accPieces[r.IntN(len(accPieces))], "z")
		all = append(all, kept{s, strings.Clone(s)})
	}
	for i, k := range all {
		if k.s != k.copied {
			t.Fatalf("string %d changed after it was handed out: %q, was %q", i, k.s, k.copied)
		}
	}
}

// TestAccAppendsInPlace: a loop of appends to the newest string allocates a
// few times as the buffer doubles, not once per append, which is what makes
// it linear. 10,000 appends of 119 bytes allocated 10,000 times before.
func TestAccAppendsInPlace(t *testing.T) { //nolint:paralleltest // AllocsPerRun panics in a parallel test
	piece := strings.Repeat("p", 119)
	allocs := testing.AllocsPerRun(5, func() {
		var a StrAcc
		s := ""
		for range 10000 {
			s = a.Append(s, piece)
		}
	})
	if allocs > 40 {
		t.Errorf("10,000 appends allocated %v times, want a few dozen at most", allocs)
	}
}

// TestAccCachesTheLength: Length knows an ASCII newest string's length
// without StrLength's scan, and stops trusting it once the string has
// anything but ASCII or isn't the newest.
func TestAccCachesTheLength(t *testing.T) {
	t.Parallel()

	var a StrAcc
	s := a.Append(strings.Repeat("a", accMin), "b")
	if !a.asc || !a.newest(s) {
		t.Fatalf("after an ASCII append: asc %v, newest %v; want both", a.asc, a.newest(s))
	}
	older := s
	s = a.Append(s, "c")
	if a.newest(older) {
		t.Errorf("a string with an append after it is still the newest")
	}
	s = a.Append(s, "é")
	if a.asc {
		t.Errorf("after appending é, the buffer is still taken to be ASCII")
	}
	if got := a.Length(s); got != float64(accMin+3) {
		t.Errorf("Length = %v, want %d", got, accMin+3)
	}
}
