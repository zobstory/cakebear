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
//
// A throw out of fn, or a rejection of an async fn's promise, rejects the
// spawn's promise (M6 plan D7), so the caller's await throws it again, and a
// try around it catches it. A failure nothing in the spawned domain catches,
// a throw in one of its timers say, or a rejection nothing there handles, is
// what it is under Node, where fn runs in the caller's thread: uncaught.
// Before the result it is raised in the caller's domain, where the result
// would have arrived and nothing can catch it, so it ends main as main's own
// would, and a request as the request's own would (D8). After the result it
// is reported as an uncaught one is, and ends the program if main spawned
// it, or else only itself, as in a request's work after its response.

// Spawn is spawn(fn) for a fn that returns a value.
func Spawn[T any](d *Domain, fn func(*Domain) T) *Promise[T] {
	return spawn(d, func(s *Domain, done func(T), fail func(*Thrown)) {
		var v T
		if t := Try(func() { v = fn(s) }); t != nil {
			fail(t)
			return
		}
		done(v)
	})
}

// SpawnVoid is spawn(fn) for a fn that returns nothing.
func SpawnVoid(d *Domain, fn func(*Domain)) *Promise[struct{}] {
	return Spawn(d, func(s *Domain) struct{} {
		fn(s)
		return struct{}{}
	})
}

// SpawnAsync is spawn(fn) for an async fn, or any fn returning a promise:
// the result is what that promise settles to, once the spawned domain has
// run until it does.
func SpawnAsync[T any](d *Domain, fn func(*Domain) *Promise[T]) *Promise[T] {
	return spawn(d, func(s *Domain, done func(T), fail func(*Thrown)) {
		var p *Promise[T]
		if t := Try(func() { p = fn(s) }); t != nil {
			fail(t)
			return
		}
		p.then(func() {
			if p.reason != nil {
				fail(p.reason)
				return
			}
			done(p.value)
		})
	})
}

func spawn[T any](d *Domain, start func(s *Domain, done func(T), fail func(*Thrown))) *Promise[T] {
	p := &Promise[T]{settlement: settlement{d: d}}
	d.spawned++
	fromMain := d == mainDomain
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
		defer s.abort() // nothing drives s after this: end the calls still parked in it
		defer func() {
			r := recover()
			switch {
			case r != nil && delivered:
				failedAfterResult(r, s, fromMain)
			case r != nil:
				// Raised where the result would have arrived, in a task of
				// the caller's, outside any try. s has failed and nothing
				// runs in it again, so what was thrown goes as it is.
				deliver(func() { panic(r) })
			case !delivered:
				deliver(func() {}) // the result can never come: release the caller
			}
		}()

		s.runTask(func() {
			start(s, func(v T) {
				if s.busy() {
					// Work s still has to do, a timer say, can reach v and
					// change it in parallel with the caller: hand the caller
					// a copy, aliasing kept (M4 plan D11). With nothing left,
					// nothing can, and v goes as it is.
					v = Clone(v)
				}
				deliver(func() { p.resolve(v) })
			}, func(t *Thrown) {
				if s.busy() {
					t = &Thrown{V: Clone(t.V), Site: t.Site} // as for a result (M6 plan D10)
				}
				deliver(func() { p.reject(t) })
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
			defer func() {
				// Before unref: while the work runs main can't end, so a
				// failure ends the program here rather than after a normal
				// exit.
				if r := recover(); r != nil {
					failedAfterResult(r, s, fromMain)
				}
			}()
			s.finish()
		}
	}()
	return p
}

// failedAfterResult is r, a failure in s after its result went to its
// caller. A throw or a rejection nothing handled is reported as an uncaught
// one is, and ends the program when main spawned s; a Go panic is
// reported.
func failedAfterResult(r any, s *Domain, fromMain bool) {
	report, ok := failureReport(r, s.running)
	switch {
	case !ok:
		warnf("cakebear: a spawned function panicked after its result was delivered: %v", r)
	case fromMain:
		Flush()
		writeErr(report)
		exit(1)
	default:
		writeErr(report)
	}
}
