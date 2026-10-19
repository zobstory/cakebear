package runtime

import (
	"math"
	"strings"
	"sync"
	"testing"
)

// TestMaybeEqualityIsJavaScripts: Go's == on two Maybes is ===, since an
// empty Maybe holds nothing but T's zero value.
func TestMaybeEqualityIsJavaScripts(t *testing.T) {
	t.Parallel()

	nan := math.NaN()
	for _, tt := range []struct {
		name string
		a, b Maybe[float64]
		want bool
	}{
		{"1 === 1", Some(1.0), Some(1.0), true},
		{"NaN !== NaN", Some(nan), Some(nan), false},
		{"0 === -0", Some(0.0), Some(math.Copysign(0, -1)), true},
		{"undefined === undefined", Maybe[float64]{}, Maybe[float64]{}, true},
		{"null === null", NullOf[float64](), NullOf[float64](), true},
		{"null !== undefined", NullOf[float64](), Maybe[float64]{}, false},
		{"0 !== undefined", Some(0.0), Maybe[float64]{}, false},
		{"0 !== null", Some(0.0), NullOf[float64](), false},
	} {
		if got := tt.a == tt.b; got != tt.want {
			t.Errorf("%s: == gave %v", tt.name, got)
		}
	}
	type obj struct{}
	o, p := &obj{}, &obj{}
	if Some(o) != Some(o) || Some(o) == Some(p) {
		t.Errorf("maybe objects compare by identity")
	}
}

func TestMaybeStates(t *testing.T) {
	t.Parallel()

	u, n, v := Maybe[string]{}, NullOf[string](), Some("")
	if !u.IsUndefined() || u.IsNull() || !u.IsEmpty() {
		t.Errorf("the zero Maybe is undefined")
	}
	if n.IsUndefined() || !n.IsNull() || !n.IsEmpty() {
		t.Errorf("NullOf is null")
	}
	if v.IsUndefined() || v.IsNull() || v.IsEmpty() {
		t.Errorf(`Some("") is a value, though a falsy one`)
	}
	for _, tt := range []struct {
		m    Maybe[float64]
		want string
	}{{Maybe[float64]{}, "undefined"}, {NullOf[float64](), "null"}, {Some(2.5), "2.5"}} {
		if got := MaybeToString(tt.m, NumberToString); got != tt.want {
			t.Errorf("MaybeToString = %q, want %q", got, tt.want)
		}
	}
}

// TestMustSaysWhatWasEmpty: an unwrap of an empty Maybe, where TypeScript's
// narrowing was unsound, panics naming the value and what Node does.
func TestMustSaysWhatWasEmpty(t *testing.T) {
	t.Parallel()

	if got := Some(3.0).Must("p.x"); got != 3 {
		t.Errorf("Must of 3 = %v", got)
	}
	for _, tt := range []struct {
		m    Maybe[float64]
		want string
	}{
		{Maybe[float64]{}, "cakebear: p.x is undefined where TypeScript had narrowed it to a value (Node carries on with undefined)"},
		{NullOf[float64](), "cakebear: p.x is null where TypeScript had narrowed it to a value (Node carries on with null)"},
	} {
		if got := panicked(func() { tt.m.Must("p.x") }); got != tt.want {
			t.Errorf("Must panicked %q, want %q", got, tt.want)
		}
	}
}

// TestUnwrapsSayWhatNodeDoes: each unwrap of an empty Maybe says whether
// the checker narrowed it or a ! asserted it, and what Node does with it:
// carry on, or throw the TypeError it throws for the access, in its own
// words, the state in place of $. One holding a value is that value,
// whatever the access.
func TestUnwrapsSayWhatNodeDoes(t *testing.T) {
	t.Parallel()

	const narrowed, asserted = " where TypeScript had narrowed it to a value", ", though a ! asserted it isn't"
	for _, tt := range []struct {
		method string
		unwrap func(Maybe[float64]) float64
		why    string
		node   string
	}{
		{"Must", func(m Maybe[float64]) float64 { return m.Must("p") }, narrowed, "carries on with $"},
		{"Assert", func(m Maybe[float64]) float64 { return m.Assert("p") }, asserted, "carries on with $"},
		{"MustRead", func(m Maybe[float64]) float64 { return m.MustRead("p", "'x'") }, narrowed,
			"throws TypeError: Cannot read properties of $ (reading 'x')"},
		{"AssertRead", func(m Maybe[float64]) float64 { return m.AssertRead("p", "'0'") }, asserted,
			"throws TypeError: Cannot read properties of $ (reading '0')"},
		{"MustWrite", func(m Maybe[float64]) float64 { return m.MustWrite("p", "'x'") }, narrowed,
			"throws TypeError: Cannot set properties of $ (setting 'x')"},
		{"AssertWrite", func(m Maybe[float64]) float64 { return m.AssertWrite("p", "an index") }, asserted,
			"throws TypeError: Cannot set properties of $ (setting an index)"},
		{"MustDelete", func(m Maybe[float64]) float64 { return m.MustDelete("p") }, narrowed,
			"throws TypeError: Cannot convert undefined or null to object"},
		{"AssertDelete", func(m Maybe[float64]) float64 { return m.AssertDelete("p") }, asserted,
			"throws TypeError: Cannot convert undefined or null to object"},
		{"MustIn", func(m Maybe[float64]) float64 { return m.MustIn("p", "'x'") }, narrowed,
			"throws TypeError: Cannot use 'in' operator to search for 'x' in $"},
		{"AssertIn", func(m Maybe[float64]) float64 { return m.AssertIn("p", "'x'") }, asserted,
			"throws TypeError: Cannot use 'in' operator to search for 'x' in $"},
		{"MustIterate", func(m Maybe[float64]) float64 { return m.MustIterate("p") }, narrowed, "throws TypeError: p is not iterable"},
		{"AssertIterate", func(m Maybe[float64]) float64 { return m.AssertIterate("p") }, asserted, "throws TypeError: p is not iterable"},
	} {
		if got := tt.unwrap(Some(3.0)); got != 3 {
			t.Errorf("%s of 3 = %v", tt.method, got)
		}
		for _, empty := range []Maybe[float64]{{}, NullOf[float64]()} {
			state := empty.emptyName()
			want := "cakebear: p is " + state + tt.why + " (Node " + strings.ReplaceAll(tt.node, "$", state) + ")"
			if got := panicked(func() { tt.unwrap(empty) }); got != want {
				t.Errorf("%s of %s panicked %q, want %q", tt.method, state, got, want)
			}
		}
	}
}

// TestFormatWritesAMaybeStringAsAString: at console.log's top level a string
// is written as it is, and so is one a Maybe holds; inside a container it is
// quoted, as any string is.
func TestFormatWritesAMaybeStringAsAString(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		args []any
		want string
	}{
		{[]any{Some("x")}, "x"},
		{[]any{Some("x"), Some(1.0)}, "x 1"},
		{[]any{Maybe[string]{}}, "undefined"},
		{[]any{NullOf[string](), Some("y")}, "null y"},
		{[]any{NewArray(Some("x"), Maybe[string]{})}, "[ 'x', undefined ]"},
	} {
		if got := Format(tt.args...); got != tt.want {
			t.Errorf("Format(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
	if got := panicked(func() { Format(Some("%d"), 1.0) }); !strings.Contains(got, "format string") {
		t.Errorf("a Maybe first argument holding %% isn't treated as a format: %q", got)
	}
}

// TestMaybeArraysPadAsArraysDo: an array of maybe numbers, every one of them
// a number, groups and pads as a number[] does; one empty among the first 101
// makes it pad as other arrays do, and one past them changes nothing.
func TestMaybeArraysPadAsArraysDo(t *testing.T) {
	t.Parallel()

	plain := make([]float64, 30)
	maybes := make([]Maybe[float64], 30)
	for i := range plain {
		plain[i] = float64(i * i * 7 % 1000)
		maybes[i] = Some(plain[i])
	}
	if a, b := Inspect(NewArray(plain...)), Inspect(NewArray(maybes...)); a != b {
		t.Errorf("all present, a maybe array printed\n%s\nwhere the number[] printed\n%s", b, a)
	}
	if !NewArray(maybes...).numeric() {
		t.Errorf("all present: not numeric")
	}
	maybes[29] = Maybe[float64]{}
	if NewArray(maybes...).numeric() {
		t.Errorf("an undefined among them: still numeric")
	}
	long := make([]Maybe[float64], 105)
	for i := range long {
		long[i] = Some(float64(i))
	}
	long[104] = Maybe[float64]{}
	if !NewArray(long...).numeric() {
		t.Errorf("an undefined past the first 101: no longer numeric")
	}
}

// TestCloneCopiesWhatAMaybeHolds: a spawn's result is copied (D11), and a
// Maybe holding an object or an array is copied with what it holds, which
// would otherwise be shared between two domains.
func TestCloneCopiesWhatAMaybeHolds(t *testing.T) {
	t.Parallel()

	obj := newTestObject([]string{"a"}, []any{1.0})
	m := Some[Object](obj)
	copied := Clone(m)
	if copied.Must("m") == Object(obj) {
		t.Fatalf("the object a Maybe holds was shared, not copied")
	}
	if Inspect(copied) != Inspect(m) {
		t.Errorf("the copy prints %s, the original %s", Inspect(copied), Inspect(m))
	}
	arr := NewArray(Some[Object](obj), Maybe[Object]{})
	if got := Clone(arr).Get(0).Must("arr[0]"); got == Object(obj) {
		t.Errorf("an object in an array of maybes was shared")
	}
	if Clone(Maybe[Object]{}) != (Maybe[Object]{}) {
		t.Errorf("an undefined Maybe didn't clone to undefined")
	}
}

// TestLayoutSteps: writing a property an object lacks appends its key, where
// Node puts a property assigned after the object was made; deleting it
// removes it; and the tables objects reach are shared.
func TestLayoutSteps(t *testing.T) {
	t.Parallel()

	base := NewLayout([]string{"a"}, []int{0})
	withB := base.With(1, "b")
	if got := strings.Join(withB.keys, ","); got != "a,b" {
		t.Fatalf("a then b: keys %s", got)
	}
	if base.With(1, "b") != withB || withB.With(1, "b") != withB {
		t.Errorf("the same step made a new table")
	}
	if !withB.Has(1) || base.Has(1) {
		t.Errorf("Has: base %v, withB %v", base.Has(1), withB.Has(1))
	}
	readded := withB.Without(0).With(0, "a")
	if got := strings.Join(readded.keys, ","); got != "b,a" {
		t.Errorf("deleted and assigned again, a goes last, as in Node: keys %s", got)
	}
	if got := strings.Join(withB.Without(1).keys, ","); got != "a" {
		t.Errorf("deleting b from a,b: keys %s", got)
	}
	if base.Without(5) != base {
		t.Errorf("deleting a property the table lacks changed it")
	}
	o := newTestObject([]string{"a"}, []any{1.0})
	o.values = append(o.values, Some("x"))
	o.layout = o.layout.With(1, "b")
	if got := Inspect(o); got != "{ a: 1, b: 'x' }" {
		t.Errorf("printed %s", got)
	}
	if got := JSONStringify(o, JSONGap{}); got != `{"a":1,"b":"x"}` {
		t.Errorf("serialised %s", got)
	}
}

// TestLayoutStepsAcrossGoroutines: tables are package-level, so requests
// running in parallel take the same steps from the same table at once. Every
// goroutine, every time, gets the one table a step leads to, the first time
// included, when several miss at once; and the race detector (the runtime
// gate runs under -race) sees no race.
func TestLayoutStepsAcrossGoroutines(t *testing.T) {
	t.Parallel()

	for round := range 20 {
		base := NewLayout([]string{"a"}, []int{0})
		const workers = 16
		var mu sync.Mutex
		seen := [3]map[*Layout]bool{{}, {}, {}}
		var start, done sync.WaitGroup
		start.Add(1)
		for range workers {
			done.Go(func() {
				start.Wait()
				for range 50 {
					got := [3]*Layout{base.With(1, "b"), base.With(2, "c").With(1, "b"), base.With(1, "b").Without(0)}
					mu.Lock()
					for i, l := range got {
						seen[i][l] = true
					}
					mu.Unlock()
				}
			})
		}
		start.Done()
		done.Wait()
		for i, tables := range seen {
			if len(tables) != 1 {
				t.Fatalf("round %d: step %d led to %d tables, want 1", round, i, len(tables))
			}
		}
	}
}

func BenchmarkLayoutWith(b *testing.B) {
	base := NewLayout([]string{"a"}, []int{0})
	base.With(1, "b")
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if base.With(1, "b") == nil {
				b.Fatal("nil")
			}
		}
	})
}
