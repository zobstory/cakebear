package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// jsonEntry is one line of testdata/json-goldens.jsonl: a value, the space
// JSON.stringify was given, if any, and what Node made of them, a string or
// the TypeError it threw. OutU is a string holding a lone surrogate, as its
// code units, which only a gap can put in the output. Any says the value is
// an unknown (M6).
type jsonEntry struct {
	Value  json.RawMessage `json:"value"`
	Space  json.RawMessage `json:"space"`
	Any    bool            `json:"any"`
	Out    *string         `json:"out"`
	OutU   []uint16        `json:"outU"`
	Throws *string         `json:"throws"`
}

// wtf8 is a string of UTF-16 code units as the runtime holds it: UTF-8,
// with each lone surrogate as the three bytes UTF-8 would make of it.
func wtf8(units []uint16) string {
	var b []byte
	for i := 0; i < len(units); i++ {
		u := rune(units[i])
		if utf16.IsSurrogate(u) && u < 0xdc00 && i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] <= 0xdfff {
			b = utf8.AppendRune(b, utf16.DecodeRune(u, rune(units[i+1])))
			i++
			continue
		}
		if utf16.IsSurrogate(u) {
			b = append(b, 0xe0|byte(u>>12), 0x80|byte(u>>6&0x3f), 0x80|byte(u&0x3f))
			continue
		}
		b = utf8.AppendRune(b, u)
	}
	return string(b)
}

// stringify is JSONStringify, with how it failed, if it did: what throws
// says of a throw, or a panic's value.
func stringify(v any, gap JSONGap) (out string, panicked any) {
	defer func() {
		switch p := recover().(type) {
		case nil:
		case *Thrown:
			panicked = throws(func() { panic(p) })
		default:
			panicked = p
		}
	}()
	return JSONStringify(v, gap, nil), nil
}

// cyclePanic is how stringify fails where Node throws err: with err itself,
// a TypeError a try can catch (M6 plan D9).
func cyclePanic(err string) string { return err }

func checkJSONEntry(t *testing.T, line int, e jsonEntry) bool {
	t.Helper()
	d := &decoder{t: t, ids: map[int]any{}}
	var v any
	if e.Any {
		v = d.unknown(e.Value)
	} else {
		v = d.value(e.Value)
	}
	gap := JSONGap{}
	if e.Space != nil {
		switch space := d.value(e.Space).(type) {
		case float64:
			gap = JSONGapNumber(space)
		case string:
			gap = JSONGapString(space)
		default:
			t.Fatalf("entry %d: a space of %T", line, space)
		}
	}
	got, panicked := stringify(v, gap)
	switch {
	case e.Throws != nil:
		if panicked != cyclePanic(*e.Throws) {
			t.Errorf("entry %d: gave %q, panic %v\nwant the panic for what Node threw\n%s", line, got, panicked, *e.Throws)
			return false
		}
	case panicked != nil:
		t.Errorf("entry %d: panicked: %v", line, panicked)
		return false
	default:
		want := wtf8(e.OutU)
		if e.Out != nil {
			want = *e.Out
		}
		if got != want {
			t.Errorf("entry %d: gave\n%s\nwant, as Node gives\n%s\n(value %s, space %s)", line, got, want, truncate(string(e.Value), 300), e.Space)
			return false
		}
	}
	return true
}

// TestJSONGoldens: every entry of the corpus Node 24.18 stringified
// (testdata/json-goldens.jsonl, made by testdata/gen-json-goldens.mjs).
func TestJSONGoldens(t *testing.T) {
	t.Parallel()

	f, err := os.Open("testdata/json-goldens.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	n, failed, threw := 0, 0, 0
	for scanner.Scan() {
		n++
		var e jsonEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("line %d: %v", n, err)
		}
		if e.Throws != nil {
			threw++
		}
		if !checkJSONEntry(t, n, e) {
			failed++
			if failed >= 20 {
				t.Fatalf("stopping after 20 mismatches, at line %d", n)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 1000 || threw < 50 {
		t.Errorf("the corpus has %d entries, %d of them cycles; want the whole of it", n, threw)
	}
	t.Logf("%d entries, %d of them cycles, %d mismatched", n, threw, failed)
}

func TestJSONGapNumber(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		space  float64
		spaces int
		lines  bool
	}{
		{1, 1, true}, {2, 2, true}, {10, 10, true}, {11, 10, true}, {1e9, 10, true},
		{1.5, 1, true}, {1.9, 1, true}, {9.99, 9, true}, {math.Inf(1), 10, true},
		// V8's gap of no spaces, which still breaks lines.
		{0.999, 0, true}, {0.5, 0, true}, {1e-9, 0, true}, {5e-324, 0, true},
		{0, 0, false}, {NegZero, 0, false}, {-0.5, 0, false}, {-1, 0, false},
		{math.NaN(), 0, false}, {math.Inf(-1), 0, false},
	} {
		want := JSONGap{indent: fmt.Sprintf("%*s", c.spaces, ""), lines: c.lines}
		if got := JSONGapNumber(c.space); got != want {
			t.Errorf("JSONGapNumber(%v) = %+v, want %+v", c.space, got, want)
		}
	}
}

func TestJSONGapString(t *testing.T) {
	t.Parallel()

	high := "\xed\xa0\xbd" // the high half of U+1F600, as the runtime holds it
	lone := "\xed\xa0\x80" // U+D800
	if got := JSONGapString(""); got != (JSONGap{}) {
		t.Errorf(`JSONGapString("") = %+v, want none`, got)
	}
	for _, c := range []struct{ space, want string }{
		{"\t", "\t"},
		{" ", " "},
		{"0123456789", "0123456789"},
		{"0123456789x", "0123456789"},
		{"abcdefghijklmn", "abcdefghij"},
		{"12345678😀", "12345678😀"},
		{"123456789😀", "123456789" + high},
		{"1234567890😀", "1234567890"},
		{"😀😀😀😀😀😀", "😀😀😀😀😀"},
		{"日本語", "日本語"},
		{"日本語日本語日本語日本語", "日本語日本語日本語日"},
		{lone + lone + lone + lone + lone + lone + lone + lone + lone + lone + lone, lone + lone + lone + lone + lone + lone + lone + lone + lone + lone},
		{"123456789" + lone + "x", "123456789" + lone},
		// V8's gap ends at a NUL.
		{"a\x00b", "a"},
		{"\x00", ""},
		{"\x00ab", ""},
		{"0123456789\x00", "0123456789"},
		{"日\x00本", "日"},
	} {
		if got := JSONGapString(c.space); got != (JSONGap{indent: c.want, lines: true}) {
			t.Errorf("JSONGapString(%q) = %+v, want an indent of %q", c.space, got, c.want)
		}
	}
}

// TestJSONOutputs: what the corpus can't hold. Each output was recorded
// from Node 24.18, but for the extension types, which Node has no values
// of, and the bytes that are no string of JavaScript's.
func TestJSONOutputs(t *testing.T) {
	t.Parallel()

	lone := "\xed\xa0\x80" // U+D800, as a string literal puts it in the runtime
	self := newTestObject([]string{"self"}, []any{nil})
	self.values[0] = self
	for _, c := range []struct {
		name string
		v    any
		want string
	}{
		// The extension types, which Node has no values of: as the integers
		// they are, and a float32 as the number it widens to.
		{"i32", NewArray[int32](-2147483648, 0, 7), `[-2147483648,0,7]`},
		{"i64", int64(math.MaxInt64), `9223372036854775807`},
		{"u32", NewArray[uint32](4294967295), `[4294967295]`},
		{"u64", uint64(math.MaxUint64), `18446744073709551615`},
		{"f32", NewArray(float32(1.1), float32(math.Inf(1)), float32(math.NaN()), float32(NegZero)), `[1.100000023841858,null,null,0]`},
		{"booleans", NewArray(true, false), `[true,false]`},
		// A lone surrogate in a key, and two halves that met by
		// concatenation, which JavaScript pairs.
		{"lone key", newTestObject([]string{lone}, []any{1.0}), `{"\ud800":1}`},
		{"halves", "\xed\xa0\xbd\xed\xb8\x80", `"😀"`},
		{"halves reversed", "\xed\xb8\x80\xed\xa0\xbd", `"\ude00\ud83d"`},
		{"high then high", "\xed\xa0\xbd\xed\xa0\xbd\xed\xb8\x80", `"\ud83d😀"`},
		// Not a string the runtime makes: U+FFFD for each byte.
		{"cut half", "\xed\xa0", "\"\ufffd\ufffd\""},
		{"invalid byte", "a\xffb", "\"a\ufffdb\""},
		// A value reached twice that isn't a cycle is written twice.
		{"shared", func() any {
			s := newTestObject([]string{"v"}, []any{1.0})
			return NewArray[any](s, s, newTestObject([]string{"s"}, []any{s}))
		}(), `[{"v":1},{"v":1},{"s":{"v":1}}]`},
		{"empty containers", newTestObject([]string{"a", "b"}, []any{NewArray[any](), newTestObject(nil, nil)}), `{"a":[],"b":{}}`},
	} {
		if got, panicked := stringify(c.v, JSONGap{}); got != c.want || panicked != nil {
			t.Errorf("%s: gave %s, panic %v; want %s", c.name, got, panicked, c.want)
		}
	}
	if _, panicked := stringify(self, JSONGapNumber(2)); panicked != cyclePanic("TypeError: Converting circular structure to JSON\n    --> starting at object with constructor 'Object'\n    --- property 'self' closes the circle") {
		t.Errorf("a self-reference panicked %v", panicked)
	}
}

// TestJSONRefusesWhatLoweringRefuses: a value of no kind the subset can
// serialise is a bug in lowering, so it panics rather than guess.
func TestJSONRefusesWhatLoweringRefuses(t *testing.T) {
	t.Parallel()

	_, panicked := stringify(NewArray[any](func() {}), JSONGap{})
	if panicked != "cakebear: JSON.stringify has no way to write a func()" {
		t.Errorf("a function panicked %v", panicked)
	}
}
