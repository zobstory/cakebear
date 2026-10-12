package runtime

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The M2 target programs, written by hand the way M2's emitter will write
// them: every function takes its caller's domain first, an async body runs in
// Async, and await is Await. Each logs through log, so one test can compare a
// program's stdout with Node's and another can run many copies at once.

func sleepFn(d *Domain, ms float64) *Promise[struct{}] {
	return NewPromiseVoid(d, func(d *Domain, resolve func(*Domain)) {
		SetTimeout(d, resolve, ms)
	})
}

// sleepProgram is targets-m2/sleep.ts.
func sleepProgram(d *Domain, log func(string)) {
	main := func(d *Domain) *Promise[struct{}] {
		return Async(d, func(co *Co) struct{} {
			log("a")
			Await(co, sleepFn(d, 20))
			log("b")
			return struct{}{}
		})
	}
	main(d)
	log("c")
}

// orderingProgram is targets-m2/ordering.ts.
func orderingProgram(d *Domain, log func(string)) {
	now := func(d *Domain, n float64) *Promise[float64] {
		return Async(d, func(*Co) float64 { return n })
	}
	quick := func(d *Domain, name string) *Promise[float64] {
		return Async(d, func(co *Co) float64 {
			log(name + ": sync part")
			one := Await(co, now(d, 1))
			log(name + ": resumed (microtask)")
			return one
		})
	}
	slow := func(d *Domain, name string) *Promise[float64] {
		return Async(d, func(co *Co) float64 {
			log(name + ": before the timer")
			Await(co, sleepFn(d, 0))
			log(name + ": after the timer (macrotask)")
			return 2
		})
	}
	both := func(d *Domain) *Promise[struct{}] {
		return Async(d, func(co *Co) struct{} {
			a := Await(co, quick(d, "q"))
			b := Await(co, slow(d, "s"))
			if a+b == 3 {
				log("both: done")
			}
			return struct{}{}
		})
	}

	SetTimeout(d, func(*Domain) { log("timeout 0, scheduled first") }, 0)
	both(d)
	quick(d, "q2")
	log("top level done")
}

// orderingWant is what Node 24.18 prints for ordering.ts, every run (M2 plan §3).
var orderingWant = []string{
	"q: sync part",
	"q2: sync part",
	"top level done",
	"q: resumed (microtask)",
	"q2: resumed (microtask)",
	"s: before the timer",
	"timeout 0, scheduled first",
	"s: after the timer (macrotask)",
	"both: done",
}

// runProgram runs a program as the generated main will: its top-level code on
// the main domain, then Run. It returns what the program printed.
func runProgram(t *testing.T, program func(*Domain, func(string))) string {
	t.Helper()
	stdout, _, _ := isolate(t)
	program(Main(), LogString)
	waitFor(t, runAsync(nil), "the program to finish")
	Flush()
	return stdout.String()
}

// Byte for byte what Node prints, 20 runs in a row: a scheduler's ordering
// bugs show up only some of the time (M2 plan, check 2).
func TestOrderingMatchesNode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	want := strings.Join(orderingWant, "\n") + "\n"
	for run := range 20 { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(strconv.Itoa(run), func(t *testing.T) {
			if got := runProgram(t, orderingProgram); got != want {
				t.Fatalf("printed\n%s\nwant, as Node prints\n%s", got, want)
			}
		})
	}
}

// The part of an async function before its first await runs synchronously,
// so "a" prints before "c", and the rest waits for the timer (M2 check 1).
func TestSleepMatchesNode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	start := time.Now()
	if got, want := runProgram(t, sleepProgram), "a\nc\nb\n"; got != want {
		t.Fatalf("printed %q, want %q, as Node prints", got, want)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("finished after %v; the 20 ms timer cannot have fired", elapsed)
	}
}

// 100 domains at once, each running ordering.ts, must each still see Node's
// order. The logs are plain slices appended by whichever goroutine holds that
// domain's token: if a handoff ever let two run at once, -race reports it.
func TestManyDomainsKeepTheirOwnOrder(t *testing.T) {
	t.Parallel()

	const n = 100
	logs := make([][]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			d := newDomain()
			d.runTask(func() { orderingProgram(d, func(s string) { logs[i] = append(logs[i], s) }) })
			d.finish()
		})
	}
	wg.Wait()
	for i, got := range logs {
		if !slices.Equal(got, orderingWant) {
			t.Fatalf("domain %d logged %q, want %q", i, got, orderingWant)
		}
	}
}

// recorder logs into a slice, for tests that run one domain by hand.
func recorder() (*[]string, func(string)) {
	var got []string
	return &got, func(s string) { got = append(got, s) }
}

// An async body with no await runs to the end before Async returns, and its
// promise is already settled.
func TestAsyncRunsItsBodyBeforeReturning(t *testing.T) {
	t.Parallel()

	d := newDomain()
	got, log := recorder()
	p := Async(d, func(*Co) float64 {
		log("body")
		return 7
	})
	log("after the call")
	if want := []string{"body", "after the call"}; !slices.Equal(*got, want) {
		t.Errorf("logged %q, want %q", *got, want)
	}
	if !p.settled || p.value != 7 {
		t.Errorf("promise settled=%v value=%v, want settled with 7", p.settled, p.value)
	}
}

// Awaiting a promise that has already settled still gives way: the code after
// the await runs in a microtask, after the caller's synchronous code.
func TestAwaitOnASettledPromiseStillYields(t *testing.T) {
	t.Parallel()

	d := newDomain()
	got, log := recorder()
	d.runTask(func() {
		done := Async(d, func(*Co) float64 { return 1 })
		Async(d, func(co *Co) struct{} {
			log("before the await")
			log("awaited " + NumberToString(Await(co, done)))
			return struct{}{}
		})
		log("caller continues")
	})
	want := []string{"before the await", "caller continues", "awaited 1"}
	if !slices.Equal(*got, want) {
		t.Errorf("logged %q, want %q", *got, want)
	}
}

// Calls awaiting one promise resume in the order they awaited it.
func TestAwaitersResumeInOrder(t *testing.T) {
	t.Parallel()

	d := newDomain()
	got, log := recorder()
	d.runTask(func() {
		gate := sleepFn(d, 1)
		for _, name := range []string{"first", "second", "third"} {
			Async(d, func(co *Co) struct{} {
				Await(co, gate)
				log(name)
				return struct{}{}
			})
		}
	})
	d.finish()
	if want := []string{"first", "second", "third"}; !slices.Equal(*got, want) {
		t.Errorf("resumed %q, want %q", *got, want)
	}
}

// A second resolve does nothing, as in JavaScript.
func TestSecondResolveDoesNothing(t *testing.T) {
	t.Parallel()

	d := newDomain()
	var resolve func(*Domain, float64)
	p := NewPromise(d, func(_ *Domain, r func(*Domain, float64)) { resolve = r })
	if p.settled {
		t.Fatal("settled before resolve was called")
	}
	resolve(d, 1)
	resolve(d, 2)
	if p.value != 1 {
		t.Errorf("value = %v after resolve(1) then resolve(2), want 1", p.value)
	}
}

// A promise never crosses domains: lower/ refuses what would let it, and the
// runtime panics rather than race if one does.
func TestPromiseOutsideItsDomainPanics(t *testing.T) {
	t.Parallel()

	home, other := newDomain(), newDomain()
	var resolve func(*Domain)
	p := NewPromiseVoid(home, func(_ *Domain, r func(*Domain)) { resolve = r })

	awaited := panicked(func() {
		Async(other, func(co *Co) struct{} { return Await(co, p) })
	})
	if awaited != errCrossDomain {
		t.Errorf("awaiting another domain's promise panicked with %q, want %q", awaited, errCrossDomain)
	}
	if got := panicked(func() { resolve(other) }); got != errCrossDomain {
		t.Errorf("resolving from another domain panicked with %q, want %q", got, errCrossDomain)
	}
}

// A panic in an async body surfaces on the goroutine holding the token, as it
// would if the code had run there: in the caller before the first await, and
// in whoever resumed it after one.
func TestAsyncPanicReachesTheTokenHolder(t *testing.T) {
	t.Parallel()

	d := newDomain()
	if got := panicked(func() {
		Async(d, func(*Co) struct{} { panic("before any await") })
	}); got != "before any await" {
		t.Errorf("the caller saw %q, want the body's panic", got)
	}

	// After an await the body is resumed by a microtask, so the panic comes
	// out of the drain that ran it, on the domain's driver.
	resumed := newDomain()
	if got := panicked(func() {
		resumed.runTask(func() {
			Async(resumed, func(co *Co) struct{} {
				Await(co, Async(resumed, func(*Co) float64 { return 0 }))
				panic("after an await")
			})
		})
	}); got != "after an await" {
		t.Errorf("the driver saw %q, want the body's panic", got)
	}
}
