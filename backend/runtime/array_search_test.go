package runtime

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

// The searches, slice and sort. As for the iteration methods, the expected
// values are what Node 24.18 gave for the same calls.

// TestIncludesAndIndexOf: includes is SameValueZero and indexOf strict
// equality, which differ only for NaN; both take -0 for 0, and an object
// only for itself.
//
//	ns = [1, NaN, -0, 3]
//	ns.includes(NaN), ns.indexOf(NaN)   // true, -1
//	ns.includes(0), ns.indexOf(0)       // true, 2
//	[0].includes(-0), [0].indexOf(-0)   // true, 0
//	ns.indexOf(3), ns.indexOf(9)        // 3, -1
func TestIncludesAndIndexOf(t *testing.T) {
	t.Parallel()

	nan, negZero := math.NaN(), math.Copysign(0, -1)
	ns := NewArray(1, nan, negZero, 3)
	for _, tt := range []struct {
		name     string
		xs       *Array[float64]
		v        float64
		includes bool
		index    float64
	}{
		{"NaN", ns, nan, true, -1},
		{"0 for -0", ns, 0, true, 2},
		{"-0 for 0", NewArray(0.0), negZero, true, 0},
		{"3", ns, 3, true, 3},
		{"9", ns, 9, false, -1},
		{"NaN where there is none", NewArray(1.0, 2.0), nan, false, -1},
	} {
		if got := Includes(tt.xs, tt.v); got != tt.includes {
			t.Errorf("includes(%s) = %t", tt.name, got)
		}
		if got := IndexOf(tt.xs, tt.v); got != tt.index {
			t.Errorf("indexOf(%s) = %v, want %v", tt.name, got, tt.index)
		}
	}
	if !Includes(NewArray(float32(nan)), float32(nan)) {
		t.Error("an f32 NaN isn't found")
	}
	a, b := newTestObject([]string{"x"}, []any{1.0}), newTestObject([]string{"x"}, []any{1.0})
	objs := NewArray(b, a)
	if Includes(NewArray(a), b) || !Includes(objs, a) || IndexOf(objs, a) != 1 {
		t.Error("objects aren't compared by identity")
	}
	if IndexOf(NewArray("a", "b", "b"), "b") != 1 {
		t.Error("indexOf isn't the first index")
	}
}

// TestSlice: each bound is ToIntegerOrInfinity, then counted from the end
// if negative, then clamped. Every row is Node's for [0, 1, 2, 3, 4].
func TestSlice(t *testing.T) {
	t.Parallel()

	nan, inf := math.NaN(), math.Inf(1)
	xs := NewArray(0.0, 1, 2, 3, 4)
	for _, tt := range []struct {
		bounds []float64
		want   string
	}{
		{nil, "[0 1 2 3 4]"},
		{[]float64{2}, "[2 3 4]"},
		{[]float64{-2}, "[3 4]"},
		{[]float64{1, 3}, "[1 2]"},
		{[]float64{1, -1}, "[1 2 3]"},
		{[]float64{-0.5}, "[0 1 2 3 4]"},
		{[]float64{1.9}, "[1 2 3 4]"},
		{[]float64{-1.9}, "[4]"},
		{[]float64{nan}, "[0 1 2 3 4]"},
		{[]float64{nan, 2}, "[0 1]"},
		{[]float64{10}, "[]"},
		{[]float64{-10}, "[0 1 2 3 4]"},
		{[]float64{3, 1}, "[]"},
		{[]float64{inf}, "[]"},
		{[]float64{-inf}, "[0 1 2 3 4]"},
		{[]float64{0, inf}, "[0 1 2 3 4]"},
		{[]float64{0, -inf}, "[]"},
		{[]float64{2, nan}, "[]"},
		{[]float64{-3, -1}, "[2 3]"},
		{[]float64{1.5, 3.5}, "[1 2]"},
		{[]float64{0, -0.5}, "[]"},
	} {
		got := xs.Slice(tt.bounds...)
		if s := fmt.Sprint(got.items); s != tt.want {
			t.Errorf("slice(%v) = %s, want %s", tt.bounds, s, tt.want)
		}
	}
	copied := xs.Slice(0, 2)
	copied.Push(9)
	copied.Set(0, -1)
	if fmt.Sprint(xs.items) != "[0 1 2 3 4]" {
		t.Errorf("writing to a slice reached the array: %v", xs.items)
	}
}

// TestSortIsStableAndInPlace: equal keys keep their order, whether the
// comparator says 0 or NaN for them, and the array sorted is the one
// returned.
//
//	keys = [a2, b1, c2, d1, e0, f2, g1]
//	keys.sort((p, q) => p.k === q.k ? NaN : p.k - q.k)  // ebdgacf
//	keys.sort((p, q) => q.k - p.k)                      // acfbdge
//	[3, 1, 2].sort(() => NaN)                           // 3,1,2
//	[3, 1, 2].sort((a, b) => b - a)                     // 3,2,1
func TestSortIsStableAndInPlace(t *testing.T) {
	t.Parallel()

	d := newDomain()
	type kv struct {
		n string
		k float64
	}
	keys := NewArray(kv{"a", 2}, kv{"b", 1}, kv{"c", 2}, kv{"d", 1}, kv{"e", 0}, kv{"f", 2}, kv{"g", 1})
	names := func(a *Array[kv]) string {
		var b strings.Builder
		for _, p := range a.items {
			b.WriteString(p.n)
		}
		return b.String()
	}
	if got := keys.Sort(d, func(_ *Domain, p, q kv) float64 {
		if p.k == q.k {
			return math.NaN()
		}
		return p.k - q.k
	}); got != keys || names(keys) != "ebdgacf" {
		t.Errorf("sort by key gave %s, want ebdgacf in the same array", names(got))
	}
	keys.Sort(d, func(_ *Domain, p, q kv) float64 { return q.k - p.k })
	if names(keys) != "acfbdge" {
		t.Errorf("sort by key descending gave %s, want acfbdge", names(keys))
	}

	// Long enough that an unstable sort would reorder equal keys:
	// slices.SortFunc insertion-sorts 12 elements or fewer, which is stable
	// by accident.
	//
	//	big = []; for (i = 0; i < 100; i++) big.push({ n: i, k: (i * 37) % 7 });
	//	big.sort((p, q) => p.k - q.k).map((p) => p.n).join(",")
	big := NewArray[kv]()
	for i := range 100 {
		big.Push(kv{strconv.Itoa(i), float64(i * 37 % 7)})
	}
	big.Sort(d, func(_ *Domain, p, q kv) float64 { return p.k - q.k })
	order := Map(d, big, func(_ *Domain, p kv, _ float64, _ *Array[kv]) string { return p.n }).Join(",")
	if want := "0,7,14,21,28,35,42,49,56,63,70,77,84,91,98,4,11,18,25,32,39,46,53,60,67,74,81,88,95," +
		"1,8,15,22,29,36,43,50,57,64,71,78,85,92,99,5,12,19,26,33,40,47,54,61,68,75,82,89,96," +
		"2,9,16,23,30,37,44,51,58,65,72,79,86,93,6,13,20,27,34,41,48,55,62,69,76,83,90,97," +
		"3,10,17,24,31,38,45,52,59,66,73,80,87,94"; order != want {
		t.Errorf("100 elements by 7 keys sorted to\n%s\nwant Node's\n%s", order, want)
	}
	for _, tt := range []struct {
		name string
		cmp  func(*Domain, float64, float64) float64
		want string
	}{
		{"NaN", func(*Domain, float64, float64) float64 { return math.NaN() }, "[3 1 2]"},
		{"-0", func(*Domain, float64, float64) float64 { return math.Copysign(0, -1) }, "[3 1 2]"},
		{"descending", func(_ *Domain, a, b float64) float64 { return b - a }, "[3 2 1]"},
		{"ascending", func(_ *Domain, a, b float64) float64 { return a - b }, "[1 2 3]"},
		{"Infinity", func(_ *Domain, a, b float64) float64 { return (a - b) * math.Inf(1) }, "[1 2 3]"},
	} {
		if got := fmt.Sprint(NewArray(3.0, 1, 2).Sort(d, tt.cmp).items); got != tt.want {
			t.Errorf("sort with a comparator giving %s = %s, want %s", tt.name, got, tt.want)
		}
	}
}

// TestSortCopiesOutAndWritesBack: a comparator sees the array unsorted, and
// one that changes the array gets what Node gives.
//
//	a = [3, 1, 2]; a.sort((x, y) => { once: a.push(0); return x - y })    // [1, 2, 3, 0]
//	b = [3, 1, 2]; b.sort((x, y) => { once: b.length = 0; return x - y }) // [1, 2, 3]
//	c = [3, 1, 2]; c.sort((x, y) => { saw c.join(); return x - y })       // only "3,1,2"
//	e = [3, 1, 2, 5, 4]; e.sort((x, y) => { once: e[0] = 100; return x - y }) // [1, 2, 3, 4, 5]
func TestSortCopiesOutAndWritesBack(t *testing.T) {
	t.Parallel()

	d := newDomain()
	once := func(f func()) func(*Domain, float64, float64) float64 {
		done := false
		return func(_ *Domain, x, y float64) float64 {
			if !done {
				done = true
				f()
			}
			return x - y
		}
	}
	for _, tt := range []struct {
		name   string
		items  []float64
		change func(*Array[float64])
		want   string
	}{
		{"a push", []float64{3, 1, 2}, func(a *Array[float64]) { a.Push(0) }, "[1 2 3 0]"},
		{"a shrink", []float64{3, 1, 2}, func(a *Array[float64]) { a.SetLength(0, nil) }, "[1 2 3]"},
		{"a write", []float64{3, 1, 2, 5, 4}, func(a *Array[float64]) { a.Set(0, 100) }, "[1 2 3 4 5]"},
	} {
		xs := NewArray(tt.items...)
		if got := fmt.Sprint(xs.Sort(d, once(func() { tt.change(xs) })).items); got != tt.want {
			t.Errorf("sort with %s in the comparator = %s, want %s", tt.name, got, tt.want)
		}
	}
	c := NewArray(3.0, 1, 2)
	c.Sort(d, func(_ *Domain, x, y float64) float64 {
		if got := c.Join(","); got != "3,1,2" {
			t.Errorf("the comparator saw %s, want the array unsorted", got)
		}
		return x - y
	})
}
