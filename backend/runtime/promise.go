package runtime

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
// Rejection is not modelled: M2 has no throw, and lower/ refuses reject.

// Promise is TypeScript's Promise<T>. Promise<void> is Promise[struct{}].
//
// A promise belongs to the domain that created it. lower/ keeps one from
// reaching another domain, as it does any mutable state, and the runtime
// panics if one does rather than race.
type Promise[T any] struct {
	d         *Domain
	settled   bool
	value     T
	reactions []func()
}

// resolve settles p with v and queues its reactions. A second call does
// nothing, as in JavaScript.
func (p *Promise[T]) resolve(v T) {
	if p.settled {
		return
	}
	p.settled, p.value = true, v
	for _, r := range p.reactions {
		p.d.queueMicrotask(r)
	}
	p.reactions = nil
}

// then queues r as a microtask once p has settled: straight away if it
// already has, because JavaScript never runs a reaction synchronously.
func (p *Promise[T]) then(r func()) {
	if p.settled {
		p.d.queueMicrotask(r)
		return
	}
	p.reactions = append(p.reactions, r)
}

// errCrossDomain is the panic for a promise used outside its domain.
const errCrossDomain = "cakebear: a promise was used outside the request or program that created it"

func (p *Promise[T]) checkDomain(d *Domain) {
	if d != p.d {
		panic(errCrossDomain)
	}
}

// NewPromise is `new Promise<T>((resolve, reject) => …)`. The executor runs
// at once, before NewPromise returns, as in JavaScript. resolve and reject
// are ordinary function values, so each takes the domain of whoever calls
// it. reject takes the reason as an unknown holds it (unknown.go).
func NewPromise[T any](d *Domain, executor func(*Domain, func(*Domain, T), func(*Domain, any))) *Promise[T] {
	p := &Promise[T]{d: d}
	executor(d, func(caller *Domain, v T) {
		p.checkDomain(caller)
		p.resolve(v)
	}, rejectNotYet)
	return p
}

// NewPromiseVoid is NewPromise for Promise<void>, whose resolve takes no
// value (ir.ResolveType). That is what lets `setTimeout(resolve, ms)` pass
// resolve as a timer callback.
func NewPromiseVoid(d *Domain, executor func(*Domain, func(*Domain), func(*Domain, any))) *Promise[struct{}] {
	p := &Promise[struct{}]{d: d}
	executor(d, func(caller *Domain) {
		p.checkDomain(caller)
		p.resolve(struct{}{})
	}, rejectNotYet)
	return p
}

// rejectNotYet stands in for reject until rejection lands (M6 step 4):
// lowering refuses any use of it, so nothing calls it.
func rejectNotYet(*Domain, any) {
	panic("cakebear: reject is not implemented yet")
}

// Co is one async call in progress: the goroutine Async started for it, and
// the two channels its domain's token passes through.
type Co struct {
	d      *Domain
	resume chan struct{} // the token, handed to the call
	yield  chan struct{} // the token, handed back by the call

	// A panic in the body is carried to whichever goroutine takes the token
	// back. Raised on the call's own goroutine it would end the process; on
	// the token holder it ends what it would have ended had the code run
	// there: for a request, only that connection, which net/http recovers.
	failed   bool
	panicked any
}

// Async calls an async function's body on its own goroutine and returns the
// promise of its result. The body runs synchronously up to its first Await
// before Async returns, as in JavaScript, because the caller waits for the
// token to come back.
func Async[T any](d *Domain, body func(*Co) T) *Promise[T] {
	p := &Promise[T]{d: d}
	co := &Co{d: d, resume: make(chan struct{}), yield: make(chan struct{})}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				co.failed, co.panicked = true, r
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
// due, exactly as in Node.
func Await[T any](co *Co, p *Promise[T]) T {
	p.checkDomain(co.d)
	p.then(func() {
		co.resume <- struct{}{}
		co.wait()
	})
	co.yield <- struct{}{}
	<-co.resume
	return p.value
}
