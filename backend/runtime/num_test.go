package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

// numEntry is one line of testdata/num-goldens.jsonl or uri-goldens.jsonl:
// a function's name, its arguments, and its result under Node, or what it
// threw.
type numEntry struct {
	Fn     string            `json:"fn"`
	Args   []json.RawMessage `json:"args"`
	Out    json.RawMessage   `json:"out"`
	Throws *string           `json:"throws"`
}

// numCall is the runtime's function for an entry, called, with the message
// of a panic, if it panicked.
func numCall(t *testing.T, fn string, args []any) (out, panicked any) {
	t.Helper()
	defer func() { panicked = recover() }()
	switch fn {
	case "toFixed":
		return NumberToFixed(args[0].(float64), args[1].(float64)), nil
	case "Number":
		return StringToNumber(args[0].(string)), nil
	case "parseInt":
		if len(args) == 1 {
			return ParseInt(args[0].(string)), nil
		}
		return ParseInt(args[0].(string), args[1].(float64)), nil
	case "parseFloat":
		return ParseFloat(args[0].(string)), nil
	case "isFinite":
		return NumberIsFinite(args[0].(float64)), nil
	case "isNaN":
		return NumberIsNaN(args[0].(float64)), nil
	case "isInteger":
		return NumberIsInteger(args[0].(float64)), nil
	case "mod":
		return Mod(args[0].(float64), args[1].(float64)), nil
	case "encodeURIComponent":
		return EncodeURIComponent(args[0].(string)), nil
	case "decodeURIComponent":
		return DecodeURIComponent(args[0].(string)), nil
	}
	t.Fatalf("no function %q", fn)
	return nil, nil
}

// numSameValue is SameValue: NaN is NaN, and 0 and -0 differ.
func numSameValue(a, b any) bool {
	x, ok := a.(float64)
	y, ok2 := b.(float64)
	if ok && ok2 {
		return math.Float64bits(x) == math.Float64bits(y) || math.IsNaN(x) && math.IsNaN(y)
	}
	return a == b
}

// show is a value as a mismatch reports it, a number as JavaScript writes
// it, with its zero's sign.
func numShow(v any) string {
	if x, ok := v.(float64); ok {
		if x == 0 && math.Signbit(x) {
			return "-0"
		}
		return NumberToString(x)
	}
	return fmt.Sprintf("%q", v)
}

func checkNumEntry(t *testing.T, line int, e numEntry) bool {
	t.Helper()
	d := &decoder{t: t, ids: map[int]any{}}
	args := make([]any, len(e.Args))
	for i, raw := range e.Args {
		args[i] = d.value(raw)
	}
	got, panicked := numCall(t, e.Fn, args)
	call := fmt.Sprintf("line %d: %s%s", line, e.Fn, truncate(string(numJSON(t, e.Args)), 200))
	if e.Throws != nil {
		msg, ok := panicked.(string)
		if !ok || !strings.HasPrefix(msg, "cakebear: ") || !strings.HasSuffix(msg, " (Node throws "+*e.Throws+")") {
			t.Errorf("%s: gave %s, panic %v; want the panic for what Node threw, %s", call, numShow(got), panicked, *e.Throws)
			return false
		}
		return true
	}
	if panicked != nil {
		t.Errorf("%s: panicked: %v", call, panicked)
		return false
	}
	if want := d.value(e.Out); !numSameValue(got, want) {
		t.Errorf("%s: gave %s, want %s, as Node gives", call, numShow(got), numShow(want))
		return false
	}
	return true
}

func numJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// runNumGoldens checks every entry of a corpus, and gives how many there were
// of each function, and how many of them threw.
func runNumGoldens(t *testing.T, path string) (counts, throws map[string]int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	counts, throws = map[string]int{}, map[string]int{}
	n, failed := 0, 0
	for scanner.Scan() {
		n++
		var e numEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("line %d: %v", n, err)
		}
		counts[e.Fn]++
		if e.Throws != nil {
			throws[e.Fn]++
		}
		if !checkNumEntry(t, n, e) {
			failed++
			if failed >= 20 {
				t.Fatalf("stopping after 20 mismatches, at line %d", n)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d entries, %d mismatched: %v, throwing %v", n, failed, counts, throws)
	return counts, throws
}

// TestNumGoldens: every entry of the corpus of toFixed, Number, parseInt,
// parseFloat and the Number predicates that Node 24.18 gave
// (testdata/num-goldens.jsonl, made by testdata/gen-num-goldens.mjs).
func TestNumGoldens(t *testing.T) {
	t.Parallel()

	counts, throws := runNumGoldens(t, "testdata/num-goldens.jsonl")
	for fn, least := range map[string]int{
		"toFixed": 20000, "Number": 4000, "parseFloat": 4000, "parseInt": 10000,
		"isFinite": 200, "isNaN": 200, "isInteger": 200,
	} {
		if counts[fn] < least {
			t.Errorf("the corpus has %d entries of %s; want the whole of it, at least %d", counts[fn], fn, least)
		}
	}
	if throws["toFixed"] < 100 {
		t.Errorf("the corpus has %d toFixed calls that threw; want its digit counts out of range", throws["toFixed"])
	}
}

// TestURIGoldens: every entry of the corpus of encodeURIComponent and
// decodeURIComponent that Node 24.18 gave (testdata/uri-goldens.jsonl).
func TestURIGoldens(t *testing.T) {
	t.Parallel()

	counts, throws := runNumGoldens(t, "testdata/uri-goldens.jsonl")
	for fn, least := range map[string]int{"encodeURIComponent": 3000, "decodeURIComponent": 10000} {
		if counts[fn] < least || throws[fn] < 30 {
			t.Errorf("the corpus has %d entries of %s, %d of them throwing; want the whole of it", counts[fn], fn, throws[fn])
		}
	}
}

// TestNumWhitespace holds numSpace to every code point: the set the index
// lists is every code unit Node's Number skips, and nothing astral is.
func TestNumWhitespace(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/num-goldens-index.json")
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Whitespace []rune `json:"whitespace"`
	}
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Whitespace) != 25 {
		t.Fatalf("the index lists %d whitespace code points; Node 24.18 has 25", len(index.Whitespace))
	}
	want := map[rune]bool{}
	for _, r := range index.Whitespace {
		want[r] = true
	}
	for r := range rune(0x110000) {
		if numSpace(r) != want[r] {
			t.Errorf("numSpace(U+%04X) = %v, want %v", r, numSpace(r), want[r])
		}
	}
}

// TestNumberToFixedKeyOutputs: the strings plan's measured table, and the
// rounding rules one by one, recorded from Node 24.18.
func TestNumberToFixedKeyOutputs(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		x, digits float64
		want      string
	}{
		// A tie rounds up, away from zero; FormatFloat rounds it to even.
		{0.5, 0, "1"}, {1.5, 0, "2"}, {2.5, 0, "3"}, {1.25, 1, "1.3"}, {0.125, 2, "0.13"},
		{-0.5, 0, "-1"}, {-2.5, 0, "-3"}, {9.5, 0, "10"}, {99.5, 0, "100"}, {0.375, 2, "0.38"},
		// What only looks like a tie rounds by its double.
		{1.005, 2, "1.00"}, {1.45, 1, "1.4"}, {1.35, 1, "1.4"}, {8.345, 2, "8.35"}, {10.235, 2, "10.23"},
		// Signs: -0 has none, a negative rounding to zero keeps its own.
		{NegZero, 2, "0.00"}, {-0.0001, 2, "-0.00"}, {-0.0001, 0, "-0"}, {0, 0, "0"},
		// From 1e21, NumberToString's; NaN and the infinities by name.
		{1e21, 2, "1e+21"}, {-1e21, 0, "-1e+21"}, {999999999999999900000, 0, "999999999999999868928"},
		{math.NaN(), 2, "NaN"}, {math.Inf(1), 0, "Infinity"}, {math.Inf(-1), 3, "-Infinity"},
		// The digit count is ToIntegerOrInfinity's.
		{1.5, math.NaN(), "2"}, {1.5, -0.5, "2"}, {1.5, 2.9, "1.50"}, {1.5, 100.5, "1.5" + strings.Repeat("0", 99)},
		{0.1, 20, "0.10000000000000000555"}, {5e-324, 3, "0.000"}, {123.456, 0, "123"},
	} {
		if got := NumberToFixed(c.x, c.digits); got != c.want {
			t.Errorf("(%v).toFixed(%v) = %q, want %q", c.x, c.digits, got, c.want)
		}
	}
}

// TestNumberToFixedRefusesDigits: a count outside 0 to 100 panics with
// Node's RangeError, checked before x, so NaN and 1e21 panic too.
func TestNumberToFixedRefusesDigits(t *testing.T) {
	t.Parallel()

	const node = " is not a digit count toFixed accepts (Node throws RangeError: toFixed() digits argument must be between 0 and 100)"
	for _, c := range []struct {
		x, digits float64
		shown     string
	}{
		{1.5, 101, "101"}, {1.5, -1, "-1"}, {1.5, -1.5, "-1.5"}, {1.5, math.Inf(1), "Infinity"},
		{1.5, math.Inf(-1), "-Infinity"}, {math.NaN(), 101, "101"}, {1e21, 1e300, "1e+300"},
	} {
		func() {
			defer func() {
				if got, want := recover(), "cakebear: "+c.shown+node; got != want {
					t.Errorf("(%v).toFixed(%v) panicked with %v, want %q", c.x, c.digits, got, want)
				}
			}()
			NumberToFixed(c.x, c.digits)
		}()
	}
}

// TestNumberParsersKeyOutputs: the grammars' edges, recorded from Node 24.18.
func TestNumberParsersKeyOutputs(t *testing.T) {
	t.Parallel()

	nan := math.NaN()
	for _, c := range []struct {
		s                     string
		number, float, intDef float64
	}{
		{"", 0, nan, nan}, {" \t\n", 0, nan, nan}, {"-0", NegZero, NegZero, NegZero}, {"-", nan, nan, nan},
		{".5", 0.5, 0.5, nan}, {"1.", 1, 1, 1}, {"1e", nan, 1, 1}, {"1.e5", 1e5, 1e5, 1}, {".e5", nan, nan, nan},
		{"0x10", 16, 0, 16}, {"-0x10", nan, NegZero, -16}, {"0X1F", 31, 0, 31}, {"0b101", 5, 0, 0}, {"0o17", 15, 0, 0},
		{"0o8", nan, 0, 0}, {"0x", nan, 0, nan}, {"00x10", nan, 0, 0}, {"Infinity", math.Inf(1), math.Inf(1), nan},
		{"-Infinity", math.Inf(-1), math.Inf(-1), nan}, {"infinity", nan, nan, nan}, {"Infinityx", nan, math.Inf(1), nan},
		{"1_000", nan, 1, 1}, {"1e400", math.Inf(1), math.Inf(1), 1}, {"-1e-400", NegZero, NegZero, -1},
		{"\u00a0\ufeff1\u2028", 1, 1, 1}, {"\u180e1", nan, nan, nan}, {"\u00851", nan, nan, nan},
		{"0x1fffffffffffff1", 144115188075855860, 0, 144115188075855860}, {"1e5x", nan, 1e5, 1},
		{"0." + strings.Repeat("0", 10010) + "1e10011", 1, 1, 0},
		// Exponents strconv would saturate, the digits making up for them.
		{"1" + strings.Repeat("0", 100000) + "e-100000", 1, 1, math.Inf(1)},
		{"-0." + strings.Repeat("0", 100000) + "1e100001", -1, -1, NegZero},
	} {
		for _, f := range []struct {
			name string
			got  float64
			want float64
		}{
			{"Number", StringToNumber(c.s), c.number},
			{"parseFloat", ParseFloat(c.s), c.float},
			{"parseInt", ParseInt(c.s), c.intDef},
		} {
			if !numSameValue(f.got, f.want) {
				t.Errorf("%s(%q) = %s, want %s", f.name, truncate(c.s, 40), numShow(f.got), numShow(f.want))
			}
		}
	}
}

// TestParseIntRadix: the radix goes through ToInt32, and only 0 and 16
// take a 0x prefix.
func TestParseIntRadix(t *testing.T) {
	t.Parallel()

	nan := math.NaN()
	for _, c := range []struct {
		s     string
		radix float64
		want  float64
	}{
		{"ff", 16, 255}, {"0xff", 16, 255}, {"0xff", 0, 255}, {"0xff", 10, 0}, {"0xff", 17, 0}, {"z", 36, 35},
		{"Z", 36, 35}, {"10", 37, nan}, {"10", 1, nan}, {"", 37, nan}, {"10", 4294967312, 16}, {"10", -4294967280, 16},
		{"10", math.Inf(1), 10}, {"10", nan, 10}, {"10", 16.9, 16}, {"10", -0.5, 10}, {"10", 2147483650, nan},
		{"10", 1e10, nan}, {"12", 2.999, 1}, {"  -0x", 16, nan}, {"-0", 10, NegZero}, {"0x0x10", 16, 0},
		// The powers of two round exactly, a tie to even; others fold in doubles.
		{"20000000000001", 16, 9007199254740992}, {"20000000000003", 16, 9007199254740996},
		{"9007199254740993", 14, 1400516559726106600}, {"9007199254740993", 36, 1.9896986116031812e+24},
	} {
		if got := ParseInt(c.s, c.radix); !numSameValue(got, c.want) {
			t.Errorf("parseInt(%q, %v) = %s, want %s", c.s, c.radix, numShow(got), numShow(c.want))
		}
	}
}

func TestNumberPredicates(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		x                      float64
		finite, isNaN, integer bool
	}{
		{0, true, false, true}, {NegZero, true, false, true}, {0.5, true, false, false}, {1e300, true, false, true},
		{math.NaN(), false, true, false}, {math.Inf(1), false, false, false}, {math.Inf(-1), false, false, false},
		{4503599627370495.5, true, false, false}, {5e-324, true, false, false},
	} {
		if NumberIsFinite(c.x) != c.finite || NumberIsNaN(c.x) != c.isNaN || NumberIsInteger(c.x) != c.integer {
			t.Errorf("%s: isFinite %v, isNaN %v, isInteger %v; want %v, %v, %v", numShow(c.x),
				NumberIsFinite(c.x), NumberIsNaN(c.x), NumberIsInteger(c.x), c.finite, c.isNaN, c.integer)
		}
	}
}
