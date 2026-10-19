package runtime

import (
	"fmt"
	"math"
)

// Array is a TypeScript array, T[] or ReadonlyArray<T> (M4 plan D7). It is a
// reference to its elements, as a JavaScript array is: every alias of one
// sees a push. A Go slice alone is not, since an append can move the
// elements and leave other copies of the slice behind.
//
// Index reads and writes are checked where Node would do something an array
// of T can't hold (D8, D9): reading past the end gives undefined in Node,
// and writing past it leaves a hole. Both panic here, with a message saying
// what Node does instead.
type Array[T any] struct {
	items []T
}

// NewArray makes an array of items, which it owns from then on.
func NewArray[T any](items ...T) *Array[T] {
	return &Array[T]{items: items}
}

// Len is the number of elements, for loops: `for…of` reads it before every
// iteration, so it sees elements pushed inside the loop (D13).
func (a *Array[T]) Len() int { return len(a.items) }

// Length is `xs.length`.
func (a *Array[T]) Length() float64 { return float64(len(a.items)) }

// At is the element at i, which a loop has already checked is in range.
func (a *Array[T]) At(i int) T { return a.items[i] }

// Get is `xs[i]`. An index that isn't one of the array's panics (D8).
func (a *Array[T]) Get(i float64) T {
	n, ok := arrayIndex(i)
	switch {
	case !ok:
		panic(fmt.Sprintf("cakebear: %s is not an array index (Node reads undefined)", NumberToString(i)))
	case n >= len(a.items):
		panic(fmt.Sprintf("cakebear: index %d is out of range for an array of length %d (Node reads undefined)", n, len(a.items)))
	}
	return a.items[n]
}

// Set is `xs[i] = v`. i may be the length, which appends. Past it, a hole,
// or an index that isn't an integer at least 0, a property, panics (D9).
func (a *Array[T]) Set(i float64, v T) {
	n, ok := arrayIndex(i)
	switch {
	case !ok:
		panic(fmt.Sprintf("cakebear: %s is not an array index (Node would set a property named %q)", NumberToString(i), NumberToString(i)))
	case n > len(a.items):
		panic(fmt.Sprintf("cakebear: writing index %d of an array of length %d would leave a hole (Node allows one)", n, len(a.items)))
	case n == len(a.items):
		a.items = append(a.items, v)
	default:
		a.items[n] = v
	}
}

// SetLength is `xs.length = n`, which may only shrink the array (D9).
func (a *Array[T]) SetLength(length float64) {
	n, ok := arrayIndex(length)
	switch {
	case !ok:
		panic(fmt.Sprintf("cakebear: %s is not a valid array length (Node throws RangeError: Invalid array length)", NumberToString(length)))
	case n > len(a.items):
		panic(fmt.Sprintf("cakebear: setting the length of an array of length %d to %d would leave holes (Node allows them)", len(a.items), n))
	}
	clear(a.items[n:]) // let the dropped elements go
	a.items = a.items[:n]
}

// Push is `xs.push(v, …)`, the new length.
func (a *Array[T]) Push(vs ...T) float64 {
	a.items = append(a.items, vs...)
	return float64(len(a.items))
}

// Pop is `xs.pop()` as a statement: it removes the last element, if there is
// one. Its value, T or undefined, waits for M5's nullable values.
func (a *Array[T]) Pop() {
	if n := len(a.items); n > 0 {
		var zero T
		a.items[n-1] = zero
		a.items = a.items[:n-1]
	}
}

// arrayIndex converts an index to an int, and reports whether it is one: an
// integer from 0 to 2^32 - 2, JavaScript's largest array index.
func arrayIndex(i float64) (int, bool) {
	if i < 0 || i > math.MaxUint32-1 || i != math.Trunc(i) {
		return 0, false // also NaN, which fails every comparison
	}
	return int(i), true
}

// inspectLen, inspectAt and numeric are what inspect needs of an array of any
// element type.
func (a *Array[T]) inspectLen() int     { return len(a.items) }
func (a *Array[T]) inspectAt(i int) any { return a.items[i] }
func (a *Array[T]) numeric() bool {
	var zero T
	switch any(zero).(type) {
	case float64, float32, int32, int64, uint32, uint64:
		return true
	case Maybe[float64], Maybe[float32], Maybe[int32], Maybe[int64], Maybe[uint32], Maybe[uint64]:
		// groupArrayElements pads from the left only when every element it
		// looks at is a number: the ones output has, at most 101, so an
		// undefined past them changes nothing.
		for _, item := range a.items[:min(len(a.items), maxArrayLength+1)] {
			if state, _ := any(item).(maybe).maybeState(); state != stateValue {
				return false
			}
		}
		return true
	}
	return false
}

// cloneArray copies the array for c, its elements through CloneValue.
func (a *Array[T]) cloneArray(c *Cloner) any {
	copied := &Array[T]{items: make([]T, len(a.items))}
	c.Remember(a, copied)
	for i, v := range a.items {
		copied.items[i] = CloneValue(c, v)
	}
	return copied
}
