package runtime

// Promise's combinators and statics (S1 plan D5), with V8's microtask hops,
// measured on Node 24.18 (s1-node R21): a combinator settles in the
// reaction to the input that decides it, one hop after it, and over an
// empty array all resolves at once. Adoption is promise_adopt.go's.

// PromiseResolve is `Promise.resolve(v)` of a value: a promise fulfilled
// with it. Of a promise, lowering passes the promise itself, as V8 returns
// it (R21).
func PromiseResolve[T any](d *Domain, v T) *Promise[T] {
	p := &Promise[T]{settlement: settlement{d: d}}
	p.resolve(v)
	return p
}

// PromiseReject is `Promise.reject(reason)`, reason as an unknown holds it.
// It has no site, as reject(v) has none: an Error's report points where it
// was made, and any other reason's at nothing of the program's (R13).
func PromiseReject[T any](d *Domain, reason any) *Promise[T] {
	p := &Promise[T]{settlement: settlement{d: d}}
	p.reject(&Thrown{V: reason})
	return p
}

// PromiseAll is `Promise.all(ps)`: the values in ps's order, whatever order
// they settle in, or the first rejection in time; a later rejection is
// handled by it (R32).
func PromiseAll[T any](d *Domain, ps *Array[*Promise[T]]) *Promise[*Array[T]] {
	out := &Promise[*Array[T]]{settlement: settlement{d: d}}
	n := len(ps.items)
	if n == 0 {
		out.resolve(NewArray[T]()) // at once (R21)
		return out
	}
	vals := make([]T, n)
	left := n
	for i, p := range ps.items {
		p.checkDomain(d)
		p.then(func() {
			if p.reason != nil {
				out.reject(p.reason)
				return
			}
			vals[i] = p.value
			if left--; left == 0 {
				out.resolve(NewArray(vals...))
			}
		})
	}
	return out
}

// PromiseAllValues is `Promise.all(xs)` of an array of values, each a
// promise fulfilled with it, as Promise.resolve makes it.
func PromiseAllValues[T any](d *Domain, xs *Array[T]) *Promise[*Array[T]] {
	ps := make([]*Promise[T], len(xs.items))
	for i, v := range xs.items {
		ps[i] = PromiseResolve(d, v)
	}
	return PromiseAll(d, NewArray(ps...))
}

// PromiseRace is `Promise.race(ps)`: the first to settle, either way; a
// loser rejecting later is handled (R35). Over none it never settles.
func PromiseRace[T any](d *Domain, ps *Array[*Promise[T]]) *Promise[T] {
	out := &Promise[T]{settlement: settlement{d: d}}
	for _, p := range ps.items {
		p.checkDomain(d)
		p.then(func() {
			if p.reason != nil {
				out.reject(p.reason)
				return
			}
			out.resolve(p.value)
		})
	}
	return out
}

// Pending is a promise of any type, for PromiseAllOf.
type Pending interface{ settlementOf() *settlement }

func (p *Promise[T]) settlementOf() *settlement { return &p.settlement }

// PromiseAllOf is `Promise.all([pa, pb, …])` of promises of different
// types, `const [a, b] = await Promise.all([…])`: it settles as Promise.all
// would, and each value is read from its promise after (Value).
func PromiseAllOf(d *Domain, ps ...Pending) *Promise[struct{}] {
	out := &Promise[struct{}]{settlement: settlement{d: d}}
	if len(ps) == 0 {
		out.resolve(struct{}{})
		return out
	}
	left := len(ps)
	for _, p := range ps {
		s := p.settlementOf()
		s.checkDomain(d)
		s.then(func() {
			if s.reason != nil {
				out.reject(s.reason)
				return
			}
			if left--; left == 0 {
				out.resolve(struct{}{})
			}
		})
	}
	return out
}

// Value is a fulfilled promise's value, read once PromiseAllOf has settled.
func (p *Promise[T]) Value() T {
	if !p.settled || p.reason != nil {
		panic("cakebear: a promise's value read before it was fulfilled")
	}
	return p.value
}
