package runtime

// Exceptions (M6 plan D2, D3): `throw v` panics with a *Thrown, and a try
// block is a function literal that Try runs, whose deferred Catch takes the
// *Thrown back. Any other panic, a Go runtime error or a cakebear invariant,
// goes on up: no catch can see it, and no finally runs for it (D3). Catch
// raises it again inside the deferred call, on top of the panicking frames,
// so a crash still prints where the panic began.
//
// The emitted code looks like this (m6-go's spike, §3):
//
//	// try { … } catch (e) { … }: nothing leaves the try early
//	if ts_t1 := cakebearrt.Try(func() {
//		…
//		panic(&cakebearrt.Thrown{V: v, Site: &ts_sites[3]}) // throw v
//	}); ts_t1 != nil {
//		e := ts_t1.V
//		…
//	}
//
//	// try { … return x; } finally { … }: the try's ways out are codes
//	ts_k1, ts_t1 := cakebearrt.TryK(func() int {
//		ts_r = x
//		return 1
//	})
//	… // the finally, inline: its own return, break or throw discards both
//	cakebearrt.Rethrow(ts_t1)
//	if ts_k1 == 1 {
//		return ts_r
//	}
//
// The runtime throws the same way where Node throws (D9): a *Thrown holding
// an *Error with Node's name, message and code (throwError).
//
// A throw from program code the runtime calls, a callback or a comparator,
// unwinds through the runtime's own frames. So the runtime never calls
// program code while it holds a lock or is partway through changing a value
// (D10): outMu, errMu, layoutsMu and an inbox's lock are only ever held
// around runtime code, and Sort sorts a copy, so a comparator that throws
// leaves the array as it was, as in Node (R23,
// TestThrowsThroughCallbacksMatchNode).

// Site is a place in the program's source, for the report of an uncaught
// error (D6, R6): a throw, a `new Error`, or an operation the runtime throws
// for. The emitter generates one for each such place, at package level, and
// passes a pointer to it, so a site costs nothing at run time. A nil *Site
// is a place nobody knows, and the report then has no source excerpt.
type Site struct {
	// File is the source file's absolute path, as it was at build time.
	// The report writes it as a file: URL, as Node writes a module's.
	File string
	// Line is the 1-based line, and Col the 1-based column in UTF-16 code
	// units, as V8 counts it, of the character the caret goes under.
	Line, Col int
	// Text is the source line, as Node prints it: with its types blanked,
	// as Node's type stripping blanks them (R5), and no line terminator.
	Text string
}

// Thrown is a JavaScript value in flight: what `throw v` panics with.
type Thrown struct {
	// V is the value thrown, as an unknown holds it (unknown.go): a catch
	// binds the very value, so `===` holds between them (R18, R39).
	V any
	// Site is where it was thrown: the throw statement, or the operation the
	// runtime threw for. A rethrow is a throw of its own, with its own site;
	// Rethrow, which ends a finally, keeps the throw's.
	Site *Site
}

// String is what a *Thrown reads as wherever Go prints a panic: on a
// goroutine nothing recovers, say, which would be a cakebear bug. It is no
// Go error: nothing returns one.
func (t *Thrown) String() string { return "Uncaught " + Inspect(t.V) }

// Try runs body, a try block, and returns what it threw, or nil if it
// completed. A panic that isn't a throw is raised again.
func Try(body func()) (t *Thrown) {
	defer Catch(&t)
	body()
	return nil
}

// TryK is Try for a block that can leave early, by return, break or
// continue: body returns its completion code, 0 for falling off its end,
// and k is that code, or 0 when it threw.
func TryK(body func() int) (k int, t *Thrown) {
	defer Catch(&t)
	return body(), nil
}

// Catch, deferred, stores a throw in *t and stops it. recover works here
// because Catch is the deferred call itself, which is also why anything
// else is raised again from here rather than after it returns: the frames
// that panicked are still below it (m6-go §2, the trap).
func Catch(t **Thrown) {
	if r := recover(); r != nil {
		th, ok := r.(*Thrown)
		if !ok {
			panic(r)
		}
		*t = th
	}
}

// Rethrow ends a finally: the throw that was pending when it began, if
// any, goes on. A finally that left by return, break, continue or a throw
// of its own never reaches it, which is how each of those discards it
// (R21).
func Rethrow(t *Thrown) {
	if t != nil {
		panic(t)
	}
}

// throwError throws an Error of kind with message, made at site: what the
// runtime does where Node throws (D9).
func throwError(kind ErrorKind, message string, site *Site) {
	panic(&Thrown{V: NewError(kind, message, site), Site: site})
}

// throwCoded is throwError for one of Node's own errors, which carry a code
// (R34): `Error [ERR_HTTP_HEADERS_SENT]: …`.
func throwCoded(kind ErrorKind, code, message string, site *Site) {
	panic(&Thrown{V: newCodedError(kind, code, message, site), Site: site})
}
