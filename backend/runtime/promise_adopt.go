package runtime

// Adoption (S1 plan D5): a promise taking another's state, through an async
// function's `return p` or an executor's `resolve(p)`. V8 calls p.then from
// a job it queues there (NewPromiseResolveThenableJob), so the promise
// settles a hop after a reaction to p alone would: two hops after a settled
// p, where `return await p` takes one (measured on Node 24.18).

// AwaitAdopt is an async function's `return p`: its promise adopts p's
// state. V8 calls p.then from a job it queues on the return
// (NewPromiseResolveThenableJob), so the function waits a hop before it
// waits for p: with p settled, its promise settles a hop later than after
// `return await p`, and with p pending, as soon after p as an await would.
func AwaitAdopt[T any](co *Co, p *Promise[T]) T {
	job := &Promise[struct{}]{settlement: settlement{d: co.d}}
	job.resolve(struct{}{})
	Await(co, job)
	return Await(co, p)
}

// adoptFn is an adopting executor's resolve(q) of a promise q. The promise
// is resolved from then on, though still pending: a later resolve, reject
// or throw from its executor does nothing (R16). Resolved with itself, it
// rejects with V8's TypeError.
func (p *Promise[T]) adoptFn(caller *Domain, q *Promise[T]) {
	p.checkDomain(caller)
	if p.settled || p.locked {
		return
	}
	if q == p {
		p.reject(&Thrown{V: NewError(ErrorType, "Chaining cycle detected for promise #<Promise>", nil)})
		return
	}
	q.checkDomain(caller)
	p.locked = true
	p.d.queueMicrotask(func() {
		q.then(func() {
			p.locked = false
			if q.reason != nil {
				p.reject(q.reason)
				return
			}
			p.resolve(q.value)
		})
	})
}

// NewPromiseAdopting is NewPromise for an executor that calls resolve with a
// promise: lowering gives it adopt beside resolve and reject, and makes each
// such call one of adopt.
func NewPromiseAdopting[T any](d *Domain, executor func(*Domain, func(*Domain, T), func(*Domain, any), func(*Domain, *Promise[T]))) *Promise[T] {
	p := &Promise[T]{settlement: settlement{d: d}}
	p.execute(func() {
		executor(d, func(caller *Domain, v T) {
			p.checkDomain(caller)
			p.resolve(v)
		}, p.rejectFn, p.adoptFn)
	})
	return p
}

// NewPromiseVoidAdopting is NewPromiseAdopting for Promise<void>.
func NewPromiseVoidAdopting(d *Domain, executor func(*Domain, func(*Domain), func(*Domain, any), func(*Domain, *Promise[struct{}]))) *Promise[struct{}] {
	p := &Promise[struct{}]{settlement: settlement{d: d}}
	p.execute(func() {
		executor(d, func(caller *Domain) {
			p.checkDomain(caller)
			p.resolve(struct{}{})
		}, p.rejectFn, p.adoptFn)
	})
	return p
}
