package runtime

import "runtime"

// Promises and async functions, on goroutines (CLAUDE.md, semantics decision
// 3, as amended for M2).
//
// Every async call gets its own goroutine, so `await` can simply block it,
// with no rewriting of the function into states. What keeps that equivalent
// to JavaScript is the domain's token: the caller hands it to the new
// goroutine and waits, the goroutine hands it back when it awaits or returns,
// and an Await resumes only when a microtask hands the token to it again. So
// within a domain one piece of code runs at a time, in Node's order. The
// emitted code looks like this:
//
//	func f(d *rt.Domain, n float64) *rt.Promise[float64] {
//		return rt.Async(d, func(co *rt.Co) float64 {
//			v := rt.Await(co, g(d, n))
//			return v + 1
//		})
//	}
//
// Rejection (M6 plan D7): a throw out of an async body, or out of an
// executor, rejects the promise, and so does an executor's reject; an Await
// of a rejected promise throws the same *Thrown again in the awaiter, so a
// catch there binds the very value thrown (R18). A rejection takes the same
// microtask hops as a resolution (R17), since both queue the promise's
// reactions as they settle. One that nothing awaits by the time the
// microtasks have drained ends the domain (rejection.go).

// Promise is TypeScript's Promise<T>. Promise<void> is Promise[struct{}].
//
// A promise belongs to the domain that created it. lower/ keeps one from
// reaching another domain, as it does any mutable state, and the runtime
// panics if one does rather than race.
type Promise[T any] struct {
	settlement
	value T
}

// settlement is the part of a promise its type has no part in: whether it
// has settled and how, and who is waiting.
type settlement struct {
	d       *Domain
	settled bool
	// reason is what the promise was rejected with: nil while it is pending
	// and once it is fulfilled.
	reason *Thrown
	// handled is set once anything awaits the promise. A rejection still
	// unhandled when the microtasks have drained ends the domain (R9).
	handled   bool
	reactions []func()
}

// resolve fulfils p with v and queues its reactions. Once p has settled it
// does nothing, as in JavaScript (R16).
func (p *Promise[T]) resolve(v T) {
	if p.settled {
		return
	}
	p.value = v
	p.settle(nil)
}

// reject settles p with a throw. Once p has settled it does nothing (R16).
// A rejection nothing awaits yet is noted for the check after the drain,
// which looks again then: an await before it is in time (R11).
func (s *settlement) reject(t *Thrown) {
	if s.settled {
		return
	}
	if !s.handled {
		s.d.rejected = append(s.d.rejected, s)
	}
	s.settle(t)
}

func (s *settlement) settle(reason *Thrown) {
	s.settled, s.reason = true, reason
	for _, r := range s.reactions {
		s.d.queueMicrotask(r)
	}
	s.reactions = nil
}

// then queues r as a microtask once the promise has settled: straight away
// if it already has, because JavaScript never runs a reaction synchronously.
// A reaction is what handles a rejection.
func (s *settlement) then(r func()) {
	s.handled = true
	if s.settled {
		s.d.queueMicrotask(r)
		return
	}
	s.reactions = append(s.reactions, r)
}

// errCrossDomain is the panic for a promise used outside its domain.
const errCrossDomain = "cakebear: a promise was used outside the request or program that created it"

func (s *settlement) checkDomain(d *Domain) {
	if d != s.d {
		panic(errCrossDomain)
	}
}

// NewPromise is `new Promise<T>((resolve, reject) => …)`. The executor runs
// at once, before NewPromise returns, as in JavaScript. resolve and reject
// are ordinary function values, so each takes the domain of whoever calls
// it. reject takes the reason as an unknown holds it (unknown.go). A throw
// out of the executor rejects the promise, unless it has settled (R16).
func NewPromise[T any](d *Domain, executor func(*Domain, func(*Domain, T), func(*Domain, any))) *Promise[T] {
	p := &Promise[T]{settlement: settlement{d: d}}
	p.execute(func() {
		executor(d, func(caller *Domain, v T) {
			p.checkDomain(caller)
			p.resolve(v)
		}, p.rejectFn)
	})
	return p
}

// NewPromiseVoid is NewPromise for Promise<void>, whose resolve takes no
// value (ir.ResolveType). That is what lets `setTimeout(resolve, ms)` pass
// resolve as a timer callback.
func NewPromiseVoid(d *Domain, executor func(*Domain, func(*Domain), func(*Domain, any))) *Promise[struct{}] {
	p := &Promise[struct{}]{settlement: settlement{d: d}}
	p.execute(func() {
		executor(d, func(caller *Domain) {
			p.checkDomain(caller)
			p.resolve(struct{}{})
		}, p.rejectFn)
	})
	return p
}

// execute runs an executor, and rejects with what it threw.
func (s *settlement) execute(executor func()) {
	if t := Try(executor); t != nil {
		s.reject(t)
	}
}

// rejectFn is an executor's reject. Its throw has no site: the emitter
// passes reject none, so the report of a reason that isn't an Error, which
// would point at the throw, has no excerpt (rejection.go). An Error's points
// where it was made.
func (s *settlement) rejectFn(caller *Domain, reason any) {
	s.checkDomain(caller)
	s.reject(&Thrown{V: reason})
}

// Co is one async call in progress: the goroutine Async started for it, and
// the two channels its domain's token passes through.
type Co struct {
	d      *Domain
	resume chan struct{} // the token, handed to the call
	yield  chan struct{} // the token, handed back by the call

	// A panic in the body that isn't a throw is carried to whichever
	// goroutine takes the token back. Raised on the call's own goroutine it
	// would end the process; on the token holder it ends what it would have
	// ended had the code run there: for a request, only that connection,
	// which net/http recovers. A throw rejects the call's promise instead.
	failed   bool
	panicked any

	// parkedAt is the call's place in its domain's parked registry while it
	// waits in an Await, and aborted is set when the domain was abandoned
	// with the call still waiting (parked.go).
	parkedAt int
	aborted  bool
}

// Async calls an async function's body on its own goroutine and returns the
// promise of its result. The body runs synchronously up to its first Await
// before Async returns, as in JavaScript, because the caller waits for the
// token to come back. A throw before then rejects the promise, and the
// caller carries on (R15).
func Async[T any](d *Domain, body func(*Co) T) *Promise[T] {
	p := &Promise[T]{settlement: settlement{d: d}}
	co := &Co{d: d, resume: make(chan struct{}), yield: make(chan struct{})}
	go func() {
		defer func() {
			// An aborted call is exiting, not panicking: recover gives nil.
			if r := recover(); r != nil {
				if t, ok := r.(*Thrown); ok {
					p.reject(t) // this goroutine holds the token
				} else {
					co.failed, co.panicked = true, r
				}
			}
			co.yield <- struct{}{}
		}()
		p.resolve(body(co))
	}()
	co.wait()
	return p
}

// wait blocks until the call hands the token back, by awaiting or returning,
// and re-raises a panic from its body here.
func (co *Co) wait() {
	<-co.yield
	if co.failed {
		panic(co.panicked)
	}
}

// Await is `await p` inside the async call co. It always gives the token
// back, even for a settled promise, and resumes in the microtask p's
// settling queues, which is what orders it after the code that was already
// due, exactly as in Node. A rejection is thrown again here, the same
// *Thrown, so what a catch gets is what was thrown or rejected (R18).
//
// While it waits, the call is in its domain's parked registry. A domain
// abandoned with the call still there hands it the token once more, and it
// exits its goroutine (parked.go), running no more of the program: in Node
// it would never have resumed.
func Await[T any](co *Co, p *Promise[T]) T {
	p.checkDomain(co.d)
	p.then(func() {
		co.d.unpark(co)
		co.resume <- struct{}{}
		co.wait()
	})
	co.d.park(co)
	co.yield <- struct{}{}
	<-co.resume
	if co.aborted {
		runtime.Goexit() // runs the runtime's deferred calls, then Async's, which hands the token back
	}
	if p.reason != nil {
		panic(p.reason)
	}
	return p.value
}
