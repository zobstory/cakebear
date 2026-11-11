package runtime

import (
	"testing"
	"time"
)

// spin busy-waits, so that a timer comes due and a fetch completes while the
// domain is still running its task: when the loop next looks, both are ready.
func spin(d time.Duration) {
	for start := time.Now(); time.Since(start) < d; {
	}
}

// A fetch settles as I/O, after a timer already due (R18): Node's poll phase
// follows its timers phase.
func TestFetchCompletionComesAfterDueTimers(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	base := fetchServer(t)
	for range 20 {
		got := runProgram(t, func(d *Domain, log func(string)) {
			p := Fetch(d, base+"/hello", nil, nil)
			SetTimeout(d, func(*Domain) { log("timeout 0") }, 0)
			Async(d, func(co *Co) struct{} {
				Await(co, p)
				log("fetch resolved")
				return struct{}{}
			})
			spin(50 * time.Millisecond)
			log("sync done")
		})
		if want := "sync done\ntimeout 0\nfetch resolved\n"; got != want {
			t.Fatalf("printed\n%s\nwant, as Node orders them\n%s", got, want)
		}
	}
}

// A body read whose bytes are in settles when the microtask queue drains:
// after any chain of microtasks, before a timer (R19).
func TestBodyReadSettlesWhenTheMicrotasksDrain(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	base := fetchServer(t)
	got := runProgram(t, func(d *Domain, log func(string)) {
		Async(d, func(co *Co) struct{} {
			r := Await(co, Fetch(d, base+"/hello", nil, nil))
			Await(co, sleepFn(d, 30)) // the body has arrived by now
			SetTimeout(d, func(*Domain) { log("timeout 0") }, 0)
			text := r.Text(d, nil)
			Async(d, func(co *Co) struct{} {
				log("text " + Await(co, text))
				return struct{}{}
			})
			Async(d, func(co *Co) struct{} {
				for range 50 {
					Await(co, resolved(d))
				}
				log("chain of 50 done")
				return struct{}{}
			})
			return struct{}{}
		})
	})
	if want := "chain of 50 done\ntext hello, world\ntimeout 0\n"; got != want {
		t.Fatalf("printed\n%s\nwant, as Node orders them\n%s", got, want)
	}
}

func resolved(d *Domain) *Promise[struct{}] {
	p := &Promise[struct{}]{settlement: settlement{d: d}}
	p.resolve(struct{}{})
	return p
}

// A fetch in flight is pending work: the program waits for it (R17).
func TestFetchInFlightKeepsTheProgramAlive(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	base := fetchServer(t)
	got := runProgram(t, func(d *Domain, log func(string)) {
		p := Fetch(d, base+"/slow?ms=100", nil, nil)
		Async(d, func(co *Co) struct{} {
			r := Await(co, p)
			log("settled after main " + Await(co, r.Text(d, nil)))
			return struct{}{}
		})
		log("main's last line")
	})
	if want := "main's last line\nsettled after main slow\n"; got != want {
		t.Fatalf("printed\n%s\nwant\n%s", got, want)
	}
}
