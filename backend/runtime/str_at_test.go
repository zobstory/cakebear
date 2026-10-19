package runtime

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// The reads that may be undefined (M5 plan D9). The AT section of the
// string corpus holds at, codePointAt and s[i] at every code unit of its
// strings and past both ends, and an array's at and xs[i]
// (testdata/gen-str-goldens.mjs); TestStrGoldens runs them.

// asDecoded is m as the corpus decoder gives Node's result: the value m
// holds, or an empty Maybe[any] in m's state.
func asDecoded[T any](m Maybe[T]) any {
	switch {
	case m.IsUndefined():
		return Maybe[any]{}
	case m.IsNull():
		return NullOf[any]()
	}
	return m.v
}

// arrayRead is xs.at(i), or xs[i] where it may be undefined, as compiled
// code reads one: OrUndefined of an array of T, and MaybeOrUndefined of an
// array of maybes, whose element is the result as it is.
func arrayRead(xs any, i float64, at bool) any {
	switch xs := xs.(type) {
	case *Array[float64]:
		return readOf(xs, i, at)
	case *Array[string]:
		return readOf(xs, i, at)
	case *Array[bool]:
		return readOf(xs, i, at)
	case *Array[any]: // [], which the decoder can't type
		return readOf(xs, i, at)
	case *Array[Maybe[float64]]:
		return readOfMaybes(xs, i, at)
	case *Array[Maybe[string]]:
		return readOfMaybes(xs, i, at)
	case *Array[Maybe[any]]: // [undefined]
		return readOfMaybes(xs, i, at)
	}
	panic(fmt.Sprintf("no array read for a %T", xs))
}

func readOf[T any](xs *Array[T], i float64, at bool) any {
	if at {
		return asDecoded(OrUndefined(xs.RelativeAt(i)))
	}
	return asDecoded(OrUndefined(xs.Lookup(i)))
}

func readOfMaybes[T any](xs *Array[Maybe[T]], i float64, at bool) any {
	if at {
		return asDecoded(MaybeOrUndefined(xs.RelativeAt(i)))
	}
	return asDecoded(MaybeOrUndefined(xs.Lookup(i)))
}

// TestStrIndexPanicsWhereNodeReadsUndefined: s[i] typed string reads what
// Node reads wherever that is a code unit, for every index entry of the
// corpus, and panics everywhere Node reads undefined, saying so (M4 plan
// D8, as an array's index does).
func TestStrIndexPanicsWhereNodeReadsUndefined(t *testing.T) {
	t.Parallel()

	read, panicked := 0, 0
	readStrGoldens(t, func(line int, e strEntry) {
		if e.Fn != "index" {
			return
		}
		d := &decoder{t: t, ids: map[int]any{}}
		s, i := d.value(e.Args[0]).(string), d.value(e.Args[1]).(float64)
		want := d.value(e.Out)
		got, p := callStr(func(strArgs) any { return StrIndex(s, i) }, nil)
		if _, undefined := want.(Maybe[any]); undefined {
			panicked++
			if msg, _ := p.(string); !strings.HasPrefix(msg, "cakebear: ") || !strings.HasSuffix(msg, " (Node reads undefined)") {
				t.Errorf("line %d: %q[%v] gave %q, panic %v; want the panic for Node's undefined", line, s, i, got, p)
			}
			return
		}
		read++
		if p != nil || got != want {
			t.Errorf("line %d: %q[%v] gave %q, panic %v; want %q", line, s, i, got, p, want)
		}
	})
	if read < 200 || panicked < 1000 {
		t.Errorf("%d reads and %d panics: want the whole of the index entries", read, panicked)
	}
}

// TestStrIndexMessages: what the panic says, an index that isn't one told
// from one past the end, whose length is in code units.
func TestStrIndexMessages(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		s    string
		i    float64
		want string
	}{
		{"a😀b", 4, "cakebear: index 4 is out of range for a string of length 4 (Node reads undefined)"},
		{"abc", 1e21, "cakebear: index 1e+21 is out of range for a string of length 3 (Node reads undefined)"},
		{"abc", -1, "cakebear: -1 is not a string index (Node reads undefined)"},
		{"abc", 1.5, "cakebear: 1.5 is not a string index (Node reads undefined)"},
		{"abc", math.NaN(), "cakebear: NaN is not a string index (Node reads undefined)"},
		{"abc", math.Inf(1), "cakebear: Infinity is not a string index (Node reads undefined)"},
	} {
		if got := panicOf(func() { StrIndex(tt.s, tt.i) }); got != tt.want {
			t.Errorf("%q[%v] panicked with %q, want %q", tt.s, tt.i, got, tt.want)
		}
	}
	if got := StrIndex("abc", math.Copysign(0, -1)); got != "a" {
		t.Errorf(`"abc"[-0] = %q, want "a": -0 is the property "0"`, got)
	}
}
