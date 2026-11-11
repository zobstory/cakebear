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
	// rejected holds the promises rejected with nothing awaiting them since
	// the last drain, which ends by checking them again (rejection.go).
	rejected []*settlement
	// parked holds the async calls waiting in an Await (parked.go).
	parked []*Co

	// inflight counts the I/O this domain started that hasn't completed (a
	// fetch, fetch.go). Each is pending work, as a spawn is: a domain with a
	// fetch in flight waits for it (R17).
	inflight int
	// afterDrain holds what runs when the microtask queue is empty, before
	// any macrotask: a body read whose bytes are in, which Node settles at
	// that point, after any chain of microtasks and before a timer (R19).
	afterDrain []func()

	// The inbox is the one part of a domain other goroutines touch: a spawned
	// domain posts its result here. It has its own lock, and wake tells the
	// driver, which may be waiting on nothing but the inbox.
	inboxMu sync.Mutex
	inbox   []func()
	wake    chan struct{}
	// io is the inbox's other half, for I/O completions: what a fetch's
	// goroutine posts. They run after the timers already due, as Node's
	// poll phase follows its timers phase (R18, S1 plan D11), where a
	// spawn's result keeps its place before them: under the shim that runs
	// a program with spawn under Node, a spawn's result is a microtask.
	io []func()
	// unrefd counts the timers in timers that keep nothing alive (timer's
	// unref), which pending leaves out.
	unrefd int
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

// postIO queues f, an I/O completion, to run in d as a task, from any
// goroutine, and wakes d's driver.
func (d *Domain) postIO(f func()) {
	d.inboxMu.Lock()
	d.io = append(d.io, f)
	d.inboxMu.Unlock()
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// runIO runs the I/O completions posted so far, in the order they were
// posted, each as a task. The driver calls it after firing the timers that
// are due.
func (d *Domain) runIO() {
	d.inboxMu.Lock()
	in := d.io
	d.io = nil
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
// callbacks before promise reactions. Last, as Node's
// processTicksAndRejections does, it ends the domain if a rejection is
// still unhandled (rejection.go).
func (d *Domain) drain() {
	for {
		if t := d.tick; t != nil {
			d.tick = nil
			panic(t)
		}
		if d.microHead == len(d.micro) {
			if len(d.afterDrain) == 0 {
				break
			}
			// The queue is empty: what waited for that runs, and its own
			// microtasks drain before the next of them (R19).
			f := d.afterDrain[0]
			d.afterDrain = d.afterDrain[1:]
			d.running = inCallback
			f()
			continue
		}
		f := d.micro[d.microHead]
		d.micro[d.microHead] = nil
		d.microHead++
		d.running = inMicrotask
		f()
	}
	d.micro, d.microHead = d.micro[:0], 0
	if len(d.rejected) > 0 {
		d.checkRejections()
	}
}

// runTask runs f as one macrotask: f itself, then every microtask it leads to.
func (d *Domain) runTask(f func()) {
	f()
	d.drain()
}

// pending reports whether the domain still has work that will run by itself:
// a timer, a spawn whose result is still to come, or I/O in flight. Microtasks are always
// drained before anything asks. A promise that nothing will ever settle is
// not pending work, so a program blocked only on one ends, as under Node.
func (d *Domain) pending() bool {
	return len(d.timers) > d.unrefd || d.spawned > 0 || d.inflight > 0
}

// timer is one setTimeout.
type timer struct {
	when  time.Time
	seq   uint64
	id    float64
	cb    func(*Domain)
	index int // in the heap, for ClearTimeout
	// unref marks a timer that keeps nothing alive, as Node's unref'd
	// timers don't: AbortSignal.timeout's (abort.go). It fires while the
	// domain has other work, and is dropped with the domain otherwise.
	unref bool
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

// Timeout is what @types/node's setTimeout returns where the DOM's returns a
// number (S1 plan D12): a timer that only clearTimeout takes.
type Timeout struct{ id float64 }

// SetNodeTimeout is setTimeout as @types/node declares it.
func SetNodeTimeout(d *Domain, cb func(*Domain), ms float64) *Timeout {
	return &Timeout{id: SetTimeout(d, cb, ms)}
}

// ClearNodeTimeout is clearTimeout of a Timeout.
func ClearNodeTimeout(d *Domain, t *Timeout) { ClearTimeout(d, t.id) }

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
		if t.unref {
			d.unrefd--
		}
		d.running = inTimer
		d.runTask(func() { t.cb(d) })
	}
}

// waitNext blocks until the earliest timer is due or another goroutine posts
// to the inbox, then runs what was posted, fires every timer that is due,
// and runs the I/O completions posted (R18: timers already due come first).
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
	d.runIO()
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
