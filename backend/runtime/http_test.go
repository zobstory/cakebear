package runtime

import (
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
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

	s := CreateServer(func(req *IncomingMessage, res *ServerResponse) {
		LogString("handled " + req.URL())
		res.SetHeader("Content-Type", "text/plain").End(req.Method() + " " + req.URL())
	})
	s.Listen(0, func() { LogString("listening") })
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
	s := CreateServer(handler)
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
		{"writeHead then end", "GET", "/", func(_ *IncomingMessage, res *ServerResponse) { res.WriteHead(404).End() }, 404, "", "", false},
		{"setHeader replaces", "GET", "/", func(_ *IncomingMessage, res *ServerResponse) {
			res.SetHeader("X-A", "1").SetHeader("x-a", "2").End("ok")
		}, 200, "ok", "2", false},
		{"end twice", "GET", "/", func(_ *IncomingMessage, res *ServerResponse) { res.End("a").End() }, 200, "a", "", false},
		{"raw url and method", "POST", "/a%20b?x=1&y=%2F", func(req *IncomingMessage, res *ServerResponse) {
			res.End(req.Method() + " " + req.URL())
		}, 200, "POST /a%20b?x=1&y=%2F", "", false},
		{"no end", "GET", "/forgot", func(_ *IncomingMessage, res *ServerResponse) { res.SetHeader("X-A", "set") }, 200, "", "set", true},
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

// One bad request must not take the server down: net/http recovers the
// handler's panic and drops only that connection.
func TestHandlerPanicDropsOnlyItsConnection(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	base := serveWith(t, func(req *IncomingMessage, res *ServerResponse) {
		if req.URL() == "/boom" {
			res.WriteHead(200).SetHeader("Late", "x") // Node throws here
		}
		res.End("fine")
	})
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

	if r := fetch(client, "GET", base+"/boom"); r.err == nil {
		t.Errorf("/boom got %d %q, want the connection dropped", r.status, r.body)
	}
	if r := fetch(client, "GET", base+"/"); r.err != nil || r.body != "fine" {
		t.Errorf("after a panic, / got %v %q; the server should still serve", r.err, r.body)
	}
}

// Each misuse Node throws for panics with Node's own message, and the calls
// Node accepts do not.
func TestResponseMisuseMatchesNode(t *testing.T) {
	t.Parallel()

	fresh := func() *ServerResponse { return &ServerResponse{w: httptest.NewRecorder(), status: 200} }
	for _, tt := range []struct {
		name string
		do   func(*ServerResponse)
		want string // "" means it must not panic
	}{
		{"setHeader after writeHead", func(r *ServerResponse) { r.WriteHead(200).SetHeader("a", "b") }, "ERR_HTTP_HEADERS_SENT"},
		{"setHeader after end", func(r *ServerResponse) { r.End().SetHeader("a", "b") }, "ERR_HTTP_HEADERS_SENT"},
		{"writeHead twice", func(r *ServerResponse) { r.WriteHead(200).WriteHead(201) }, "ERR_HTTP_HEADERS_SENT"},
		{"status too low", func(r *ServerResponse) { r.WriteHead(99) }, "Invalid status code: 99"},
		{"status too high", func(r *ServerResponse) { r.WriteHead(1000) }, "Invalid status code: 1000"},
		{"status fractional", func(r *ServerResponse) { r.WriteHead(200.5) }, "Invalid status code: 200.5"},
		{"status NaN", func(r *ServerResponse) { r.WriteHead(math.NaN()) }, "Invalid status code: NaN"},
		{"write after end", func(r *ServerResponse) { r.End().End("x") }, "ERR_STREAM_WRITE_AFTER_END"},
		{"end twice, empty", func(r *ServerResponse) { r.End("a").End() }, ""},
		{"status bounds", func(r *ServerResponse) { r.WriteHead(999).End() }, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := panicked(func() { tt.do(fresh()) })
			switch {
			case tt.want == "" && got != "":
				t.Errorf("panicked with %q, want no panic", got)
			case tt.want != "" && !strings.Contains(got, tt.want):
				t.Errorf("panic = %q, want one containing %q", got, tt.want)
			}
		})
	}

	r := fresh()
	if r.SetHeader("a", "b") != r || r.WriteHead(200) != r || r.End() != r {
		t.Error("a method that returns `this` in TypeScript must return its receiver")
	}
}

func panicked(f func()) (msg string) {
	defer func() {
		if p := recover(); p != nil {
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

	CreateServer(func(*IncomingMessage, *ServerResponse) {}).Listen(float64(port))
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
	for _, port := range []float64{70000, 3000.5, -1, math.NaN()} { //nolint:paralleltest // isolate swaps process-wide runtime state
		t.Run(NumberToString(port), func(t *testing.T) {
			_, stderr, exits := isolate(t)
			s := CreateServer(func(*IncomingMessage, *ServerResponse) {}).Listen(port)
			if s.srv != nil {
				t.Cleanup(func() { _ = s.close() }) // only if a mutation let it bind
			}
			if got := exitCode(t, exits); got != 1 {
				t.Errorf("exit code = %d, want 1", got)
			}
			want := "ERR_SOCKET_BAD_PORT]: port should be >= 0 and < 65536. Received " + NumberToString(port)
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr = %q, want %q", stderr.String(), want)
			}
		})
	}
}
