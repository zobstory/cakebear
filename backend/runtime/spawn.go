package runtime

// spawn(fn): Go's `go`, as a cakebear global (M2 plan, D7 and D10).
//
// spawn starts a new domain on its own goroutine, so fn runs truly in
// parallel with its caller, and returns at once a promise of fn's result. The
// promise belongs to the caller's domain, and it is the one promise that a
// different domain settles: the spawned domain posts the settling to the
// caller's inbox, which its driver runs as a task, the way an I/O completion
// reaches a Node event loop. Until then the spawn counts as the caller's
// pending work, so the caller cannot go idle, and a program cannot exit,
// under one.
//
// lower/ holds fn to the race rule a request handler is held to, so the two
// domains share nothing mutable, and the promise is the only thing that
// crosses between them.

// Spawn is spawn(fn) for a fn that returns a value.
func Spawn[T any](d *Domain, fn func(*Domain) T) *Promise[T] {
	return spawn(d, func(s *Domain, done func(T)) { done(fn(s)) })
}

// SpawnVoid is spawn(fn) for a fn that returns nothing.
func SpawnVoid(d *Domain, fn func(*Domain)) *Promise[struct{}] {
	return spawn(d, func(s *Domain, done func(struct{})) {
		fn(s)
		done(struct{}{})
	})
}

// SpawnAsync is spawn(fn) for an async fn: the result is what its promise
// resolves to, once the spawned domain has run until it does.
func SpawnAsync[T any](d *Domain, fn func(*Domain) *Promise[T]) *Promise[T] {
	return spawn(d, func(s *Domain, done func(T)) {
		p := fn(s)
		p.then(func() { done(p.value) })
	})
}

func spawn[T any](d *Domain, start func(s *Domain, done func(T))) *Promise[T] {
	p := &Promise[T]{d: d}
	d.spawned++
	s := newDomain()
	go func() {
		// Only this goroutine and the async calls it hands s's token to run
		// below, one at a time, so delivered needs no lock.
		delivered := false
		deliver := func(f func()) {
			if !delivered {
				delivered = true
				d.post(func() {
					d.spawned--
					f()
				})
			}
		}
		defer func() {
			r := recover()
			switch {
			case r != nil && !delivered:
				// Raised where the result was awaited: in a request it
				// drops that connection, as the request's own panics do.
				deliver(func() { panic(r) })
			case r != nil:
				warnf("cakebear: a spawned function panicked after its result was delivered: %v", r)
			case !delivered:
				deliver(func() {}) // the result can never come: release the caller
			}
		}()

		s.runTask(func() {
			start(s, func(v T) {
				if s.pending() {
					// Work s still has to do, a timer say, can reach v and
					// change it in parallel with the caller: hand the caller
					// a copy, aliasing kept (M4 plan D11). With nothing left,
					// nothing can, and v goes as it is.
					v = Clone(v)
				}
				deliver(func() { p.resolve(v) })
			})
		})
		for !delivered && s.pending() {
			s.waitNext()
		}
		// What s has left to do after its result carries on, and keeps the
		// process alive, as a request's work after its response does.
		if s.pending() {
			ref()
			defer unref()
			s.finish()
		}
	}()
	return p
}
