package runtime

import (
	"container/heap"
	"math"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Timers fire by due time, and timers due together in the order they were
// scheduled. A delay of 0 is 1 ms, as in Node, so it ties with an explicit 1.
func TestTimersFireByDueTimeThenScheduleOrder(t *testing.T) {
	t.Parallel()

	d := newDomain()
	got, log := recorder()
	for _, tm := range []struct {
		name string
		ms   float64
	}{{"thirty", 30}, {"one", 1}, {"zero", 0}, {"also one", 1}, {"ten", 10}} {
		SetTimeout(d, func(*Domain) { log(tm.name) }, tm.ms)
	}
	d.finish()
	if want := []string{"one", "zero", "also one", "ten", "thirty"}; !slices.Equal(*got, want) {
		t.Errorf("fired %q, want %q", *got, want)
	}
}

// Timers due at the same instant fire in the order they were scheduled. Two
// time.Now calls in a row rarely return the same value here, so the test above
// almost never reaches the tie-break; on a coarse clock it decides the order
// of every pair of equal delays. This pins it directly.
func TestTimersDueTogetherFireInScheduleOrder(t *testing.T) {
	t.Parallel()

	same := time.Now()
	var h timerHeap
	for _, seq := range []uint64{4, 1, 5, 3, 2} {
		heap.Push(&h, &timer{when: same, seq: seq})
	}
	heap.Push(&h, &timer{when: same.Add(-time.Nanosecond), seq: 9}) // earlier wins over seq
	var got []uint64
	for h.Len() > 0 {
		got = append(got, heap.Pop(&h).(*timer).seq)
	}
	if want := []uint64{9, 1, 2, 3, 4, 5}; !slices.Equal(got, want) {
		t.Errorf("popped seq %v, want %v", got, want)
	}
}

func TestTimerDelayFollowsNode(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		ms   float64
		want time.Duration
	}{
		{0, time.Millisecond},
		{-5, time.Millisecond},
		{math.NaN(), time.Millisecond},
		{math.Inf(1), time.Millisecond},
		{math.MaxInt32 + 1, time.Millisecond}, // past 2^31-1 ms, Node falls back to 1
		{math.MaxInt32, math.MaxInt32 * time.Millisecond},
		{1.5, 1500 * time.Microsecond},
		{250, 250 * time.Millisecond},
	} {
		if got := timerDelay(tt.ms); got != tt.want {
			t.Errorf("timerDelay(%v) = %v, want %v", tt.ms, got, tt.want)
		}
	}
}

// A cleared timer never fires, even when cleared by an earlier timer, and
// clearing an id that has fired, was never issued, or is another domain's
// does nothing.
func TestClearTimeout(t *testing.T) {
	t.Parallel()

	d, other := newDomain(), newDomain()
	got, log := recorder()
	late := SetTimeout(d, func(*Domain) { log("late") }, 5)
	SetTimeout(d, func(d *Domain) {
		log("early")
		ClearTimeout(d, late)
	}, 1)
	kept := SetTimeout(d, func(*Domain) { log("kept") }, 2)
	ClearTimeout(other, kept) // not other's to clear
	ClearTimeout(d, 0)
	ClearTimeout(d, -1)

	d.finish()
	if want := []string{"early", "kept"}; !slices.Equal(*got, want) {
		t.Errorf("fired %q, want %q", *got, want)
	}
	ClearTimeout(d, kept) // already fired
	if d.pending() {
		t.Error("the domain still reports pending work")
	}
}

func TestTimerIDsAreDistinctAcrossDomains(t *testing.T) {
	t.Parallel()

	a, b := newDomain(), newDomain()
	ids := []float64{
		SetTimeout(a, func(*Domain) {}, 1), SetTimeout(b, func(*Domain) {}, 1),
		SetTimeout(a, func(*Domain) {}, 1), SetTimeout(b, func(*Domain) {}, 1),
	}
	for i, id := range ids {
		if id <= 0 || id != math.Trunc(id) {
			t.Errorf("id %v is not a positive integer", id)
		}
		if slices.Contains(ids[:i], id) {
			t.Errorf("id %v issued twice", id)
		}
	}
}

// After top-level code, main runs what an ES module runs under Node, in the
// order measured on Node 24.18: its microtasks, then the callbacks queued for
// after it (listen's, which Node runs through process.nextTick), then timers.
func TestRunOrdersLikeAnESModule(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	got := runProgram(t, func(d *Domain, log func(string)) {
		enqueue(func() { log("listen callback") })
		Async(d, func(co *Co) struct{} {
			Await(co, Async(d, func(*Co) float64 { return 0 }))
			log("promise microtask")
			return struct{}{}
		})
		SetTimeout(d, func(*Domain) { log("timer 0") }, 0)
		log("top level done")
	})
	if want := "top level done\npromise microtask\nlisten callback\ntimer 0\n"; got != want {
		t.Errorf("printed\n%s\nwant, as Node prints for an ES module\n%s", got, want)
	}
}

// A program whose last work is a timer exits when the timer fires (M2
// check 6), and shows what it logged before the wait while it waits.
func TestRunWaitsForTimersAndFlushesBeforeWaiting(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	stdout, _, _ := isolate(t)
	LogString("before")
	SetTimeout(Main(), func(*Domain) { LogString("after") }, 150)

	start := time.Now()
	done := runAsync(nil)
	deadline := time.Now().Add(100 * time.Millisecond)
	for stdout.String() != "before\n" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := stdout.String(); got != "before\n" {
		t.Errorf("while waiting on the timer, stdout held %q, want what was logged before the wait", got)
	}
	waitFor(t, done, "Run to return after the last timer")
	Flush()
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Errorf("Run returned after %v, before its 150 ms timer was due", elapsed)
	}
	if got := stdout.String(); got != "before\nafter\n" {
		t.Errorf("stdout = %q, want before, then after", got)
	}
}

// Ctrl-C while main waits on a timer exits 130, as M1's server does.
func TestSignalWhileWaitingOnATimer(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	stdout, _, exits := isolate(t)
	LogString("waiting")
	SetTimeout(Main(), func(*Domain) { LogString("never") }, 60_000)

	sigs := make(chan os.Signal, 1)
	done := runAsync(sigs)
	sigs <- syscall.SIGINT
	if code := exitCode(t, exits); code != 130 {
		t.Errorf("exit code = %d, want 130", code)
	}
	waitFor(t, done, "Run to stop")
	if got := stdout.String(); !strings.Contains(got, "waiting") || strings.Contains(got, "never") {
		t.Errorf("stdout = %q, want the output so far and not the timer's", got)
	}
}
