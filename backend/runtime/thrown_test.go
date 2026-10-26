package runtime

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// throws runs f and says how it ended: "" when it returned, String() of
// the Error it threw, as Node's String(e) writes it, or "threw " and the
// value as console.log prints it for a throw of anything else. A panic that
// isn't a throw is "panic: " and its value, so a test that wants a throw
// fails on one.
func throws(f func()) (got string) {
	defer func() {
		switch p := recover().(type) {
		case nil:
		case *Thrown:
			if e, ok := p.V.(*Error); ok {
				got = ErrorToString(e)
			} else {
				got = "threw " + Format(p.V)
			}
		default:
			got = fmt.Sprintf("panic: %v", p)
		}
	}()
	f()
	return ""
}

// threwString is String() of the Error p threw, when p, a recovered panic,
// is a throw of one: what the goldens record of Node's throws.
func threwString(p any) (string, bool) {
	if th, ok := p.(*Thrown); ok {
		if e, ok := th.V.(*Error); ok {
			return ErrorToString(e), true
		}
	}
	return "", false
}

// thrown runs f and returns what it threw, failing the test if it threw
// nothing, or panicked with something that isn't a throw.
func thrown(t *testing.T, f func()) *Thrown {
	t.Helper()
	var p any
	func() {
		defer func() { p = recover() }()
		f()
	}()
	th, ok := p.(*Thrown)
	if !ok {
		t.Fatalf("it panicked with %v, want a throw", p)
	}
	return th
}

// Try returns nothing for a block that completes, and the very *Thrown a
// block threw, so a catch binds the value thrown, identity kept (R18).
func TestTryCatchesWhatWasThrown(t *testing.T) {
	t.Parallel()

	if got := Try(func() {}); got != nil {
		t.Errorf("a block that completed threw %v", got)
	}
	err := NewError(ErrorBase, "m", nil)
	want := &Thrown{V: err}
	got := Try(func() { panic(want) })
	if got != want || got.V != any(err) {
		t.Errorf("Try gave %v, want the throw itself", got)
	}
	// TryK gives the block's completion code, or 0 with the throw.
	if k, th := TryK(func() int { return 2 }); k != 2 || th != nil {
		t.Errorf("TryK = %d, %v; want 2, nil", k, th)
	}
	if k, th := TryK(func() int { panic(want) }); k != 0 || th != want {
		t.Errorf("TryK of a throw = %d, %v; want 0 and the throw", k, th)
	}
}

var sink string

// A Go panic is no throw (D3): it passes Try, TryK and Rethrow unchanged,
// so no catch sees it, and keeps the frames that panicked.
func TestGoPanicsPassEveryTry(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("a cakebear bug")
	for name, run := range map[string]func(){
		"Try":         func() { _ = Try(func() { panic(sentinel) }) },
		"TryK":        func() { _, _ = TryK(func() int { panic(sentinel) }) },
		"nested":      func() { _ = Try(func() { _ = Try(func() { panic(sentinel) }) }) },
		"nil pointer": func() { _ = Try(func() { var o *Error; sink = o.message }) },
	} {
		var got any
		func() {
			defer func() { got = recover() }()
			run()
		}()
		switch name {
		case "nil pointer":
			if _, ok := got.(interface{ RuntimeError() }); !ok {
				t.Errorf("%s: recovered %v, want the Go runtime error", name, got)
			}
		default:
			if err, _ := got.(error); !errors.Is(err, sentinel) {
				t.Errorf("%s: recovered %v, want the panic itself", name, got)
			}
		}
	}
}

// Rethrow raises the pending throw, the same one, and nothing for none: the
// end of a finally.
func TestRethrowEndsAFinally(t *testing.T) {
	t.Parallel()

	Rethrow(nil)
	want := &Thrown{V: "x"}
	if got := Try(func() { Rethrow(want) }); got != want {
		t.Errorf("Rethrow raised %v, want %v", got, want)
	}
}

// A *Thrown nothing recovered prints as what it holds.
func TestThrownPrintsWhatItHolds(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		v    any
		want string
	}{
		{NewError(ErrorType, "t", nil), "Uncaught [TypeError: t]"},
		{"m", "Uncaught 'm'"},
		{nil, "Uncaught undefined"},
	} {
		if got := (&Thrown{V: c.v}).String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
	if !strings.HasPrefix(fmt.Sprint(&Thrown{V: 5.0}), "Uncaught 5") {
		t.Error("a *Thrown doesn't print through String()")
	}
}

// TestThrowsThroughCallbacksMatchNode: a throw from an array method's
// callback unwinds through the runtime to the try, and what was done before
// it stays done (R23): m6-node's c4-08, as the emitter writes it, prints
// what Node 24.18 printed. sort's comparator throwing on its 4th call
// leaves the array as it was, since the runtime sorts a copy: it never
// calls program code midway through changing a value (M6 plan D10).
func TestThrowsThroughCallbacksMatchNode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	stdout, _, _ := isolate(t)
	d := Main()
	catchMessage := func(body func()) string {
		th := Try(body)
		if th == nil {
			t.Fatal("nothing was thrown")
		}
		return Unbox[*Error](th.V, "e").Message()
	}
	throwError := func(m string) { panic(&Thrown{V: NewError(ErrorBase, m, nil)}) }

	xs := NewArray(1.0, 2.0, 3.0, 4.0)
	seen := NewArray[float64]()
	m := catchMessage(func() {
		xs.ForEach(d, func(_ *Domain, x float64, _ float64, _ *Array[float64]) {
			seen.Push(x)
			if x == 3 {
				throwError("forEach stopped at " + NumberToString(x))
			}
		})
	})
	Log(m, "seen", seen)

	mapped := NewArray(0.0)
	m = catchMessage(func() {
		mapped = Map(d, xs, func(_ *Domain, x float64, _ float64, _ *Array[float64]) float64 {
			if x == 2 {
				throwError("map stopped at " + NumberToString(x))
			}
			return x * 10
		})
	})
	Log(m, "mapped unchanged", mapped)

	ys := NewArray(5.0, 3.0, 1.0, 4.0, 2.0, 9.0, 7.0)
	calls := 0.0
	m = catchMessage(func() {
		ys.Sort(d, func(_ *Domain, a, b float64) float64 {
			calls++
			if calls == 4 {
				throwError("comparator threw on call " + NumberToString(calls))
			}
			return a - b
		})
	})
	Log(m, "array after", ys)

	zs := NewArray(3.0, 1.0, 2.0)
	m = catchMessage(func() {
		zs.Push(4)
		zs.Filter(d, func(_ *Domain, z float64, _ float64, _ *Array[float64]) bool {
			if z == 2 {
				throwError("filter threw")
			}
			return true
		})
	})
	Log(m, "zs", zs)

	acc := NewArray[string]()
	m = catchMessage(func() {
		Reduce(d, NewArray(1.0, 2.0, 3.0), func(_ *Domain, sum, x float64, _ float64, _ *Array[float64]) float64 {
			acc.Push("visit " + NumberToString(x))
			if x == 2 {
				throwError("reduce threw at " + NumberToString(x) + " with sum " + NumberToString(sum))
			}
			return sum + x
		}, 0)
	})
	Log(m, acc)
	Flush()

	const node = "forEach stopped at 3 seen [ 1, 2, 3 ]\nmap stopped at 2 mapped unchanged [ 0 ]\n" +
		"comparator threw on call 4 array after [\n  5, 3, 1, 4,\n  2, 9, 7\n]\nfilter threw zs [ 3, 1, 2, 4 ]\n" +
		"reduce threw at 2 with sum 1 [ 'visit 1', 'visit 2' ]\n"
	if got := stdout.String(); got != node {
		t.Errorf("printed\n%s\nwant, as Node printed\n%s", got, node)
	}
}
