package runtime

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// finishWithin drives d until it is idle, failing rather than hanging if a
// spawn's result never wakes it.
func finishWithin(t *testing.T, d *Domain, limit time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.finish()
	}()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("the domain was still waiting after %v: a spawn's result never woke it", limit)
	}
}

// Two spawns run at once, not one after the other under the caller's token:
// each waits for the other to start. Run one after the other, the first
// would wait in vain. It used to time two 100 ms sleeps against a 180 ms
// limit, which a loaded machine could miss with the spawns in parallel.
func TestSpawnsRunInParallel(t *testing.T) {
	t.Parallel()

	d := newDomain()
	var started sync.WaitGroup
	started.Add(2)
	both := make(chan struct{})
	go func() {
		started.Wait()
		close(both)
	}()
	met := make(chan bool, 2)
	d.runTask(func() {
		for range 2 {
			SpawnVoid(d, func(*Domain) {
				started.Done()
				select {
				case <-both:
					met <- true
				case <-time.After(5 * time.Second):
					met <- false
				}
			})
		}
	})
	d.finish()
	for range 2 {
		if !<-met {
			t.Fatal("a spawn waited 5 s for the other to start; they ran one after the other")
		}
	}
}

// spawn returns at once; the caller's synchronous code runs on, and the result
// arrives later, in the caller's domain, where awaiting it gives the value.
func TestSpawnResultArrivesInTheCallersDomain(t *testing.T) {
	t.Parallel()

	d := newDomain()
	got, log := recorder()
	d.runTask(func() {
		p := Spawn(d, func(s *Domain) float64 {
			if s == d {
				t.Error("the spawned function ran in its caller's domain")
			}
			time.Sleep(10 * time.Millisecond)
			return 42
		})
		if p.d != d {
			t.Error("spawn's promise does not belong to the caller's domain")
		}
		Async(d, func(co *Co) struct{} {
			log("result " + NumberToString(Await(co, p)))
			return struct{}{}
		})
		log("caller carries on")
	})
	finishWithin(t, d, 5*time.Second)
	if want := []string{"caller carries on", "result 42"}; !slices.Equal(*got, want) {
		t.Errorf("logged %q, want %q", *got, want)
	}
}

// An async function spawned resolves with what its promise resolves to, once
// the spawned domain has run its own timers that far.
func TestSpawnAsyncWaitsForItsPromise(t *testing.T) {
	t.Parallel()

	d := newDomain()
	var result float64
	// Before runTask, which starts the spawn and so its 20 ms timer: timing
	// from after it lets the timer look early.
	start := time.Now()
	d.runTask(func() {
		p := SpawnAsync(d, func(s *Domain) *Promise[float64] {
			return Async(s, func(co *Co) float64 {
				Await(co, sleepFn(s, 20))
				return 7
			})
		})
		Async(d, func(co *Co) struct{} {
			result = Await(co, p)
			return struct{}{}
		})
	})
	d.finish()
	if result != 7 {
		t.Errorf("result = %v, want 7", result)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("resolved after %v, before the spawned 20 ms timer", elapsed)
	}
}

// A panic in a spawned function surfaces in its caller's domain, where its
// result would have arrived, not on a goroutine nothing recovers.
func TestSpawnPanicIsRaisedInTheCaller(t *testing.T) {
	t.Parallel()

	d := newDomain()
	d.runTask(func() {
		SpawnVoid(d, func(*Domain) { panic("boom in the spawn") })
	})
	if got := panicked(d.finish); got != "boom in the spawn" {
		t.Errorf("the caller's driver saw %q, want the spawn's panic", got)
	}
}

// A spawned async function whose promise can never settle does not hold its
// caller forever: once the spawned domain is idle, the caller is released,
// and the spawn's own promise simply stays pending, as in Node.
func TestNeverSettlingSpawnReleasesTheCaller(t *testing.T) {
	t.Parallel()

	d := newDomain()
	var p *Promise[struct{}]
	d.runTask(func() {
		p = SpawnAsync(d, func(s *Domain) *Promise[struct{}] {
			return NewPromiseVoid(s, func(*Domain, func(*Domain)) {})
		})
	})
	done := make(chan struct{})
	go func() {
		d.finish()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the caller's domain never went idle")
	}
	if p.settled {
		t.Error("a spawn whose promise never settles resolved anyway")
	}
}

// The main program waits for a spawn, as for a timer, then exits; output the
// spawn's result leads to is printed.
func TestRunWaitsForASpawn(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	got := runProgram(t, func(d *Domain, log func(string)) {
		p := Spawn(d, func(*Domain) string {
			time.Sleep(30 * time.Millisecond)
			return "from the spawn"
		})
		Async(d, func(co *Co) struct{} {
			log(Await(co, p))
			return struct{}{}
		})
		log("main done")
	})
	if want := "main done\nfrom the spawn\n"; got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
}

// Work a spawned domain still has after its result is delivered carries on,
// and keeps the process alive until it is done.
func TestSpawnedWorkAfterTheResultKeepsRunning(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	fired := make(chan struct{})
	d := newDomain()
	d.runTask(func() {
		Spawn(d, func(s *Domain) float64 {
			SetTimeout(s, func(*Domain) { close(fired) }, 50)
			return 1
		})
	})
	d.finish() // the result came back
	if liveHandles.Load() != 1 {
		t.Errorf("liveHandles = %d while the spawn's timer is pending, want 1", liveHandles.Load())
	}
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("the spawned domain's timer never fired")
	}
	waitUntil(t, "the spawned domain to stop keeping the process alive", func() bool { return liveHandles.Load() == 0 })
}

// 100 spawns from one domain, each running ordering.ts in its own domain,
// with results coming back concurrently: every result arrives, and under
// -race nothing is shared but through the inbox.
func TestManySpawnsUnderRace(t *testing.T) {
	t.Parallel()

	const n = 100
	d := newDomain()
	results := make([]string, n)
	d.runTask(func() {
		for i := range n {
			p := Spawn(d, func(s *Domain) string {
				var lines []string
				s.runTask(func() { orderingProgram(s, func(line string) { lines = append(lines, line) }) })
				s.finish()
				return strings.Join(lines, "|")
			})
			Async(d, func(co *Co) struct{} {
				results[i] = Await(co, p)
				return struct{}{}
			})
		}
	})
	d.finish()
	want := strings.Join(orderingWant, "|")
	for i, got := range results {
		if got != want {
			t.Fatalf("spawn %d returned %q, want %q", i, got, want)
		}
	}
}

// TestSpawnResultIsCopiedWhileWorkRemains (M4 plan D11): a spawned function
// that returns an array and leaves a timer that changes it hands its caller
// a copy, so the timer, running in parallel, can't change what the caller
// holds. One that leaves nothing behind hands over the array itself. Run
// with -race too: without the copy, the timer's push races the caller's
// read.
func TestSpawnResultIsCopiedWhileWorkRemains(t *testing.T) {
	t.Parallel()

	d := newDomain()
	var original, got, settled, settledGot *Array[float64]
	var lengthLater int
	// The spawned timer runs on its own goroutine after the caller's domain
	// may have finished, so it reports the original's length this way.
	pushed := make(chan int, 1)
	d.runTask(func() {
		busy := Spawn(d, func(s *Domain) *Array[float64] {
			xs := NewArray(1.0, 2.0)
			original = xs
			SetTimeout(s, func(*Domain) { pushed <- int(xs.Push(3)) }, 5)
			return xs
		})
		idle := Spawn(d, func(*Domain) *Array[float64] {
			settled = NewArray(7.0)
			return settled
		})
		Async(d, func(co *Co) struct{} {
			got = Await(co, busy)
			settledGot = Await(co, idle)
			SetTimeout(d, func(*Domain) { lengthLater = got.Len() }, 30)
			return struct{}{}
		})
	})
	finishWithin(t, d, 5*time.Second)
	if got == original {
		t.Fatal("the caller got the array its spawned timer still changes, not a copy")
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
	if settledGot != settled {
		t.Error("a spawn with nothing left to do copied its result anyway")
	}
}
