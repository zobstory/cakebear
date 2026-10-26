package runtime

import (
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

type response struct {
	status int
	header http.Header
	body   string
	length int64
	err    error
}

func fetch(client *http.Client, method, url string) response {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return response{err: err}
	}
	resp, err := client.Do(req)
	if err != nil {
		return response{err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return response{status: resp.StatusCode, header: resp.Header, body: string(body), length: resp.ContentLength, err: err}
}

// A request that arrives before the program's top-level code has finished is
// held, not served: the port is bound, but serving starts in Run, after the
// listen callback. So "listening" always prints before any handler output,
// as under Node.
func TestListenServesAfterTopLevelCode(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	stdout, _, _ := isolate(t)

	s := CreateServer(func(_ *Domain, req *IncomingMessage, res *ServerResponse) {
		LogString("handled " + req.URL())
		res.SetHeader("Content-Type", "text/plain", nil).End(req.Method() + " " + req.URL())
	})
	s.Listen(0, nil, func(*Domain) { LogString("listening") })
	base := "http://" + s.ln.Addr().String()

	results := make(chan response, 1)
	go func() { results <- fetch(http.DefaultClient, "GET", base+"/health?x=1") }()
	time.Sleep(150 * time.Millisecond)
	if strings.Contains(stdout.String(), "handled") {
		t.Fatalf("a request was served before Run: %q", stdout.String())
	}

	done := runAsync(nil)
	var r response
	select {
	case r = <-results:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the response")
	}
	if r.err != nil {
		t.Fatalf("request failed: %v", r.err)
	}
	if r.status != 200 || r.header.Get("Content-Type") != "text/plain" || r.body != "GET /health?x=1" || r.length != 15 {
		t.Errorf("response = %d %q %q length %d, want 200 text/plain %q length 15",
			r.status, r.header.Get("Content-Type"), r.body, r.length, "GET /health?x=1")
	}
	if got := stdout.String(); got != "listening\nhandled /health?x=1\n" {
		t.Errorf("stdout = %q, want the listen callback before the handler", got)
	}

	if err := s.close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	waitFor(t, done, "Run to return once the server closed")
}

func serveWith(t *testing.T, handler func(*IncomingMessage, *ServerResponse)) string {
	t.Helper()
	s := CreateServer(func(_ *Domain, req *IncomingMessage, res *ServerResponse) { handler(req, res) })
	ts := httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	ts.Config.ErrorLog = log.New(io.Discard, "", 0) // panics are expected in some tests
	ts.Start()
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestServerResponseOverHTTP(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	for _, tt := range []struct { //nolint:paralleltest // isolate swaps process-wide runtime state
		name, method, path string
		handler            func(*IncomingMessage, *ServerResponse)
		status             int
		body, header       string // header is X-A's value
		warns              bool
	}{
		{"writeHead then end", "GET", "/", func(_ *IncomingMessage, res *ServerResponse) { res.WriteHead(404, nil).End() }, 404, "", "", false},
		{"setHeader replaces", "GET", "/", func(_ *IncomingMessage, res *ServerResponse) {
			res.SetHeader("X-A", "1", nil).SetHeader("x-a", "2", nil).End("ok")
		}, 200, "ok", "2", false},
		{"end twice", "GET", "/", func(_ *IncomingMessage, res *ServerResponse) { res.End("a").End() }, 200, "a", "", false},
		{"raw url and method", "POST", "/a%20b?x=1&y=%2F", func(req *IncomingMessage, res *ServerResponse) {
			res.End(req.Method() + " " + req.URL())
		}, 200, "POST /a%20b?x=1&y=%2F", "", false},
		{"no end", "GET", "/forgot", func(_ *IncomingMessage, res *ServerResponse) { res.SetHeader("X-A", "set", nil) }, 200, "", "set", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, _ := isolate(t)
			r := fetch(http.DefaultClient, tt.method, serveWith(t, tt.handler)+tt.path)
			if r.err != nil {
				t.Fatalf("request failed: %v", r.err)
			}
			if r.status != tt.status || r.body != tt.body || r.header.Get("X-A") != tt.header {
				t.Errorf("got %d %q X-A=%q, want %d %q X-A=%q", r.status, r.body, r.header.Get("X-A"), tt.status, tt.body, tt.header)
			}
			warned := strings.Contains(stderr.String(), "returned without calling res.end()")
			if warned != tt.warns {
				t.Errorf("warned = %v, want %v; stderr = %q", warned, tt.warns, stderr.String())
			}
		})
	}
}

// TestContentTypeOnlyWhenSet: a response the program gave no Content-Type
// goes out with none, as Node sends it, rather than with one net/http sniffed
// from the body; one it set is kept.
func TestContentTypeOnlyWhenSet(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	for _, tt := range []struct { //nolint:paralleltest // isolate swaps process-wide runtime state
		name    string
		handler func(*IncomingMessage, *ServerResponse)
		want    []string // the Content-Type values sent, nil for no header
	}{
		{"none set, markup body", func(_ *IncomingMessage, res *ServerResponse) { res.End("<html><body>hi</body></html>") }, nil},
		{"none set, writeHead", func(_ *IncomingMessage, res *ServerResponse) { res.WriteHead(404, nil).End("not found\n") }, nil},
		{"set", func(_ *IncomingMessage, res *ServerResponse) {
			res.SetHeader("content-type", "application/json", nil).End("{}")
		}, []string{"application/json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			r := fetch(http.DefaultClient, "GET", serveWith(t, tt.handler)+"/")
			if r.err != nil {
				t.Fatalf("request failed: %v", r.err)
			}
			if got := r.header["Content-Type"]; !slices.Equal(got, tt.want) {
				t.Errorf("Content-Type = %q, want %q", got, tt.want)
			}
		})
	}
}

// One bad request must not take the server down: a throw nothing catches
// drops only that connection, with the report on stderr (M6 plan D8).
func TestHandlerThrowDropsOnlyItsConnection(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	_, stderr, _ := isolate(t)
	base := serveWith(t, func(req *IncomingMessage, res *ServerResponse) {
		if req.URL() == "/boom" {
			res.WriteHead(200, nil).SetHeader("Late", "x", nil) // Node throws here
		}
		res.End("fine")
	})
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

	if r := fetch(client, "GET", base+"/boom"); r.err == nil {
		t.Errorf("/boom got %d %q, want the connection dropped", r.status, r.body)
	}
	if r := fetch(client, "GET", base+"/"); r.err != nil || r.body != "fine" {
		t.Errorf("after a throw, / got %v %q; the server should still serve", r.err, r.body)
	}
	want := "[Error [ERR_HTTP_HEADERS_SENT]: Cannot set headers after they are sent to the client] {\n  code: 'ERR_HTTP_HEADERS_SENT'\n}\n\ncakebear " + Version + "\n"
	if got := stderr.String(); got != want {
		t.Errorf("stderr = %q, want the report %q", got, want)
	}
}

// Each misuse Node throws for throws Node's own error, String() and all
// (R34), and the calls Node accepts do not. A write after the end is no
// throw in Node (R36): it fails on the next tick, past any try.
func TestResponseMisuseMatchesNode(t *testing.T) {
	t.Parallel()

	fresh := func() *ServerResponse { return &ServerResponse{w: httptest.NewRecorder(), status: 200, d: newDomain()} }
	for _, tt := range []struct {
		name string
		do   func(*ServerResponse)
		want string // "" means it must not throw
	}{
		{"setHeader after writeHead", func(r *ServerResponse) { r.WriteHead(200, nil).SetHeader("a", "b", nil) },
			"Error [ERR_HTTP_HEADERS_SENT]: Cannot set headers after they are sent to the client"},
		{"setHeader after end", func(r *ServerResponse) { r.End().SetHeader("a", "b", nil) },
			"Error [ERR_HTTP_HEADERS_SENT]: Cannot set headers after they are sent to the client"},
		{"writeHead twice", func(r *ServerResponse) { r.WriteHead(200, nil).WriteHead(201, nil) },
			"Error [ERR_HTTP_HEADERS_SENT]: Cannot write headers after they are sent to the client"},
		{"writeHead after end", func(r *ServerResponse) { r.End("a").WriteHead(201, nil) },
			"Error [ERR_HTTP_HEADERS_SENT]: Cannot write headers after they are sent to the client"},
		{"write after end", func(r *ServerResponse) { r.End().End("x") }, ""},
		{"end twice, empty", func(r *ServerResponse) { r.End("a").End() }, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := fresh()
			if got := throws(func() { tt.do(r) }); got != tt.want {
				t.Errorf("threw %q, want %q", got, tt.want)
			}
			late := throws(r.d.drain) // the next tick
			if want := "Error [ERR_STREAM_WRITE_AFTER_END]: write after end"; tt.name == "write after end" && late != want {
				t.Errorf("the next tick threw %q, want %q", late, want)
			} else if tt.name != "write after end" && late != "" {
				t.Errorf("the next tick threw %q", late)
			}
		})
	}

	r := fresh()
	if r.SetHeader("a", "b", nil) != r || r.WriteHead(200, nil) != r || r.End() != r {
		t.Error("a method that returns `this` in TypeScript must return its receiver")
	}
}

// writeHead converts its status as Node's `statusCode |= 0` does, ToInt32,
// and refuses what that leaves outside 100 to 999, naming the status as it
// was passed. Each row is what Node 24.18 sent or threw for that status.
func TestWriteHeadStatusMatchesNode(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		status float64
		sent   int    // the status Node sent, or 0 where it threw
		thrown string // the message after "RangeError [ERR_HTTP_INVALID_STATUS_CODE]: "
	}{
		{200, 200, ""},
		{200.5, 200, ""},
		{999.9, 999, ""},
		{4294967496, 200, ""}, // 2^32 + 200
		{999, 999, ""},
		{99, 0, "Invalid status code: 99"},
		{99.99, 0, "Invalid status code: 99.99"},
		{1000, 0, "Invalid status code: 1000"},
		{-0.5, 0, "Invalid status code: -0.5"},
		{math.Copysign(0, -1), 0, "Invalid status code: -0"},
		{1e10, 0, "Invalid status code: 10000000000"},
		{2147483848, 0, "Invalid status code: 2147483848"}, // wraps negative
		{4294967295.5, 0, "Invalid status code: 4294967295.5"},
		{math.Inf(1), 0, "Invalid status code: Infinity"},
		{math.Inf(-1), 0, "Invalid status code: -Infinity"},
		{math.NaN(), 0, "Invalid status code: NaN"},
		{1e21, 0, "Invalid status code: 1e+21"},
		{1e-7, 0, "Invalid status code: 1e-7"},
	} {
		t.Run(numberForDisplay(tt.status), func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			r := &ServerResponse{w: rec, status: 200}
			got := throws(func() { r.WriteHead(tt.status, nil).End() })
			if tt.thrown != "" {
				if want := "RangeError [ERR_HTTP_INVALID_STATUS_CODE]: " + tt.thrown; got != want {
					t.Errorf("threw %q, want %q", got, want)
				}
				return
			}
			if got != "" {
				t.Fatalf("threw %q; Node sent %d", got, tt.sent)
			}
			if rec.Code != tt.sent {
				t.Errorf("sent %d, want %d, as Node sent", rec.Code, tt.sent)
			}
		})
	}
}

// panicked runs f and gives the value of a Go panic, as fmt prints it, or
// "" if it returned. A throw is no panic: it is "threw " and String() of
// what it threw (throws), so a test that wants a panic fails on one.
func panicked(f func()) (msg string) {
	defer func() {
		switch p := recover().(type) {
		case nil:
		case *Thrown:
			msg = "threw " + throws(func() { panic(p) })
		default:
			msg = fmt.Sprint(p)
		}
	}()
	f()
	return ""
}

func TestListenPortInUse(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	_, stderr, exits := isolate(t)
	hold, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Close() }()
	port := hold.Addr().(*net.TCPAddr).Port

	CreateServer(func(*Domain, *IncomingMessage, *ServerResponse) {}).Listen(float64(port), nil)
	if got := exitCode(t, exits); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
	want := fmt.Sprintf("Error: listen EADDRINUSE: address already in use 127.0.0.1:%d", port)
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
	if liveHandles.Load() != 0 || !queueEmpty() {
		t.Error("a server that failed to bind was counted as listening")
	}
}

func TestListenRejectsBadPorts(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	// Each is refused as Node 24.18 refuses it, in its words, with a
	// RangeError listen throws there and then, which a try can catch (R37);
	// -0 is port 0.
	for _, port := range []float64{70000, 65536, 3000.5, -1, math.NaN(), math.Inf(1), 1e21, 4294967296, 1e-7} { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(NumberToString(port), func(t *testing.T) {
			_, stderr, _ := isolate(t)
			s := CreateServer(func(*Domain, *IncomingMessage, *ServerResponse) {})
			th := thrown(t, func() { s.Listen(port, nil) })
			if s.srv != nil {
				t.Cleanup(func() { _ = s.close() }) // only if a mutation let it bind
			}
			e, _ := th.V.(*Error)
			want := "RangeError [ERR_SOCKET_BAD_PORT]: options.port should be >= 0 and < 65536. Received type number (" + NumberToString(port) + ")."
			if e == nil || ErrorToString(e) != want || e.code != "ERR_SOCKET_BAD_PORT" || e.Kind() != ErrorRange {
				t.Errorf("threw %v, want %q", th.V, want)
			}
			if stderr.String() != "" || liveHandles.Load() != 0 || !queueEmpty() {
				t.Errorf("a refused port wrote %q, or was counted as listening", stderr.String())
			}
		})
	}
}
