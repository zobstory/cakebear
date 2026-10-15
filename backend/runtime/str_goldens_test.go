package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// strEntry is one line of testdata/str-goldens.jsonl: a function, its
// arguments, and what Node made of them, a value or the RangeError it threw.
type strEntry struct {
	Fn     string            `json:"fn"`
	Args   []json.RawMessage `json:"args"`
	Out    json.RawMessage   `json:"out"`
	Throws *string           `json:"throws"`
}

// strArgs are an entry's decoded arguments.
type strArgs []any

func (a strArgs) str(i int) string { return a[i].(string) }

// nums are the numbers from i on, the optional arguments a call passed.
func (a strArgs) nums(i int) []float64 {
	var out []float64
	for _, v := range a[i:] {
		out = append(out, v.(float64))
	}
	return out
}

func (a strArgs) strs(i int) []string {
	var out []string
	for _, v := range a[i:] {
		out = append(out, v.(string))
	}
	return out
}

// strFns calls the runtime function each fn names, with an entry's
// arguments as compiled code passes them.
var strFns = map[string]func(a strArgs) any{
	"length":      func(a strArgs) any { return StrLength(a.str(0)) },
	"charAt":      func(a strArgs) any { return StrCharAt(a.str(0), a[1].(float64)) },
	"charCodeAt":  func(a strArgs) any { return StrCharCodeAt(a.str(0), a[1].(float64)) },
	"includes":    func(a strArgs) any { return StrIncludes(a.str(0), a.str(1), a.nums(2)...) },
	"startsWith":  func(a strArgs) any { return StrStartsWith(a.str(0), a.str(1), a.nums(2)...) },
	"endsWith":    func(a strArgs) any { return StrEndsWith(a.str(0), a.str(1), a.nums(2)...) },
	"indexOf":     func(a strArgs) any { return StrIndexOf(a.str(0), a.str(1), a.nums(2)...) },
	"lastIndexOf": func(a strArgs) any { return StrLastIndexOf(a.str(0), a.str(1), a.nums(2)...) },
	"slice":       func(a strArgs) any { return StrSlice(a.str(0), a.nums(1)...) },
	"substring":   func(a strArgs) any { return StrSubstring(a.str(0), a.nums(1)...) },
	"toLowerCase": func(a strArgs) any { return StrToLowerCase(a.str(0)) },
	"toUpperCase": func(a strArgs) any { return StrToUpperCase(a.str(0)) },
	"trim":        func(a strArgs) any { return StrTrim(a.str(0)) },
	"trimStart":   func(a strArgs) any { return StrTrimStart(a.str(0)) },
	"trimEnd":     func(a strArgs) any { return StrTrimEnd(a.str(0)) },
	"split":       func(a strArgs) any { return StrSplit(a.str(0), a.str(1), a.nums(2)...) },
	"replace":     func(a strArgs) any { return StrReplace(a.str(0), a.str(1), a.str(2)) },
	"replaceAll":  func(a strArgs) any { return StrReplaceAll(a.str(0), a.str(1), a.str(2)) },
	"padStart":    func(a strArgs) any { return StrPadStart(a.str(0), a[1].(float64), a.strs(2)...) },
	"padEnd":      func(a strArgs) any { return StrPadEnd(a.str(0), a[1].(float64), a.strs(2)...) },
	"repeat":      func(a strArgs) any { return StrRepeat(a.str(0), a[1].(float64)) },
	"compare":     func(a strArgs) any { return float64(StrCompare(a.str(0), a.str(1))) },
	"concat":      func(a strArgs) any { return StrConcat(a.strs(0)...) },
	"int": func(a strArgs) any {
		v, err := strconv.ParseInt(a.str(0), 10, 64)
		if err != nil {
			panic(err)
		}
		return IntToString(v)
	},
	"uint": func(a strArgs) any {
		v, err := strconv.ParseUint(a.str(0), 10, 64)
		if err != nil {
			panic(err)
		}
		return UintToString(v)
	},
	// writeLine writes what Format makes through wellFormed;
	// TestLogWritesWellFormed holds Log to that.
	"log":     func(a strArgs) any { return wellFormed(Format(a...)) },
	"inspect": func(a strArgs) any { return Inspect(a[0]) },
	"join":    func(a strArgs) any { return a[0].(interface{ Join(sep string) string }).Join(a.str(1)) },
}

// callStr runs one entry, with the message of a panic, if it panicked.
func callStr(fn func(strArgs) any, args strArgs) (out any, panicked any) {
	defer func() { panicked = recover() }()
	return fn(args), nil
}

// sameValue compares a result with what Node gave, NaN equal to NaN.
func sameValue(got, want any) bool {
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		return ok && (g == w && math.Signbit(g) == math.Signbit(w) || math.IsNaN(g) && math.IsNaN(w))
	case *Array[string]:
		g, ok := got.(*Array[string])
		return ok && slices.Equal(g.items, w.items)
	case *Array[any]: // an empty array, which the decoder can't type
		g, ok := got.(*Array[string])
		return ok && len(w.items) == 0 && len(g.items) == 0
	}
	return got == want
}

func show(v any) string {
	if a, ok := v.(*Array[string]); ok {
		return fmt.Sprintf("%q", a.items)
	}
	return fmt.Sprintf("%#v", v)
}

func checkStrEntry(t *testing.T, line int, e strEntry) bool {
	t.Helper()
	fn, ok := strFns[e.Fn]
	if !ok {
		t.Fatalf("line %d: no function %q", line, e.Fn)
	}
	d := &decoder{t: t, ids: map[int]any{}}
	args := make(strArgs, len(e.Args))
	for i, raw := range e.Args {
		args[i] = d.value(raw)
	}
	got, panicked := callStr(fn, args)
	switch {
	case e.Throws != nil:
		if msg, _ := panicked.(string); !strings.HasPrefix(msg, "cakebear: ") || !strings.HasSuffix(msg, "(Node throws "+*e.Throws+")") {
			t.Errorf("line %d: %s%s gave %s, panic %v\nwant the panic for what Node threw, %s", line, e.Fn, args, show(got), panicked, *e.Throws)
			return false
		}
	case panicked != nil:
		t.Errorf("line %d: %s%s panicked: %v", line, e.Fn, args, panicked)
		return false
	default:
		want := d.value(e.Out)
		if !sameValue(got, want) {
			t.Errorf("line %d: %s%q gave\n%s\nwant, as Node gives\n%s", line, e.Fn, []any(args), truncate(show(got), 300), truncate(show(want), 300))
			return false
		}
	}
	return true
}

// readStrGoldens calls each for every entry of the corpus, numbered from 1.
func readStrGoldens(t *testing.T, each func(line int, e strEntry)) {
	t.Helper()
	f, err := os.Open("testdata/str-goldens.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	n := 0
	for scanner.Scan() {
		n++
		var e strEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("line %d: %v", n, err)
		}
		each(n, e)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

type strIndex struct {
	Whitespace []rune `json:"whitespace"`
	Sections   map[string]struct {
		First int `json:"first"`
		Last  int `json:"last"`
		Count int `json:"count"`
	} `json:"sections"`
}

func readStrIndex(t *testing.T) strIndex {
	t.Helper()
	raw, err := os.ReadFile("testdata/str-goldens-index.json")
	if err != nil {
		t.Fatal(err)
	}
	var idx strIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		t.Fatal(err)
	}
	return idx
}

// TestStrGoldens: every entry of the corpus Node 24.18 recorded
// (testdata/str-goldens.jsonl, made by testdata/gen-str-goldens.mjs).
func TestStrGoldens(t *testing.T) {
	t.Parallel()

	n, failed, threw := 0, 0, 0
	fns := map[string]int{}
	readStrGoldens(t, func(line int, e strEntry) {
		n++
		fns[e.Fn]++
		if e.Throws != nil {
			threw++
		}
		if !checkStrEntry(t, line, e) {
			failed++
			if failed >= 20 {
				t.Fatalf("stopping after 20 mismatches, at line %d", line)
			}
		}
	})
	if n < 40000 || threw < 500 || len(fns) != len(strFns) {
		t.Errorf("the corpus has %d entries, %d of them throwing, of %d functions; want the whole of it", n, threw, len(fns))
	}
	t.Logf("%d entries, %d of them throwing, %d mismatched", n, threw, failed)
}

// Every code point the corpus doesn't list as changing case must map to
// itself: CASE_UPPER and CASE_LOWER hold every one that changes, found by
// trying them all. Halves too, which neither maps.
func TestStrCaseEveryCodePoint(t *testing.T) {
	t.Parallel()

	idx := readStrIndex(t)
	want := map[string]map[string]string{"toUpperCase": {}, "toLowerCase": {}}
	readStrGoldens(t, func(line int, e strEntry) {
		for _, section := range []string{"CASE_UPPER", "CASE_LOWER"} {
			if s := idx.Sections[section]; line >= s.First && line <= s.Last {
				d := &decoder{t: t, ids: map[int]any{}}
				want[e.Fn][d.value(e.Args[0]).(string)] = d.value(e.Out).(string)
			}
		}
	})
	if len(want["toUpperCase"]) != idx.Sections["CASE_UPPER"].Count || len(want["toLowerCase"]) != idx.Sections["CASE_LOWER"].Count {
		t.Fatalf("read %d and %d code points that change case, want the index's %d and %d", len(want["toUpperCase"]), len(want["toLowerCase"]),
			idx.Sections["CASE_UPPER"].Count, idx.Sections["CASE_LOWER"].Count)
	}
	bad := 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		s := string(r)
		if r >= 0xd800 && r <= 0xdfff {
			s = string(appendHalf(nil, r))
		}
		for fn, f := range map[string]func(string) string{"toUpperCase": StrToUpperCase, "toLowerCase": StrToLowerCase} {
			w, ok := want[fn][s]
			if !ok {
				w = s
			}
			if got := f(s); got != w {
				bad++
				if bad <= 10 {
					t.Errorf("U+%04X.%s() = %q, want %q", r, fn, got, w)
				}
			}
		}
	}
}

// Trim removes exactly JavaScript's whitespace, which the index lists from
// Node, and nothing else of all the code points.
func TestStrTrimEveryCodePoint(t *testing.T) {
	t.Parallel()

	idx := readStrIndex(t)
	if len(idx.Whitespace) < 25 {
		t.Fatalf("the index lists %d whitespace code points", len(idx.Whitespace))
	}
	space := map[rune]bool{}
	for _, r := range idx.Whitespace {
		space[r] = true
	}
	bad := 0
	for r := rune(0); r <= unicode.MaxRune && bad < 10; r++ {
		s := string(r)
		if r >= 0xd800 && r <= 0xdfff {
			s = string(appendHalf(nil, r))
		}
		want := s + "x" + s
		if space[r] {
			want = "x"
		}
		if got := StrTrim(s + "x" + s); got != want {
			bad++
			t.Errorf("U+%04X: trim gave %q, want %q", r, got, want)
		}
	}
}

// The exceptions were found against one version of Go's unicode tables;
// under another, the code points Go maps differently aren't in them.
// Regenerate with scripts/gen-case-table.mjs.
func TestStrCaseTableMatchesToolchain(t *testing.T) {
	t.Parallel()

	if unicode.Version != caseTableUnicode {
		t.Errorf("str_case_table.go was made against Unicode %s, and this Go's unicode package is %s: rerun scripts/gen-case-table.mjs",
			caseTableUnicode, unicode.Version)
	}
}
