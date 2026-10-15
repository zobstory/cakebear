package runtime

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// The expected values here are what Node 24.18 gave for the same calls.

// TestCallbacksGetTheDomainTheElementTheIndexAndTheArray: every iteration
// method passes its callback the caller's domain, the element, its index as
// a number, and the array itself, not a copy.
func TestCallbacksGetTheDomainTheElementTheIndexAndTheArray(t *testing.T) {
	t.Parallel()

	d := newDomain()
	xs := NewArray("a", "b")
	var seen []string
	note := func(name string) func(*Domain, string, float64, *Array[string]) bool {
		return func(got *Domain, v string, i float64, arr *Array[string]) bool {
			seen = append(seen, fmt.Sprintf("%s %s %v %t %t", name, v, i, got == d, arr == xs))
			return name == "every"
		}
	}
	Map(d, xs, func(got *Domain, v string, i float64, arr *Array[string]) bool { return note("map")(got, v, i, arr) })
	xs.Filter(d, note("filter"))
	xs.ForEach(d, func(got *Domain, v string, i float64, arr *Array[string]) { note("forEach")(got, v, i, arr) })
	xs.Some(d, note("some"))
	xs.Every(d, note("every"))
	xs.FindIndex(d, note("findIndex"))
	Reduce(d, xs, func(got *Domain, acc int, v string, i float64, arr *Array[string]) int {
		note("reduce")(got, v, i, arr)
		return acc
	}, 0)
	want := []string{"map", "filter", "forEach", "some", "every", "findIndex", "reduce"}
	if len(seen) != 2*len(want) {
		t.Fatalf("the callbacks saw %q", seen)
	}
	for i, name := range want {
		for j, v := range []string{"a", "b"} {
			if got, want := seen[2*i+j], fmt.Sprintf("%s %s %d true true", name, v, j); got != want {
				t.Errorf("%s's callback saw %q, want %q", name, got, want)
			}
		}
	}
}

// visitor is the callback the iteration methods are driven with here: a
// predicate, whose result forEach and reduce ignore and every negates, so
// that false goes on to the next element for each of them.
type visitor = func(*Domain, float64, float64, *Array[float64]) bool

// iterations runs each iteration method over xs with fn, in d.
func iterations(d *Domain) map[string]func(xs *Array[float64], fn visitor) {
	return map[string]func(*Array[float64], visitor){
		"map":       func(xs *Array[float64], fn visitor) { Map(d, xs, fn) },
		"filter":    func(xs *Array[float64], fn visitor) { xs.Filter(d, fn) },
		"some":      func(xs *Array[float64], fn visitor) { xs.Some(d, fn) },
		"findIndex": func(xs *Array[float64], fn visitor) { xs.FindIndex(d, fn) },
		"every": func(xs *Array[float64], fn visitor) {
			xs.Every(d, func(d *Domain, x, i float64, a *Array[float64]) bool { return !fn(d, x, i, a) })
		},
		"forEach": func(xs *Array[float64], fn visitor) {
			xs.ForEach(d, func(d *Domain, x, i float64, a *Array[float64]) { fn(d, x, i, a) })
		},
		"reduce": func(xs *Array[float64], fn visitor) {
			Reduce(d, xs, func(d *Domain, acc, x, i float64, a *Array[float64]) float64 { fn(d, x, i, a); return acc }, 0)
		},
	}
}

// TestIterationReadsTheLengthOnce: an element pushed by the callback isn't
// visited, and one it replaces ahead of the iteration is seen. filter keeps
// the value it read before calling the callback.
//
//	fe = [1, 2, 3]; fe.forEach((x, i, a) => { log(x); if (i === 0) a.push(99) })  // 1 2 3
//	[1, 2, 3].map((x, i, a) => { if (i === 0) a[2] = 30; return x * 10 })        // [10, 20, 300]
//	[1, 2].reduce((acc, x, i, a) => { if (i === 0) a.push(100); return acc + x }, 0)  // 3
//	fr = [1, 2, 3]; fr.filter((x, i, a) => { a[i] = x * 100; return true })        // [1, 2, 3], fr [100, 200, 300]
func TestIterationReadsTheLengthOnce(t *testing.T) {
	t.Parallel()

	d := newDomain()
	fe := NewArray(1.0, 2.0, 3.0)
	var visited []float64
	fe.ForEach(d, func(_ *Domain, x, i float64, a *Array[float64]) {
		visited = append(visited, x)
		if i == 0 {
			a.Push(99)
		}
	})
	if fmt.Sprint(visited) != "[1 2 3]" || fe.Len() != 4 {
		t.Errorf("forEach visited %v of %v", visited, fe.items)
	}

	mapped := Map(d, NewArray(1.0, 2.0, 3.0), func(_ *Domain, x, i float64, a *Array[float64]) float64 {
		if i == 0 {
			a.Set(2, 30)
		}
		return x * 10
	})
	if fmt.Sprint(mapped.items) != "[10 20 300]" {
		t.Errorf("map gave %v, want the replaced element seen", mapped.items)
	}

	sum := Reduce(d, NewArray(1.0, 2.0), func(_ *Domain, acc, x, i float64, a *Array[float64]) float64 {
		if i == 0 {
			a.Push(100)
		}
		return acc + x
	}, 0)
	if sum != 3 {
		t.Errorf("reduce gave %v, want 3: the pushed element visited", sum)
	}

	fr := NewArray(1.0, 2.0, 3.0)
	kept := fr.Filter(d, func(_ *Domain, x, i float64, a *Array[float64]) bool {
		a.Set(i, x*100)
		return true
	})
	if fmt.Sprint(kept.items, fr.items) != "[1 2 3] [100 200 300]" {
		t.Errorf("filter kept %v of %v", kept.items, fr.items)
	}

	// A push on each of the 3 visits: each method calls back 3 times, as
	// Node does, where a loop reading the length live would go on to the
	// pushed elements. The pushes stop at 6, so that one ends.
	for name, run := range iterations(d) {
		calls, xs := 0, NewArray(1.0, 2.0, 3.0)
		run(xs, func(_ *Domain, _, _ float64, a *Array[float64]) bool {
			calls++
			if a.Len() < 6 {
				a.Push(0)
			}
			return false
		})
		if calls != 3 || xs.Len() != 6 {
			t.Errorf("%s pushing on every visit called back %d times, leaving %d elements: want 3 and 6", name, calls, xs.Len())
		}
	}
}

// TestIterationPanicsWhenTheArrayShrinks (D9): where Node skips an element
// the callback removed, or reads undefined for findIndex, cakebear panics,
// saying so. A shrink while the last element is visited leaves nothing to
// skip, and doesn't.
func TestIterationPanicsWhenTheArrayShrinks(t *testing.T) {
	t.Parallel()

	d := newDomain()
	// shrinkAt returns a fresh array of n and a callback that cuts it to
	// keep elements while visiting index at.
	shrinkAt := func(n, at, keep int) (*Array[float64], visitor) {
		return NewArray(make([]float64, n)...), func(_ *Domain, _, i float64, a *Array[float64]) bool {
			if int(i) == at {
				a.SetLength(float64(keep))
			}
			return false
		}
	}
	for method, want := range map[string]string{
		"forEach":   "cakebear: the array shrank to 2 elements while forEach ran over 4 (Node skips the rest)",
		"map":       "cakebear: the array shrank to 2 elements while map ran over 4 (Node skips the rest)",
		"filter":    "while filter ran over 4 (Node skips the rest)",
		"some":      "while some ran over 4 (Node skips the rest)",
		"every":     "while every ran over 4 (Node skips the rest)",
		"findIndex": "cakebear: the array shrank to 2 elements while findIndex ran over 4 (Node reads undefined)",
		"reduce":    "while reduce ran over 4 (Node skips the rest)",
	} {
		run := iterations(d)[method]
		xs, fn := shrinkAt(4, 1, 2)
		if got := panicOf(func() { run(xs, fn) }); !strings.Contains(got, want) {
			t.Errorf("%s over a shrinking array panicked with %q, want %q", method, got, want)
		}
		xs, fn = shrinkAt(4, 3, 0)
		if got := panicOf(func() { run(xs, fn) }); got != "" {
			t.Errorf("%s shrinking the array at its last element panicked with %q", method, got)
		}
	}
	xs, fn := shrinkAt(3, 0, 1)
	if got := panicOf(func() { xs.Some(d, fn) }); !strings.Contains(got, "shrank to 1 element while some ran over 3") {
		t.Errorf("a shrink to one element panicked with %q", got)
	}
	if n := len(iterations(d)); n != 7 {
		t.Errorf("%d iteration methods are tested, want all 7", n)
	}
}

// TestMapFilterAndReduceResults: map to another element type, filter to a
// new array that aliases nothing, reduce from an initial value of another
// type, and the empty cases.
//
//	[1, 2.5, -0].map((x) => "n" + x)          // ["n1", "n2.5", "n0"]
//	[1, 2, 3, 4].filter((x) => x % 2 === 0)   // [2, 4]
//	[1, 2, 3].reduce((acc, x) => acc + x, "") // "123"
//	[1, 2, 3].reduce((acc, x, i) => acc + x * i, 100) // 108
//	[].reduce((acc, x) => acc + x, 42)       // 42
func TestMapFilterAndReduceResults(t *testing.T) {
	t.Parallel()

	d := newDomain()
	strs := Map(d, NewArray(1.0, 2.5, math.Copysign(0, -1)), func(_ *Domain, x, _ float64, _ *Array[float64]) string {
		return "n" + NumberToString(x)
	})
	if fmt.Sprintf("%q", strs.items) != `["n1" "n2.5" "n0"]` {
		t.Errorf("map gave %q", strs.items)
	}

	xs := NewArray(1.0, 2.0, 3.0, 4.0)
	even := xs.Filter(d, func(_ *Domain, x, _ float64, _ *Array[float64]) bool { return math.Mod(x, 2) == 0 })
	all := xs.Filter(d, func(*Domain, float64, float64, *Array[float64]) bool { return true })
	all.Set(0, -1)
	mapped := Map(d, xs, func(_ *Domain, x, _ float64, _ *Array[float64]) float64 { return x })
	mapped.Set(1, -2)
	if fmt.Sprint(even.items, all.items, xs.items) != "[2 4] [-1 2 3 4] [1 2 3 4]" {
		t.Errorf("filter gave %v and %v of %v: a result aliases the array", even.items, all.items, xs.items)
	}
	none := xs.Filter(d, func(*Domain, float64, float64, *Array[float64]) bool { return false })
	if none.Len() != 0 || Map(d, NewArray[float64](), func(*Domain, float64, float64, *Array[float64]) bool { return true }).Len() != 0 {
		t.Error("filtering out everything, or mapping nothing, gave elements")
	}

	if got := Reduce(d, NewArray(1.0, 2.0, 3.0), func(_ *Domain, acc string, x, _ float64, _ *Array[float64]) string {
		return acc + NumberToString(x)
	}, ""); got != "123" {
		t.Errorf("reduce to a string gave %q", got)
	}
	if got := Reduce(d, NewArray(1.0, 2.0, 3.0), func(_ *Domain, acc, x, i float64, _ *Array[float64]) float64 {
		return acc + x*i
	}, 100); got != 108 {
		t.Errorf("reduce gave %v, want 108", got)
	}
	if got := Reduce(d, NewArray[float64](), func(*Domain, float64, float64, float64, *Array[float64]) float64 { return 0 }, 42); got != 42 {
		t.Errorf("reducing nothing gave %v, want the initial value", got)
	}
}

// TestSomeEveryAndFindIndexStopEarly: each stops at the element that
// decides it.
//
//	[1, 2, 3, 4].some((x) => x === 2)  // true, 2 calls
//	[1, 2, 3, 4].every((x) => x < 2)   // false, 2 calls
//	[].some(() => true), [].every(() => false)  // false, true
//	[5, 6, 7].findIndex((x) => x === 7), [5, 6].findIndex((x) => x === 9)  // 2, -1
func TestSomeEveryAndFindIndexStopEarly(t *testing.T) {
	t.Parallel()

	d := newDomain()
	xs := NewArray(1.0, 2.0, 3.0, 4.0)
	calls := 0
	counted := func(f func(float64) bool) func(*Domain, float64, float64, *Array[float64]) bool {
		calls = 0
		return func(_ *Domain, x, _ float64, _ *Array[float64]) bool { calls++; return f(x) }
	}
	if got := xs.Some(d, counted(func(x float64) bool { return x == 2 })); !got || calls != 2 {
		t.Errorf("some = %t after %d calls, want true after 2", got, calls)
	}
	if got := xs.Every(d, counted(func(x float64) bool { return x < 2 })); got || calls != 2 {
		t.Errorf("every = %t after %d calls, want false after 2", got, calls)
	}
	if got := xs.FindIndex(d, counted(func(x float64) bool { return x == 3 })); got != 2 || calls != 3 {
		t.Errorf("findIndex = %v after %d calls, want 2 after 3", got, calls)
	}
	if got := xs.FindIndex(d, counted(func(x float64) bool { return x == 9 })); got != -1 || calls != 4 {
		t.Errorf("findIndex of nothing = %v after %d calls, want -1 after 4", got, calls)
	}
	empty := NewArray[float64]()
	if empty.Some(d, counted(func(float64) bool { return true })) || !empty.Every(d, counted(func(float64) bool { return false })) {
		t.Error("some of nothing is true, or every of nothing false")
	}
}
