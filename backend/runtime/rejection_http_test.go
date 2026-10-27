package runtime

import (
	"net/http"
	"strings"
	"testing"
)

// An async request handler's rejection nothing handles (M6 plan D8,
// done-check 7): Node ends the whole server, and cakebear only the request.
// Before the response ended its connection is dropped, after it the
// response stands, and either way the report goes to stderr as Node's goes,
// and the server serves on.

// asyncHandlerCase is testdata/async/async-handler.ts's handler, as the
// emitter writes an async arrow in a void slot: the call, its promise
// dropped.
func asyncHandlerCase(d *Domain, req *IncomingMessage, res *ServerResponse) {
	const file = "async-handler.ts"
	fail := func(d *Domain) *Promise[struct{}] {
		return Async(d, func(*Co) struct{} {
			panic(thrownError(file, 15, 3, 9, `  throw new TypeError("nothing awaits this");`, ErrorType, "nothing awaits this"))
		})
	}
	Async(d, func(co *Co) struct{} {
		if req.URL() == "/ok" {
			res.End("ok")
			return struct{}{}
		}
		if req.URL() == "/before-await" {
			panic(thrownError(file, 22, 36, 42, `  if (req.url === "/before-await") throw new Error("before any await");`, ErrorBase, "before any await"))
		}
		if req.URL() == "/after-end" {
			res.End("partial")
			panic(thrownError(file, 25, 5, 11, `    throw new Error("after the end");`, ErrorBase, "after the end"))
		}
		if req.URL() == "/unhandled" {
			fail(d)
			res.End("sent")
			return struct{}{}
		}
		if req.URL() == "/after-timer" {
			Await(co, sleepFn(d, 5))
			panic(thrownError(file, 34, 5, 11, `    throw new RangeError("after a timer");`, ErrorRange, "after a timer"))
		}
		if req.URL() == "/after-end-await" {
			res.End("partial")
			Await(co, tick(d))
			panic(thrownError(file, 39, 5, 11, `    throw new Error("after the end and an await");`, ErrorBase, "after the end and an await"))
		}
		if req.URL() == "/after-end-timer" {
			res.End("partial")
			Await(co, sleepFn(d, 5))
			panic(thrownError(file, 44, 5, 11, `    throw new Error("after the end and a timer");`, ErrorBase, "after the end and a timer"))
		}
		Await(co, tick(d))
		panic(thrownError(file, 47, 3, 9, `  throw new Error("after an await");`, ErrorBase, "after an await"))
	})
}

// asyncHandlerValueCase is testdata/async/async-handler-value.ts's.
func asyncHandlerValueCase(d *Domain, req *IncomingMessage, res *ServerResponse) {
	Async(d, func(co *Co) struct{} {
		if req.URL() == "/ok" {
			res.End("ok")
			return struct{}{}
		}
		if req.URL() == "/string" {
			panic(&Thrown{V: "a string", Site: site("async-handler-value.ts", 14, 30, `  if (req.url === "/string") throw "a string";`)})
		}
		Await(co, tick(d))
		panic(&Thrown{V: 42.0, Site: site("async-handler-value.ts", 16, 3, "  throw 42;")})
	})
}

// TestAsyncHandlerRejectionsEndOnlyTheirRequest: each failing request gets
// what Node's client got, the response or a dropped connection, the report
// is Node's, pointing where the error was made, as Node's does for a
// rejection, or for a reason that isn't an Error at the throw, and the
// server serves on.
func TestAsyncHandlerRejectionsEndOnlyTheirRequest(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	cases := loadAsync(t)
	for _, c := range []struct {
		name    string
		handler func(*Domain, *IncomingMessage, *ServerResponse)
		excerpt string
	}{
		{"async-handler/before-await", asyncHandlerCase, ""},
		{"async-handler/after-await", asyncHandlerCase, ""},
		{"async-handler/after-end", asyncHandlerCase, ""},
		{"async-handler/after-end-await", asyncHandlerCase, ""},
		{"async-handler/after-end-timer", asyncHandlerCase, ""}, // in the work after the response (finishDetached)
		{"async-handler/after-timer", asyncHandlerCase, ""},
		{"async-handler/unhandled", asyncHandlerCase, ""},
		{"async-handler-value/string", asyncHandlerValueCase,
			"file:///CASES/async-handler-value.ts:14\n  if (req.url === \"/string\") throw \"a string\";\n" + strings.Repeat(" ", 29) + "^\n"},
		{"async-handler-value/number", asyncHandlerValueCase, "file:///CASES/async-handler-value.ts:16\n  throw 42;\n  ^\n"},
	} { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(c.name, func(t *testing.T) {
			golden, ok := cases[c.name]
			if !ok {
				t.Fatalf("no golden for %s", c.name)
			}
			path := c.name[strings.Index(c.name, "/"):]
			_, stderr, _ := isolate(t)
			base := serveAsync(t, c.handler)
			client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

			r := fetch(client, "GET", base+path)
			got := "request error"
			if r.err == nil {
				got = r.describe()
			}
			if !strings.HasPrefix(golden.Limit0.Client, got) {
				t.Errorf("%s got %s (%v), want what Node's client got, %s", path, got, r.err, golden.Limit0.Client)
			}
			want := cakebearsReport(t, golden, c.excerpt)
			waitUntil(t, "the report", func() bool { return stderr.String() == want })
			// Node's server is gone by now; this one serves on.
			if r := fetch(client, "GET", base+"/ok"); r.err != nil || r.body != "ok" {
				t.Errorf("after the rejection, /ok got %v %q; the server should still serve", r.err, r.body)
			}
		})
	}
}
