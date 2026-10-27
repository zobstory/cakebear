package runtime

// Rejections nothing handles (M6 plan D6, D7; m6-node R9 to R14).
//
// Node checks for them as its processTicksAndRejections does, once the
// microtasks have drained after each macrotask: the top-level code, a timer
// callback, a callback queued for main (R9, R10). A rejected promise that
// something has awaited by then is handled, however late in the drain the
// await came (R11). The first that nothing has ends the domain, panicking an
// *unhandledRejection on its driver, which no try can catch: Exit reports it
// and exits 1 for main, serve for a request (D8), and a spawned domain
// raises it in its caller (spawn.go).
//
// A top-level await's rejection is no unhandled rejection: RunModule awaits
// the module's promise, and reports its rejection as an uncaught throw
// (R14), as Node's loader does.

// unhandledRejection is the panic that ends a domain whose rejection nothing
// handled.
type unhandledRejection struct{ t *Thrown }

// String is what one reads as wherever Go prints a panic, which would be a
// cakebear bug: everything that drives a domain recovers it.
func (u *unhandledRejection) String() string { return "Unhandled rejection: " + Inspect(u.t.V) }

// checkRejections ends the domain if a promise rejected since the last check
// is still unhandled, with the first, as Node ends with the first.
func (d *Domain) checkRejections() {
	rejected := d.rejected
	d.rejected = nil
	for _, s := range rejected {
		if !s.handled {
			panic(&unhandledRejection{t: s.reason})
		}
	}
}

// moduleHops is how many microtasks Node's ES module loader takes from a
// module's top-level code rejecting to the report: microtasks queued in the
// same round run first, through the fifth (measured on Node 24.18, the same
// for a throw before the first await, a throw after one, and an await of a
// rejected promise).
const moduleHops = 5

// failAfter, a reaction of a module's promise, throws its rejection hops
// microtasks after it, counting this one, as RunModule's report is due.
func (s *settlement) failAfter(hops int) {
	if s.reason == nil {
		return
	}
	if hops > 1 {
		s.d.queueMicrotask(func() { s.failAfter(hops - 1) })
		return
	}
	panic(s.reason)
}

// failureReport is the report of r, what a domain's driver recovered while
// p ran, in D6's shape: a throw nothing caught, or a rejection nothing
// handled. It is false for anything else, a Go panic, which goes on as it
// is.
func failureReport(r any, p phase) (string, bool) {
	switch f := r.(type) {
	case *Thrown:
		return uncaughtReport(f, p), true
	case *unhandledRejection:
		return rejectionReport(f.t), true
	}
	return "", false
}

// rejectionReport is the report of a rejection nothing handled. With an
// Error it is an uncaught throw's, pointing where the error was made, as
// Node's points at the error's top frame (R12, R6); where that isn't known,
// at the throw. Node wraps any other reason in an UnhandledPromiseRejection,
// printed as an Error is (R13), whose excerpt is from Node's own code
// (node:internal/process/promises). This one points at the program's throw
// that rejected the promise, as the report of a non-Error thrown at an ES
// module's top level does, and has none for reject(v), which has no site.
func rejectionReport(t *Thrown) string {
	if e, ok := t.V.(*Error); ok {
		site := known(e.site)
		if site == nil {
			site = known(t.Site)
		}
		return errorReport(e, site)
	}
	return errorReport(unhandledPromiseRejection(t.V), known(t.Site))
}

// unhandledPromiseRejection is the error Node makes of a reason that isn't
// one (lib/internal/process/promises.js), with its code and as it prints
// under --stack-trace-limit=0: `[UnhandledPromiseRejection: … reason "m".]`
// and a block with the code.
func unhandledPromiseRejection(reason any) *Error {
	m := "This error originated either by throwing inside of an async function without a catch block, " +
		"or by rejecting a promise which was not handled with .catch(). The promise rejected with the reason \"" +
		noSideEffectsToString(reason) + "\"."
	e := newCodedError(ErrorBase, "ERR_UNHANDLED_REJECTION", m, nil)
	e.stack = "UnhandledPromiseRejection: " + m
	return e
}

// noSideEffectsToString is V8's Object::NoSideEffectsToString, which names
// the reason in that message, of what an unknown holds: a primitive as
// ToString gives it, an array as "[object Array]" and an object as
// "#<Object>" (measured on Node 24.18). An Error never reaches it.
func noSideEffectsToString(v any) string {
	switch v.(type) {
	case nil, jsNull, bool, float64, string:
		return UnknownToString(v)
	case array:
		return "[object Array]"
	}
	return "#<Object>"
}
