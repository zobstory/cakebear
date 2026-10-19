package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"testing"
)

// testObject is an Object as the backend generates one: its values in the
// shape's order (keys sorted), and a layout naming them in the order its
// literal wrote them.
type testObject struct {
	layout *Layout
	values []any
}

func (o *testObject) Layout() *Layout { return o.layout }
func (o *testObject) Field(i int) any { return o.values[i] }
func (o *testObject) CloneObject(c *Cloner) Object {
	copied := &testObject{layout: o.layout, values: make([]any, len(o.values))}
	c.Remember(o, copied)
	for i, v := range o.values {
		copied.values[i] = CloneValue(c, v)
	}
	return copied
}

// newTestObject builds an object from its keys in creation order.
func newTestObject(keys []string, values []any) *testObject {
	sorted := slices.Clone(keys)
	slices.Sort(sorted)
	fields := make([]int, len(keys))
	shaped := make([]any, len(keys))
	for i, k := range keys {
		fields[i], _ = slices.BinarySearch(sorted, k)
		shaped[fields[i]] = values[i]
	}
	return &testObject{layout: NewLayout(keys, fields), values: shaped}
}

// goldenValue is the corpus's encoding of a value
// (testdata/gen-inspect-goldens.mjs). U is a string holding a lone
// surrogate, as its UTF-16 code units, which only the JSON corpus has
// (testdata/gen-json-goldens.mjs).
type goldenValue struct {
	N   *string              `json:"n"`
	S   *string              `json:"s"`
	U   []uint16             `json:"u"`
	B   *bool                `json:"b"`
	E   *string              `json:"e"` // "undefined" or "null"
	A   []json.RawMessage    `json:"a"`
	O   [][2]json.RawMessage `json:"o"`
	ID  *int                 `json:"id"`
	Ref *int                 `json:"ref"`
}

// decoder rebuilds the values of one corpus entry, its ids shared across
// the entry's arguments.
type decoder struct {
	t   *testing.T
	ids map[int]any
}

func (d *decoder) value(raw json.RawMessage) any {
	var g goldenValue
	if err := json.Unmarshal(raw, &g); err != nil {
		d.t.Fatalf("decoding %s: %v", raw, err)
	}
	switch {
	case g.N != nil:
		return parseNumber(d.t, *g.N)
	case g.S != nil:
		return *g.S
	case g.U != nil:
		return wtf8(g.U)
	case g.B != nil:
		return *g.B
	case g.E != nil:
		if *g.E == "null" {
			return NullOf[any]()
		}
		return Maybe[any]{}
	case g.Ref != nil:
		v, ok := d.ids[*g.Ref]
		if !ok {
			d.t.Fatalf("a ref to %d before it is defined", *g.Ref)
		}
		return v
	case g.O != nil:
		obj := &testObject{}
		d.register(g.ID, obj)
		keys := make([]string, len(g.O))
		values := make([]any, len(g.O))
		for i, kv := range g.O {
			if err := json.Unmarshal(kv[0], &keys[i]); err != nil {
				d.t.Fatalf("a key: %v", err)
			}
			values[i] = d.value(kv[1])
		}
		*obj = *newTestObject(keys, values)
		return obj
	case g.A != nil:
		return d.array(g)
	}
	// An empty array decodes with a nil A; tell it from a missing field.
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) == nil {
		if _, ok := probe["a"]; ok {
			return d.array(g)
		}
	}
	d.t.Fatalf("no value in %s", raw)
	return nil
}

// array makes the Go array the subset would: number[] as *Array[float64],
// which inspect aligns right, and the same for strings and booleans. An
// array of containers holds them as any. An array holding undefined or null
// is of maybes (M5): (number | undefined)[] is *Array[Maybe[float64]].
func (d *decoder) array(g goldenValue) any {
	kind, empties := "", false
	for _, raw := range g.A {
		var e goldenValue
		_ = json.Unmarshal(raw, &e)
		switch {
		case e.E != nil:
			empties = true
			continue
		case kind != "":
		case e.N != nil:
			kind = "n"
		case e.S != nil, e.U != nil:
			kind = "s"
		case e.B != nil:
			kind = "b"
		default:
			kind = "container"
		}
	}
	if empties {
		switch kind {
		case "n":
			return d.register(g.ID, maybeArray[float64](d, g.A))
		case "s":
			return d.register(g.ID, maybeArray[string](d, g.A))
		case "b":
			return d.register(g.ID, maybeArray[bool](d, g.A))
		}
		// Registered before its elements, which may refer back to it.
		arr := NewArray[Maybe[any]]()
		d.register(g.ID, arr)
		for _, raw := range g.A {
			arr.Push(asMaybe[any](d.value(raw)))
		}
		return arr
	}
	switch kind {
	case "n":
		items := make([]float64, len(g.A))
		for i, raw := range g.A {
			items[i] = d.value(raw).(float64)
		}
		return d.register(g.ID, NewArray(items...))
	case "s":
		items := make([]string, len(g.A))
		for i, raw := range g.A {
			items[i] = d.value(raw).(string)
		}
		return d.register(g.ID, NewArray(items...))
	case "b":
		items := make([]bool, len(g.A))
		for i, raw := range g.A {
			items[i] = d.value(raw).(bool)
		}
		return d.register(g.ID, NewArray(items...))
	}
	arr := NewArray[any]()
	d.register(g.ID, arr)
	for _, raw := range g.A {
		arr.Push(d.value(raw))
	}
	return arr
}

func maybeArray[T any](d *decoder, raws []json.RawMessage) *Array[Maybe[T]] {
	items := make([]Maybe[T], len(raws))
	for i, raw := range raws {
		items[i] = asMaybe[T](d.value(raw))
	}
	return NewArray(items...)
}

// asMaybe is v, a decoded value, as a Maybe of T: empty when it is, and
// holding v otherwise.
func asMaybe[T any](v any) Maybe[T] {
	if m, ok := v.(Maybe[any]); ok {
		if m.IsNull() {
			return NullOf[T]()
		}
		return Maybe[T]{}
	}
	return Some(v.(T))
}

func (d *decoder) register(id *int, v any) any {
	if id != nil {
		d.ids[*id] = v
	}
	return v
}

func parseNumber(t *testing.T, s string) float64 {
	switch s {
	case "-0":
		return math.Copysign(0, -1)
	case "NaN":
		return math.NaN()
	case "Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("number %q: %v", s, err)
	}
	return f
}

type goldenEntry struct {
	Args []json.RawMessage `json:"args"`
	Out  string            `json:"out"`
}

func checkEntry(t *testing.T, line int, e goldenEntry) bool {
	t.Helper()
	d := &decoder{t: t, ids: map[int]any{}}
	args := make([]any, len(e.Args))
	for i, raw := range e.Args {
		args[i] = d.value(raw)
	}
	if got := Format(args...); got != e.Out {
		t.Errorf("entry %d: printed\n%s\nwant, as Node prints\n%s\n(args %s)", line, got, e.Out, truncate(fmt.Sprint(e.Args), 300))
		return false
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// TestInspectKeyOutputs: the M4 plan's table of exact outputs, recorded from
// Node 24.18.
func TestInspectKeyOutputs(t *testing.T) {
	t.Parallel()

	for i, line := range []string{
		`{"args":[{"o":[["a",{"n":"1"}],["b",{"s":"x"}],["c",{"b":true}]]}],"out":"{ a: 1, b: 'x', c: true }"}`,
		`{"args":[{"o":[["a",{"o":[["b",{"o":[["c",{"o":[["d",{"n":"1"}]]}]]}]]}]]}],"out":"{ a: { b: { c: [Object] } } }"}`,
		`{"args":[{"a":[{"n":"1"},{"n":"2"},{"n":"3"},{"n":"4"},{"n":"5"},{"n":"6"},{"n":"7"}]}],"out":"[\n  1, 2, 3, 4,\n  5, 6, 7\n]"}`,
		`{"args":[{"o":[["users",{"a":[{"o":[["id",{"n":"1"}],["name",{"s":"Ann"}]]},{"o":[["id",{"n":"2"}],["name",{"s":"Bob"}]]},{"o":[["id",{"n":"3"}],["name",{"s":"Cy"}]]}]}]]}],"out":"{\n  users: [\n    { id: 1, name: 'Ann' },\n    { id: 2, name: 'Bob' },\n    { id: 3, name: 'Cy' }\n  ]\n}"}`,
		`{"args":[{"a":[{"s":"a"},{"s":"it's"},{"s":"x\ny"}]}],"out":"[ 'a', \"it's\", 'x\\ny' ]"}`,
		`{"args":[{"a":[{"n":"-0"},{"n":"0"},{"n":"1e+21"},{"n":"1e-7"},{"n":"0.1"},{"n":"NaN"}]}],"out":"[ -0, 0, 1e+21, 1e-7, 0.1, NaN ]"}`,
		`{"args":[{"s":"a"},{"o":[["x",{"n":"1"}]]}],"out":"a { x: 1 }"}`,
		`{"args":[{"o":[["$x",{"n":"1"}],["_y",{"n":"2"}]]}],"out":"{ '$x': 1, _y: 2 }"}`,
		`{"args":[{"o":[["y",{"n":"2"}],["x",{"n":"1"}]]}],"out":"{ y: 2, x: 1 }"}`,
		`{"args":[{"o":[["kids",{"a":[{"ref":1}]}]],"id":1}],"out":"<ref *1> { kids: [ [Circular *1] ] }"}`,
	} {
		var e goldenEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		checkEntry(t, i, e)
	}
}

// TestInspectGoldens: every entry of the corpus Node 24.18 printed
// (testdata/inspect-goldens.jsonl, made by testdata/gen-inspect-goldens.mjs).
func TestInspectGoldens(t *testing.T) {
	t.Parallel()

	f, err := os.Open("testdata/inspect-goldens.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	n, failed := 0, 0
	for scanner.Scan() {
		n++
		var e goldenEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("line %d: %v", n, err)
		}
		if !checkEntry(t, n, e) {
			failed++
			if failed >= 20 {
				t.Fatalf("stopping after 20 mismatches, at line %d", n)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 1000 {
		t.Errorf("the corpus has %d entries, want the whole of it", n)
	}
	t.Logf("%d entries, %d mismatched", n, failed)
}
