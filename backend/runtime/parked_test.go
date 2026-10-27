package runtime

import (
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"
)

// The parked registry (parked.go): a domain that will never run again ends
// the async calls still waiting in it, rather than leave a goroutine behind
// for each (M6 plan D7, done-check 7; the m6-go spike counted 100 left by
// 100 requests).

// never is a promise nothing settles.
func never(d *Domain) *Promise[struct{}] {
	return NewPromiseVoid(d, func(*Domain, func(*Domain), func(*Domain, any)) {})
}

// leftAfter is how many more goroutines than before are running once they
// settle at most 5 above it, or after 2 s.
func leftAfter(before int) int {
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+5 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	return runtime.NumGoroutine() - before
}

// TestAbandonedDomainsLeaveNoGoroutines: 100 requests each, every way a
// request's domain is abandoned with an async call parked in it, leave no
// goroutine behind: a throw with a call awaiting a timer, a rejection after
// the end with a call awaiting nothing, a response ended, or never ended,
// with a call awaiting a promise nothing settles, and work after the
// response that ends the same way.
func TestAbandonedDomainsLeaveNoGoroutines(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	base := serveAsync(t, func(d *Domain, req *IncomingMessage, res *ServerResponse) {
		waiting := func(on *Promise[struct{}]) {
			Async(d, func(co *Co) struct{} {
				Await(co, on)
				return struct{}{}
			})
		}
		switch req.URL() {
		case "/throws":
			waiting(sleepFn(d, 1))
			panic(&Thrown{V: "x"})
		case "/rejects":
			waiting(never(d))
			Async(d, func(co *Co) struct{} {
				res.End("partial")
				Await(co, tick(d))
				panic(&Thrown{V: "x"})
			})
		case "/ended":
			waiting(never(d))
			res.End("ok")
		case "/unended":
			waiting(never(d))
		case "/detached":
			res.End("ok")
			Async(d, func(co *Co) struct{} {
				Await(co, sleepFn(d, 1))
				waiting(never(d))
				return struct{}{}
			})
		}
	})
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	for _, path := range []string{"/throws", "/rejects", "/ended", "/unended", "/detached"} {
		before := runtime.NumGoroutine()
		for range 100 {
			fetch(client, "GET", base+path)
		}
		waitUntil(t, "the detached work to finish", func() bool { return liveHandles.Load() == 0 })
		left := leftAfter(before)
		t.Logf("100 requests to %s: %d goroutines more than before", path, left)
		if left > 5 {
			t.Errorf("100 requests to %s left %d goroutines behind, want none", path, left)
		}
	}
}

// TestFinishedSpawnsLeaveNoGoroutines: a spawned domain done with a call
// still awaiting a promise nothing settles ends it, and so does one that
// failed with a call parked.
func TestFinishedSpawnsLeaveNoGoroutines(t *testing.T) { //nolint:paralleltest // counts goroutines, which a parallel test would add to
	before := runtime.NumGoroutine()
	d := newDomain()
	d.runTask(func() {
		for range 100 {
			Spawn(d, func(s *Domain) float64 {
				Async(s, func(co *Co) struct{} {
					Await(co, never(s))
					return struct{}{}
				})
				return 1
			})
			p := SpawnAsync(d, func(s *Domain) *Promise[float64] {
				Async(s, func(co *Co) struct{} {
					Await(co, never(s))
					return struct{}{}
				})
				return Async(s, func(co *Co) float64 {
					Await(co, tick(s))
					panic(&Thrown{V: "x"})
				})
			})
			Async(d, func(co *Co) struct{} {
				Try(func() { Await(co, p) })
				return struct{}{}
			})
		}
	})
	finishWithin(t, d, 5*time.Second)
	left := leftAfter(before)
	t.Logf("200 spawns: %d goroutines more than before", left)
	if left > 5 {
		t.Errorf("200 spawns left %d goroutines behind, want none", left)
	}
}

// TestAbortRunsNoMoreOfTheProgram: an aborted call resumes into nothing,
// as a call that never resumes does under Node: not the code after its
// await, not a finally around it, not its caller's. Its promise stays
// pending.
func TestAbortRunsNoMoreOfTheProgram(t *testing.T) {
	t.Parallel()

	d := newDomain()
	got, log := recorder()
	var p *Promise[struct{}]
	d.runTask(func() {
		inner := Async(d, func(co *Co) struct{} {
			ts_t1 := Try(func() {
				Await(co, never(d))
				log("resumed")
			})
			log("finally")
			Rethrow(ts_t1)
			return struct{}{}
		})
		p = Async(d, func(co *Co) struct{} {
			Await(co, inner)
			log("the caller resumed")
			return struct{}{}
		})
	})
	if len(d.parked) != 2 {
		t.Fatalf("%d calls parked, want 2", len(d.parked))
	}
	d.abort()
	if len(*got) != 0 || len(d.parked) != 0 || p.settled {
		t.Errorf("after abort: logged %q, %d calls still parked, promise settled %v; want nothing, 0, false", *got, len(d.parked), p.settled)
	}
}

// TestParkedRegistryFollowsEveryAwait: calls resumed in an order other
// than they parked in leave the registry holding exactly the ones still
// waiting, each where its Co says.
func TestParkedRegistryFollowsEveryAwait(t *testing.T) {
	t.Parallel()

	d := newDomain()
	got, log := recorder()
	d.runTask(func() {
		for i, ms := range []float64{30, 10, 20, 40, 5} {
			Async(d, func(co *Co) struct{} {
				Await(co, sleepFn(d, ms))
				log(strconv.Itoa(i))
				return struct{}{}
			})
		}
	})
	check := func(when string) {
		for i, co := range d.parked {
			if co.parkedAt != i {
				t.Fatalf("%s: the call at %d says it is at %d", when, i, co.parkedAt)
			}
		}
	}
	check("all parked")
	for len(d.parked) > 0 {
		d.waitNext() // one timer, or several on a slow machine
		if len(*got)+len(d.parked) != 5 {
			t.Fatalf("%d resumed and %d parked, of 5", len(*got), len(d.parked))
		}
		check(fmt.Sprintf("with %d parked", len(d.parked)))
	}
	if want := []string{"4", "1", "2", "0", "3"}; !slices.Equal(*got, want) {
		t.Errorf("resumed %q, want %q", *got, want)
	}
}
