package runtime

import (
	"container/heap"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Domains: where JavaScript's single thread survives (CLAUDE.md, semantics
// decision 3).
//
// A domain is one place JavaScript code runs one piece at a time. The main
// program is one, and each request handler invocation is another. Within a
// domain, code runs in exactly the order Node's queues would give it:
// synchronous code first, then microtasks (promise reactions) in the order
// they were queued, then timers by due time and then scheduling order, with
// the microtasks drained after each timer. Across domains code runs in
// parallel. That is safe because lower/'s race rule leaves domains no mutable
// state to share.
//
// At any moment exactly one goroutine holds a domain's token, and only that
// goroutine touches the domain. It is the domain's driver (the main
// goroutine, or the net/http goroutine serving a request) unless the driver
// has handed the token to an async call's goroutine (Async, Await). The
// handoffs are channel operations, which order every access, so a domain
// needs no lock.
//
// Generated code passes the domain as the first argument of every function it
// compiles. Go has no goroutine-local storage, and the domain a function runs
// in is its caller's: a top-level function runs in main when main calls it and
// in a request when a handler does.

// Domain is one of the places JavaScript code runs one piece at a time.
type Domain struct {
	micro     []func() // queued microtasks; micro[microHead:] are still to run
	microHead int
	timers    timerHeap
	byID      map[float64]*timer
	seq       uint64 // breaks ties between timers due at the same moment
	// spawned counts this domain's spawns whose results have not come back
	// (spawn.go). Each is pending work: the domain cannot go idle under one.
	spawned int
	// running is what the driver is running, which the report of an
	// uncaught error reads (uncaught.go). Each kind of task sets it as it
	// starts, so a throw finds it as it was when the throw began.
	running phase
	// tick is a failure Node would raise on the next tick, which comes
	// before the next microtask (writeAfterEnd).
	tick *Thrown

	// The inbox is the one part of a domain other goroutines touch: a spawned
	// domain posts its result here. It has its own lock, and wake tells the
	// driver, which may be waiting on nothing but the inbox.
	inboxMu sync.Mutex
	inbox   []func()
	wake    chan struct{}
}

func newDomain() *Domain {
	return &Domain{byID: map[float64]*timer{}, wake: make(chan struct{}, 1)}
}

// post queues f to run in d as a task, from any goroutine, and wakes d's
// driver. One slot in wake is enough: the driver takes the whole inbox each
// time it wakes.
func (d *Domain) post(f func()) {
	d.inboxMu.Lock()
	d.inbox = append(d.inbox, f)
	d.inboxMu.Unlock()
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// runInbox runs what other goroutines have posted, in the order they posted
// it, each as a task.
func (d *Domain) runInbox() {
	d.inboxMu.Lock()
	in := d.inbox
	d.inbox = nil
	d.inboxMu.Unlock()
	for _, f := range in {
		d.running = inCallback
		d.runTask(f)
	}
}

// mainDomain is the main program's. Tests replace it, as they do the queue.
var mainDomain = newDomain()

// Main is the main program's domain, which the generated main passes to the
// program's top-level code.
func Main() *Domain { return mainDomain }

func (d *Domain) queueMicrotask(f func()) { d.micro = append(d.micro, f) }

// drain runs microtasks until there are none, including the ones queued while
// it runs, as Node does after each macrotask. A failure due on the next tick
// is raised first, and before each microtask, as Node runs process.nextTick
// callbacks before promise reactions.
func (d *Domain) drain() {
	for {
		if t := d.tick; t != nil {
			d.tick = nil
			panic(t)
		}
		if d.microHead == len(d.micro) {
			break
		}
		f := d.micro[d.microHead]
		d.micro[d.microHead] = nil
		d.microHead++
		d.running = inMicrotask
		f()
	}
	d.micro, d.microHead = d.micro[:0], 0
}

// runTask runs f as one macrotask: f itself, then every microtask it leads to.
func (d *Domain) runTask(f func()) {
	f()
	d.drain()
}

// pending reports whether the domain still has work that will run by itself:
// a timer, or a spawn whose result is still to come. Microtasks are always
// drained before anything asks. A promise that nothing will ever settle is
// not pending work, so a program blocked only on one ends, as under Node.
func (d *Domain) pending() bool { return len(d.timers) > 0 || d.spawned > 0 }

// timer is one setTimeout.
type timer struct {
	when  time.Time
	seq   uint64
	id    float64
	cb    func(*Domain)
	index int // in the heap, for ClearTimeout
}

// nextTimerID numbers timers across the whole process, so an id one domain
// got can never clear another domain's timer.
var nextTimerID atomic.Int64

// SetTimeout is the DOM's setTimeout(handler, timeout) as lower/ restricts it:
// a function and a delay, no string handler and no extra arguments. It returns
// the id ClearTimeout takes, a number as the DOM's declaration says.
func SetTimeout(d *Domain, cb func(*Domain), ms float64) float64 {
	id := float64(nextTimerID.Add(1))
	d.seq++
	t := &timer{when: time.Now().Add(timerDelay(ms)), seq: d.seq, id: id, cb: cb}
	heap.Push(&d.timers, t)
	d.byID[id] = t
	return id
}

// ClearTimeout is clearTimeout(id). An id that has fired, been cleared, or
// belongs to another domain does nothing, as in JavaScript.
func ClearTimeout(d *Domain, id float64) {
	t, ok := d.byID[id]
	if !ok {
		return
	}
	delete(d.byID, id)
	heap.Remove(&d.timers, t.index)
}

// timerDelay is Node's coercion of a delay: anything below 1 ms, above
// 2^31-1 ms, or NaN, becomes 1 ms. A fraction is kept.
func timerDelay(ms float64) time.Duration {
	if !(ms >= 1 && ms <= math.MaxInt32) {
		ms = 1
	}
	return time.Duration(ms * float64(time.Millisecond))
}

// nextDue is when the earliest timer is due.
func (d *Domain) nextDue() (time.Time, bool) {
	if len(d.timers) == 0 {
		return time.Time{}, false
	}
	return d.timers[0].when, true
}

// fireDue runs every timer that is due, earliest first, draining microtasks
// after each, as Node has since version 11.
func (d *Domain) fireDue() {
	for len(d.timers) > 0 && !d.timers[0].when.After(time.Now()) {
		t := heap.Pop(&d.timers).(*timer)
		delete(d.byID, t.id)
		d.running = inTimer
		d.runTask(func() { t.cb(d) })
	}
}

// waitNext blocks until the earliest timer is due or another goroutine posts
// to the inbox, then runs what was posted and fires every timer that is due.
// Nothing else can add work while the driver waits: only the domain's own
// code adds timers, and a spawn's result comes through the inbox.
func (d *Domain) waitNext() {
	var due <-chan time.Time
	if when, ok := d.nextDue(); ok {
		clock := time.NewTimer(time.Until(when))
		defer clock.Stop()
		due = clock.C
	}
	select {
	case <-due:
	case <-d.wake:
	}
	d.runInbox()
	d.fireDue()
}

// finish drives the domain until it has nothing left to do.
func (d *Domain) finish() {
	for d.pending() {
		d.waitNext()
	}
}

// timerHeap orders timers by due time, then by when they were scheduled.
type timerHeap []*timer

func (h timerHeap) Len() int { return len(h) }

func (h timerHeap) Less(i, j int) bool {
	if !h[i].when.Equal(h[j].when) {
		return h[i].when.Before(h[j].when)
	}
	return h[i].seq < h[j].seq
}

func (h timerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}

func (h *timerHeap) Push(x any) {
	t := x.(*timer)
	t.index = len(*h)
	*h = append(*h, t)
}

func (h *timerHeap) Pop() any {
	old := *h
	t := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	t.index = -1
	return t
}
