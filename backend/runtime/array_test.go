package runtime

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// panicOf runs f and returns what it panicked with, or "": panicked's
// words, so a throw reads as one.
func panicOf(f func()) string { return panicked(f) }

// TestArrayIsAReference: every alias of an array sees a push, as in
// JavaScript, which a Go slice copied by value would not.
func TestArrayIsAReference(t *testing.T) {
	t.Parallel()

	xs := NewArray(1.0, 2.0)
	alias := xs
	for range 100 { // enough appends to move the elements
		alias.Push(3)
	}
	if xs.Len() != 102 || xs.Get(101) != 3 {
		t.Errorf("after pushes through an alias, xs has %d elements", xs.Len())
	}
	if got := xs.Push(4, 5); got != 104 {
		t.Errorf("Push = %v, want the new length 104", got)
	}
}

// TestArrayReadsAndWrites: what Node allows that an array of T can't hold
// panics, saying what Node does instead (D8, D9).
func TestArrayReadsAndWrites(t *testing.T) {
	t.Parallel()

	xs := NewArray("a", "b", "c")
	xs.Set(1, "B")
	xs.Set(3, "d") // the length: appends
	xs.Set(-0.0, "A")
	if got := fmt.Sprint(xs.items); got != "[A B c d]" {
		t.Errorf("after the writes, xs = %s", got)
	}
	for _, tt := range []struct {
		name string
		f    func()
		want string
	}{
		{"a read past the end", func() { xs.Get(9) }, "cakebear: index 9 is out of range for an array of length 4 (Node reads undefined)"},
		{"a read at the length", func() { xs.Get(4) }, "index 4 is out of range for an array of length 4"},
		{"a negative read", func() { xs.Get(-1) }, "cakebear: -1 is not an array index (Node reads undefined)"},
		{"a fractional read", func() { xs.Get(1.5) }, "1.5 is not an array index"},
		{"a NaN read", func() { xs.Get(math.NaN()) }, "NaN is not an array index"},
		{"a write leaving a hole", func() { xs.Set(6, "x") }, "cakebear: writing index 6 of an array of length 4 would leave a hole (Node allows one)"},
		{"a negative write", func() { xs.Set(-1, "x") }, `cakebear: -1 is not an array index (Node would set a property named "-1")`},
		{"growing the length", func() { xs.SetLength(10, nil) }, "setting the length of an array of length 4 to 10 would leave holes"},
		// Node throws for a length that isn't one, so this does (M6 plan D9).
		{"a fractional length", func() { xs.SetLength(1.5, nil) }, "threw RangeError: Invalid array length"},
		{"a negative length", func() { xs.SetLength(-1, nil) }, "threw RangeError: Invalid array length"},
		{"a length past 2^32 - 1", func() { xs.SetLength(1<<32, nil) }, "threw RangeError: Invalid array length"},
		{"the largest length", func() { xs.SetLength(1<<32-1, nil) }, "to 4294967295 would leave holes (Node allows them)"},
	} {
		if got := panicOf(tt.f); !strings.Contains(got, tt.want) {
			t.Errorf("%s: panicked with %q, want %q", tt.name, got, tt.want)
		}
	}

	xs.SetLength(2, nil)
	xs.Pop()
	xs.Pop()
	xs.Pop() // on an empty array: nothing, as in JavaScript
	if xs.Len() != 0 || xs.Length() != 0 {
		t.Errorf("after shrinking and popping, xs has %d elements", xs.Len())
	}
}

// TestCloneKeepsAliasing: a value reached twice is copied once, a cycle stays
// a cycle, and nothing of the copy is the original's (D11).
func TestCloneKeepsAliasing(t *testing.T) {
	t.Parallel()

	shared := newTestObject([]string{"n"}, []any{1.0})
	tree := newTestObject([]string{"label", "kids"}, []any{"root", NewArray[any](shared, shared)})
	tree.values[0].(*Array[any]).Push(tree) // kids is field 0 in the shape's order

	copied := Clone(tree)
	if copied == tree {
		t.Fatal("Clone returned the original")
	}
	kids := copied.values[0].(*Array[any])
	if kids == tree.values[0] {
		t.Error("the copy shares the original's array")
	}
	if kids.At(0) != kids.At(1) {
		t.Error("the shared object became two copies")
	}
	if kids.At(0) == any(shared) {
		t.Error("the shared object wasn't copied")
	}
	if kids.At(2) != any(copied) {
		t.Error("the cycle leads back to the original, not the copy")
	}
	if got, want := Inspect(copied), Inspect(tree); got != want {
		t.Errorf("the copy prints\n%s\nthe original\n%s", got, want)
	}
	shared.values[0] = 2.0
	if kids.At(0).(*testObject).values[0] != 1.0 {
		t.Error("a change to the original reached the copy")
	}
	if Clone(3.5) != 3.5 || Clone("s") != "s" {
		t.Error("a primitive didn't clone to itself")
	}
}

// TestFormat: R1, and the panic that guards D12's precondition.
func TestFormat(t *testing.T) {
	t.Parallel()

	if got := Format("a", 1.0, "b", NewArray(true), math.Copysign(0, -1), int32(-7)); got != "a 1 b [ true ] -0 -7" {
		t.Errorf("Format = %q", got)
	}
	if got := Format(NewArray("x"), "y"); got != "[ 'x' ] y" {
		t.Errorf("Format = %q, want strings after the first printed raw too", got)
	}
	if got := Format("50%"); got != "50%" {
		t.Errorf("a lone format string = %q, want it as it is", got)
	}
	if got := panicOf(func() { Format("%s", "x") }); !strings.Contains(got, "format string") {
		t.Errorf("Format with a format string panicked with %q", got)
	}
	if got := panicOf(func() { Inspect(func() {}) }); !strings.Contains(got, "no way to print a func()") {
		t.Errorf("inspecting a function panicked with %q", got)
	}
}
