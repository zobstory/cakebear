package runtime

import (
	"net/http"
	"strings"
	"testing"
)

// A throw nothing catches in a request handler (M6 plan D8): Node ends the
// whole server, and cakebear ends only the request. Before the response
// ended its connection is dropped, after it the response stands, and either
// way the report goes to stderr, as Node's goes, and the server serves on.

// handlerCase is testdata/uncaught/handler.ts, as the emitter writes it.
func handlerCase(_ *Domain, req *IncomingMessage, res *ServerResponse) {
	if req.URL() == "/ok" {
		res.End("ok")
		return
	}
	if req.URL() == "/string" {
		throwAt("a string", site("handler.ts", 9, 30, `  if (req.url === "/string") throw "a string";`))
	}
	if req.URL() == "/after" {
		res.End("partial")
		const text = `    throw new RangeError("after the end");`
		throwAt(NewError(ErrorRange, "after the end", site("handler.ts", 12, 11, text)), site("handler.ts", 12, 5, text))
	}
	if req.URL() == "/header" {
		res.SetHeader("bad name", "v", site("handler.ts", 14, 34, `  if (req.url === "/header") res.setHeader("bad name", "v");`))
	}
	const text = `  throw new Error("before the end");`
	throwAt(NewError(ErrorBase, "before the end", site("handler.ts", 15, 9, text)), site("handler.ts", 15, 3, text))
}

// TestHandlerThrowsEndOnlyTheirRequest: each failing request gets what
// Node's client got, the response or a dropped connection, and the report
// Node prints, at the throw; for node:http's own error, which Node reports
// from its own code, at the call, where Node's frame for the handler points.
func TestHandlerThrowsEndOnlyTheirRequest(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	cases := loadUncaught(t)
	for _, path := range []string{"/boom", "/string", "/after", "/header"} { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(path, func(t *testing.T) {
			c := cases["handler"+path]
			_, stderr, _ := isolate(t)
			base := serveAsync(t, handlerCase)
			client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

			r := fetch(client, "GET", base+path)
			got := "request error"
			if r.err == nil {
				got = r.describe()
			}
			if !strings.HasPrefix(c.Limit0.Client, got) {
				t.Errorf("%s got %s (%v), want what Node's client got, %s", path, got, r.err, c.Limit0.Client)
			}
			want := cakebearsReport(t, c, "")
			if path == "/header" {
				body := want[strings.Index(want, "\n\n")+2:]
				want = "file:///CASES/handler.ts:14\n  if (req.url === \"/header\") res.setHeader(\"bad name\", \"v\");\n" +
					strings.Repeat(" ", 33) + "^\n\n" + body
			}
			waitUntil(t, "the report", func() bool { return stderr.String() == want })
			// Node's server is gone by now; this one serves on.
			if r := fetch(client, "GET", base+"/ok"); r.err != nil || r.body != "ok" {
				t.Errorf("after the throw, /ok got %v %q; the server should still serve", r.err, r.body)
			}
		})
	}
}

// describe is a response as the Node client in gen-uncaught-goldens.mjs
// writes one.
func (r response) describe() string {
	return strings.Join([]string{NumberToString(float64(r.status)), quoteJSON(r.body)}, " ")
}

func quoteJSON(s string) string { return string(appendJSONString(nil, s)) }
