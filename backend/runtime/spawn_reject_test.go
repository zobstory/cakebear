package runtime

import (
	"strings"
	"testing"
	"time"
)

// A spawn's rejection crossing to its caller (M6 plan D10, done-check 8),
// and what a spawned domain does once its result has gone.

// TestSpawnRejectionIsCopiedWhileWorkRemains: a spawned function that
// throws an object its own timer still changes rejects with a copy, so the
// timer, running in parallel, can't change what the caller caught. One
// that leaves nothing behind rejects with the object itself. Run with -race
// too: without the copy, the timer's push races the caller's read.
func TestSpawnRejectionIsCopiedWhileWorkRemains(t *testing.T) {
	t.Parallel()

	d := newDomain()
	var original, idle, caught, caughtIdle any
	var lengthLater int
	pushed := make(chan int, 1)
	d.runTask(func() {
		busy := Spawn(d, func(s *Domain) float64 {
			xs := NewArray(1.0, 2.0)
			original = xs
			SetTimeout(s, func(*Domain) { pushed <- int(xs.Push(3)) }, 5)
			panic(&Thrown{V: xs})
		})
		quiet := Spawn(d, func(*Domain) float64 {
			idle = NewArray(7.0)
			panic(&Thrown{V: idle})
		})
		// Each awaited from the start: a rejection arriving with nothing
		// awaiting it would end d (R11).
		Async(d, func(co *Co) struct{} {
			caught = Try(func() { Await(co, busy) }).V
			SetTimeout(d, func(*Domain) { lengthLater = caught.(*Array[float64]).Len() }, 30)
			return struct{}{}
		})
		Async(d, func(co *Co) struct{} {
			caughtIdle = Try(func() { Await(co, quiet) }).V
			return struct{}{}
		})
	})
	finishWithin(t, d, 5*time.Second)
	if caught == original {
		t.Fatal("the caller caught the array its spawned timer still changes, not a copy")
	}
	var originalLength int
	select {
	case originalLength = <-pushed:
	case <-time.After(5 * time.Second):
		t.Fatal("the spawned timer never ran")
	}
	if lengthLater != 2 || originalLength != 3 {
		t.Errorf("after the spawned timer, the caller's copy has %d elements and the original %d; want 2 and 3", lengthLater, originalLength)
	}
	if caughtIdle != idle {
		t.Error("a spawn with nothing left to do copied what it threw anyway")
	}
}

// TestSpawnResultIsCopiedWhileACallWaits: work a spawned domain has left
// isn't only its timers. An async call parked there, which a microtask
// queued behind the result will resume, can change the result as the
// caller reads it, so the result is a copy then too. Before the parked
// registry it went as it was, a data race -race reports.
func TestSpawnResultIsCopiedWhileACallWaits(t *testing.T) {
	t.Parallel()

	d := newDomain()
	var original, got *Array[float64]
	d.runTask(func() {
		p := Spawn(d, func(s *Domain) *Array[float64] {
			xs := NewArray(1.0)
			original = xs
			Async(s, func(co *Co) struct{} {
				Await(co, tick(s))
				xs.Push(2)
				return struct{}{}
			})
			return xs
		})
		Async(d, func(co *Co) struct{} {
			got = Await(co, p)
			return struct{}{}
		})
	})
	finishWithin(t, d, 5*time.Second)
	if got == original {
		t.Fatal("the caller got the array a call parked in the spawn still changes, not a copy")
	}
	if got.Len() != 1 {
		t.Errorf("the caller's copy has %d elements, want 1, as it was returned", got.Len())
	}
}

// TestSpawnFailureAfterItsResult: a throw in a spawned timer after the
// result went has nothing to reject. Main spawned it, so it ends the
// program, as the throw would have had main run it, reported as an uncaught
// throw in a timer is.
func TestSpawnFailureAfterItsResult(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	const text = `    throw new Error("after the result");`
	stdout, stderr, code := runAsMain(t, func(d *Domain) {
		ts_module := Async(d, func(co *Co) struct{} {
			Log(Await(co, Spawn(d, func(s *Domain) float64 {
				SetTimeout(s, func(*Domain) {
					panic(thrownError("x.ts", 4, 5, 11, text, ErrorBase, "after the result"))
				}, 5)
				return 1
			})))
			return struct{}{}
		})
		RunModule(ts_module, "/CASES/x.ts")
	})
	want := "file:///CASES/x.ts:4\n" + text + "\n    ^\n\n[Error: after the result]\n\ncakebear " + Version + "\n"
	if stdout != "1\n" || stderr != want || code != 1 {
		t.Errorf("printed %q, reported\n%s\nexited %d; want %q, exit 1 and\n%s", stdout, stderr, code, "1\n", want)
	}
}

// TestSpawnFailureAfterItsResultInARequest: a request's spawn does the
// same, but ends only itself, as a request's own work after its response
// does (D8): the report goes to stderr and the process carries on.
func TestSpawnFailureAfterItsResultInARequest(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	_, stderr, exits := isolate(t)
	d := newDomain() // a request's, not main's
	var got float64
	d.runTask(func() {
		p := Spawn(d, func(s *Domain) float64 {
			SetTimeout(s, func(*Domain) { panic(&Thrown{V: "after the result"}) }, 5)
			return 1
		})
		Async(d, func(co *Co) struct{} {
			got = Await(co, p)
			return struct{}{}
		})
	})
	finishWithin(t, d, 5*time.Second)
	waitUntil(t, "the report", func() bool { return strings.Contains(stderr.String(), "after the result") })
	waitUntil(t, "the spawn to stop keeping the process alive", func() bool { return liveHandles.Load() == 0 })
	if want := "after the result\n\ncakebear " + Version + "\n"; got != 1 || stderr.String() != want || len(exits) != 0 {
		t.Errorf("got %v, reported %q, exited %d times; want 1, %q, none", got, stderr.String(), len(exits), want)
	}
}
