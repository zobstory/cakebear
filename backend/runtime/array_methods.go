package runtime

import (
	"fmt"
	"math"
	"slices"
)

// The array methods that take a callback, and the searches and copies
// (M4 plan D16), each following its algorithm in ECMA-262.
//
// A callback runs in the domain of the code calling the method, d, and
// takes the element, its index and the array itself, as JavaScript's does.
// Lowering gives every callback that full signature, padding a literal that
// declares fewer parameters.
//
// The iteration methods read the length once, when they start, and each
// element as it is when they reach it: an element pushed by the callback
// isn't visited, and one it replaces is seen. Node skips an element the
// callback removed, or passes undefined for it to findIndex. An array of T
// has no undefined to pass, so cakebear panics instead (D9), saying what
// Node does.

// Map is xs.map(fn). It needs a type parameter of its own, the element type
// of the result, so it is a function rather than a method.
func Map[T, U any](d *Domain, a *Array[T], fn func(*Domain, T, float64, *Array[T]) U) *Array[U] {
	n := len(a.items)
	mapped := make([]U, n)
	for k := range n {
		mapped[k] = fn(d, a.visit(k, n, "map"), float64(k), a)
	}
	return &Array[U]{items: mapped}
}

// Filter is xs.filter(fn). The element is read before fn runs, so it is
// that value that is kept, even if fn replaces it. The result is a new
// array, never a subslice that would alias this one.
func (a *Array[T]) Filter(d *Domain, fn func(*Domain, T, float64, *Array[T]) bool) *Array[T] {
	n := len(a.items)
	var kept []T
	for k := range n {
		if v := a.visit(k, n, "filter"); fn(d, v, float64(k), a) {
			kept = append(kept, v)
		}
	}
	return &Array[T]{items: kept}
}

// ForEach is xs.forEach(fn).
func (a *Array[T]) ForEach(d *Domain, fn func(*Domain, T, float64, *Array[T])) {
	n := len(a.items)
	for k := range n {
		fn(d, a.visit(k, n, "forEach"), float64(k), a)
	}
}

// Some is xs.some(fn), which stops at the first element fn accepts.
func (a *Array[T]) Some(d *Domain, fn func(*Domain, T, float64, *Array[T]) bool) bool {
	n := len(a.items)
	for k := range n {
		if fn(d, a.visit(k, n, "some"), float64(k), a) {
			return true
		}
	}
	return false
}

// Every is xs.every(fn), which stops at the first element fn rejects.
func (a *Array[T]) Every(d *Domain, fn func(*Domain, T, float64, *Array[T]) bool) bool {
	n := len(a.items)
	for k := range n {
		if !fn(d, a.visit(k, n, "every"), float64(k), a) {
			return false
		}
	}
	return true
}

// FindIndex is xs.findIndex(fn): the index of the first element fn
// accepts, or -1.
func (a *Array[T]) FindIndex(d *Domain, fn func(*Domain, T, float64, *Array[T]) bool) float64 {
	n := len(a.items)
	for k := range n {
		if fn(d, a.visit(k, n, "findIndex"), float64(k), a) {
			return float64(k)
		}
	}
	return -1
}

// Reduce is xs.reduce(fn, init), folding from init. Lowering refuses reduce
// without an initial value, whose empty case throws a TypeError. Like Map,
// it is a function for the type parameter of its accumulator.
func Reduce[T, U any](d *Domain, a *Array[T], fn func(*Domain, U, T, float64, *Array[T]) U, init U) U {
	acc := init
	n := len(a.items)
	for k := range n {
		acc = fn(d, acc, a.visit(k, n, "reduce"), float64(k), a)
	}
	return acc
}

// visit is element k of an iteration over the n elements the array had when
// method started, or a panic if a callback has since removed it.
func (a *Array[T]) visit(k, n int, method string) T {
	if k >= len(a.items) {
		node := "skips the rest"
		if method == "findIndex" { // FindViaPredicate reads every index
			node = "reads undefined"
		}
		panic(fmt.Sprintf("cakebear: the array shrank to %s while %s ran over %d (Node %s)", elements(len(a.items)), method, n, node))
	}
	return a.items[k]
}

func elements(n int) string {
	if n == 1 {
		return "1 element"
	}
	return fmt.Sprintf("%d elements", n)
}

// Sort is xs.sort(cmp): it sorts the array in place, stably, and returns
// it. A comparator result of NaN counts as 0, as SortCompare has it.
//
// As in the spec, and V8, the elements are copied out, sorted, and written
// back, so a comparator reading the array sees it unsorted, and one that
// changes the array's length gets what Node does: the sorted elements fill
// the first places, appended where the array has become shorter, and an
// element pushed past them stays.
//
// V8 sorts with TimSort, and this with Go's stable sort, which compare
// different pairs, in a different order. For a consistent comparator a
// stable sort has exactly one result, so the arrays come out identical; a
// comparator with side effects, such as logging, sees the difference, and
// one that is inconsistent can order the elements differently.
func (a *Array[T]) Sort(d *Domain, cmp func(*Domain, T, T) float64) *Array[T] {
	sorted := slices.Clone(a.items)
	slices.SortStableFunc(sorted, func(x, y T) int {
		switch v := cmp(d, x, y); {
		case v < 0:
			return -1
		case v > 0:
			return 1
		}
		return 0 // NaN as well
	})
	n := copy(a.items, sorted)
	a.items = append(a.items, sorted[n:]...)
	return a
}

// Slice is xs.slice(start?, end?), which takes up to two bounds. Each is
// relative to the end when it is negative, and both are clamped to the
// array. The result is a copy, never a subslice that would alias this one.
func (a *Array[T]) Slice(bounds ...float64) *Array[T] {
	n := len(a.items)
	start, end := 0, n
	if len(bounds) > 0 {
		start = relativeIndex(bounds[0], n)
	}
	if len(bounds) > 1 {
		end = relativeIndex(bounds[1], n)
	}
	if start >= end {
		return &Array[T]{}
	}
	return &Array[T]{items: slices.Clone(a.items[start:end])}
}

// relativeIndex is slice's conversion of a bound: ToIntegerOrInfinity, which
// takes NaN to 0 and truncates toward zero, keeping the infinities, then
// counted from the end of an array of n elements if negative, and clamped
// to [0, n]. Truncating comes first, so -0.5 is 0, the start.
func relativeIndex(bound float64, n int) int {
	i := math.Trunc(bound)
	switch {
	case math.IsNaN(i):
		return 0
	case i < 0:
		return int(max(float64(n)+i, 0))
	}
	return int(min(i, float64(n)))
}

// Includes is xs.includes(v), which compares by SameValueZero: NaN is found,
// and 0 and -0 are equal. Objects and arrays are equal only to themselves,
// as pointers are. It and IndexOf are functions because they need T to be
// comparable, which a function value isn't: lowering refuses those.
func Includes[T comparable](a *Array[T], v T) bool {
	if isNaN(v) {
		return slices.ContainsFunc(a.items, isNaN[T])
	}
	return slices.Contains(a.items, v)
}

// IndexOf is xs.indexOf(v), the first index holding v, or -1. It compares by
// strict equality, Go's ==: NaN is never found, and 0 and -0 are equal.
func IndexOf[T comparable](a *Array[T], v T) float64 {
	return float64(slices.Index(a.items, v))
}

// isNaN reports whether v is NaN, the one value not equal to itself.
func isNaN[T comparable](v T) bool {
	return v != v
}
