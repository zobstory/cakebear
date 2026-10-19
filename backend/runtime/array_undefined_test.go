package runtime

import (
	"fmt"
	"strings"
	"testing"
)

// The array reads that may be undefined (M5 plan D9), compared with what
// Node 24.18 gave for the same calls. at and xs[i] are in the string
// corpus's AT section (str_at_test.go).

// showRead is a comma-ok read as Node prints its result: the element, or
// undefined.
func showRead[T any](v T, ok bool) string {
	if !ok {
		return "undefined"
	}
	return fmt.Sprint(v)
}

// spaced is vs as console.log separates them, with spaces.
func spaced(vs ...any) string { return strings.TrimSuffix(fmt.Sprintln(vs...), "\n") }

// TestFindAndFindLast: the first and the last element a callback accepts,
// as it was read before the callback ran, or undefined; findLast from the
// end back; an element replaced ahead of the search is seen, and one
// pushed isn't visited.
//
//	xs = [1, 2, 3]
//	xs.find((x) => x > 1), xs.findLast((x) => x < 3), xs.find((x) => x > 9), xs.findLast((x) => x > 9)  // 2 2 undefined undefined
//	[1, 2, 3].find((x, i, a) => { if (i === 0) a[2] = 7; return x === 7 })      // 7
//	[1, 2, 3].findLast((x, i, a) => { if (i === 2) a[0] = 7; return x === 7 })  // 7
//	r = [1, 2]; r.find((x, i, a) => { a[i] = 50; return true }), r              // 1 [ 50, 2 ]
//	[].find(() => true), [].findLast(() => true)                                // undefined undefined
//	["a", "b", "c"].findLast((x, i, a) => { seen.push(`${x} ${i} ${a.length}`); return x === "a" })  // a, seen [ 'c 2 3', 'b 1 3', 'a 0 3' ]
//	p = [1, 2, 3]; p.find((x, i, a) => { a.push(10); return x > 5 }), p         // undefined [ 1, 2, 3, 10, 10, 10 ]
//	q = [1, 2, 3]; q.findLast((x, i, a) => { a.push(0); return x > 5 }), q      // undefined [ 1, 2, 3, 0, 0, 0 ]
func TestFindAndFindLast(t *testing.T) {
	t.Parallel()

	d := newDomain()
	above := func(n float64) func(*Domain, float64, float64, *Array[float64]) bool {
		return func(_ *Domain, x, _ float64, _ *Array[float64]) bool { return x > n }
	}
	below := func(n float64) func(*Domain, float64, float64, *Array[float64]) bool {
		return func(_ *Domain, x, _ float64, _ *Array[float64]) bool { return x < n }
	}
	xs := NewArray(1.0, 2.0, 3.0)
	if got := spaced(showRead(xs.Find(d, above(1))), showRead(xs.FindLast(d, below(3))),
		showRead(xs.Find(d, above(9))), showRead(xs.FindLast(d, above(9)))); got != "2 2 undefined undefined" {
		t.Errorf("find and findLast gave %s, want 2 2 undefined undefined", got)
	}

	// replace sets a[index] to 7 while visiting at, and looks for 7.
	replace := func(at, index float64) func(*Domain, float64, float64, *Array[float64]) bool {
		return func(_ *Domain, x, i float64, a *Array[float64]) bool {
			if i == at {
				a.Set(index, 7)
			}
			return x == 7
		}
	}
	if got := showRead(NewArray(1.0, 2.0, 3.0).Find(d, replace(0, 2))); got != "7" {
		t.Errorf("find gave %s, want 7, the element replaced ahead of it", got)
	}
	if got := showRead(NewArray(1.0, 2.0, 3.0).FindLast(d, replace(2, 0))); got != "7" {
		t.Errorf("findLast gave %s, want 7, the element replaced ahead of it", got)
	}

	r := NewArray(1.0, 2.0)
	found := showRead(r.Find(d, func(_ *Domain, _, i float64, a *Array[float64]) bool { a.Set(i, 50); return true }))
	if got := spaced(found, r.items); got != "1 [50 2]" {
		t.Errorf("find gave %s, want 1 [50 2]: the element as it was read", got)
	}

	empty := NewArray[float64]()
	if got := spaced(showRead(empty.Find(d, above(-1))), showRead(empty.FindLast(d, above(-1)))); got != "undefined undefined" {
		t.Errorf("find and findLast of [] gave %s", got)
	}

	var seen []string
	last := showRead(NewArray("a", "b", "c").FindLast(d, func(_ *Domain, x string, i float64, a *Array[string]) bool {
		seen = append(seen, fmt.Sprintf("%s %v %v", x, i, a.Length()))
		return x == "a"
	}))
	if got := spaced(last, seen); got != "a [c 2 3 b 1 3 a 0 3]" {
		t.Errorf("findLast gave %s, want a [c 2 3 b 1 3 a 0 3]: from the end, with each index and the array", got)
	}

	for name, search := range map[string]func(*Array[float64], func(*Domain, float64, float64, *Array[float64]) bool) (float64, bool){
		"find": func(a *Array[float64], fn func(*Domain, float64, float64, *Array[float64]) bool) (float64, bool) {
			return a.Find(d, fn)
		},
		"findLast": func(a *Array[float64], fn func(*Domain, float64, float64, *Array[float64]) bool) (float64, bool) {
			return a.FindLast(d, fn)
		},
	} {
		p, calls := NewArray(1.0, 2.0, 3.0), 0
		got := showRead(search(p, func(_ *Domain, x, _ float64, a *Array[float64]) bool {
			calls++
			a.Push(10)
			return x > 5
		}))
		if got != "undefined" || calls != 3 || p.Len() != 6 {
			t.Errorf("%s pushing 10 on every visit gave %s after %d calls, leaving %v: want undefined after 3, the pushes not visited",
				name, got, calls, p.items)
		}
	}
}

// TestPopAndShift: each removes an element and gives it, or undefined from
// an empty array, which they leave empty; every alias sees it; and a queue
// of pushes and shifts keeps its order.
//
//	s = [1, 2, 3]; alias = s
//	s.shift(), alias                                     // 1 [ 2, 3 ]
//	alias.push(4); s.pop(), s                            // 4 [ 2, 3 ]
//	s.shift(), s.shift(), s.shift(), s.pop(), s.length   // 2 3 undefined undefined 0
//	qq = []; for (let i = 0; i < 100; i++) { qq.push(i); if (i % 3 === 0) qq.shift() }
//	qq.length, qq[0], qq[qq.length - 1]                  // 66 34 99
//	m = [undefined, 2, null]; m.shift(), m.pop(), m      // undefined null [ 2 ]
func TestPopAndShift(t *testing.T) {
	t.Parallel()

	s := NewArray(1.0, 2.0, 3.0)
	alias := s
	if got := spaced(showRead(s.Shift()), alias.items); got != "1 [2 3]" {
		t.Errorf("shift gave %s, want 1 [2 3]", got)
	}
	alias.Push(4)
	if got := spaced(showRead(s.Pop()), s.items); got != "4 [2 3]" {
		t.Errorf("pop after an alias's push gave %s, want 4 [2 3]", got)
	}
	if got := spaced(showRead(s.Shift()), showRead(s.Shift()), showRead(s.Shift()), showRead(s.Pop()), s.Length()); got != "2 3 undefined undefined 0" {
		t.Errorf("emptying the array gave %s, want 2 3 undefined undefined 0", got)
	}

	q := NewArray[float64]()
	for i := range 100 {
		q.Push(float64(i))
		if i%3 == 0 {
			q.Shift()
		}
	}
	if got := spaced(q.Length(), q.Get(0), q.Get(q.Length()-1)); got != "66 34 99" {
		t.Errorf("the queue ends as %s, want 66 34 99", got)
	}

	m := NewArray(Maybe[float64]{}, Some(2.0), NullOf[float64]())
	first, last := MaybeOrUndefined(m.Shift()), MaybeOrUndefined(m.Pop())
	if !first.IsUndefined() || !last.IsNull() || m.Len() != 1 || m.Get(0) != Some(2.0) {
		t.Errorf("shift and pop of [undefined, 2, null] gave %v and %v, leaving %v", first, last, m.items)
	}
}
