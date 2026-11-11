package runtime

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
)

func gzippedLen(s string) int {
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	_, _ = io.WriteString(zw, s)
	_ = zw.Close()
	return z.Len()
}

// echoed is what /echo answers: the request as the server read it.
type echoed struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// fetchEcho fetches path with init and decodes /echo's answer.
func fetchEcho(t *testing.T, url string, init *FetchInit) (echoed, string) {
	t.Helper()
	var e echoed
	out := runProgram(t, func(d *Domain, log func(string)) {
		Async(d, func(co *Co) struct{} {
			r := Await(co, Fetch(d, url, init, nil))
			log(fmt.Sprintf("%v %v", r.Status(), r.Redirected()))
			log(Await(co, r.Text(d, nil)))
			return struct{}{}
		})
	})
	first, body, _ := strings.Cut(out, "\n")
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("%v in %q", err, out)
	}
	return e, first
}

// Node's defaults, and a program's headers in their place (R4, R6).
func TestFetchSendsNodesHeaders(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	base := fetchServer(t)
	e, _ := fetchEcho(t, base+"/echo", nil)
	for name, want := range map[string]string{
		"accept": "*/*", "accept-language": "*", "sec-fetch-mode": "cors", "user-agent": "node",
		"accept-encoding": "gzip, deflate", "connection": "keep-alive",
	} {
		if got := e.Headers[name]; got != want {
			t.Errorf("a bare fetch sent %s %q, want %q, as Node sends", name, got, want)
		}
	}
	if e.Method != "GET" || e.Headers["content-type"] != "" {
		t.Errorf("a bare fetch was %s with content-type %q, want GET with none", e.Method, e.Headers["content-type"])
	}

	e, _ = fetchEcho(t, base+"/echo", &FetchInit{
		Method: "post",
		Pairs:  [][2]string{{"User-Agent", "mine"}, {"X-Rep", "1"}, {"x-rep", "2"}, {"X-Pad", "  padded  "}},
		Body:   Some("hé 😀"),
	})
	for name, want := range map[string]string{
		"user-agent": "mine", "x-rep": "1, 2", "x-pad": "padded",
		"content-type": "text/plain;charset=UTF-8", "content-length": "8",
	} {
		if got := e.Headers[name]; got != want {
			t.Errorf("a POST sent %s %q, want %q, as Node sends", name, got, want)
		}
	}
	if e.Method != "POST" || e.Body != "hé 😀" {
		t.Errorf("method %q body %q, want POST and the body", e.Method, e.Body)
	}

	e, _ = fetchEcho(t, base+"/echo", &FetchInit{Method: "POST"})
	if e.Headers["content-length"] != "0" || e.Headers["content-type"] != "" {
		t.Errorf("a POST with no body sent content-length %q content-type %q, want 0 and none (R6)", e.Headers["content-length"], e.Headers["content-type"])
	}
}

// Statuses never throw, and statusText is the wire's (R8).
func TestFetchStatusText(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	for _, tt := range []struct{ line, want string }{
		{"HTTP/1.1 200 All Good", `200 true "All Good" false`},
		{"HTTP/1.1 404 Not Found", `404 false "Not Found" false`},
		{"HTTP/1.1 299 unknown", `299 true "unknown" false`},
		{"HTTP/1.1 418 I'm a Teapot", `418 false "I'm a Teapot" false`},
		{"HTTP/1.1 500 ", `500 false "" false`},
	} {
		base := rawServer(t, tt.line+"\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		got := runProgram(t, fetchProgram([]string{base + "/"}, []*FetchInit{nil}, showText))
		if want := tt.want + "\nok\n"; got != want {
			t.Errorf("%s: printed %q, want %q", tt.line, got, want)
		}
	}
}

// Redirects: Node's methods for each code, no Referer, and its limit (R15).
func TestFetchRedirects(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	base := fetchServer(t)
	for _, tt := range []struct {
		code, method, body string
	}{
		{"301", "GET", ""}, {"302", "GET", ""}, {"303", "GET", ""}, {"307", "POST", "x"}, {"308", "POST", "x"},
	} {
		e, first := fetchEcho(t, base+"/redirect?code="+tt.code, &FetchInit{Method: "POST", Body: Some("x"), Pairs: [][2]string{{"Content-Type", "text/plain"}}})
		if first != "200 true" || e.Method != tt.method || e.Body != tt.body || e.Headers["referer"] != "" {
			t.Errorf("%s after a POST: %s, %s %q referer %q, want 200 true, %s %q and none", tt.code, first, e.Method, e.Body, e.Headers["referer"], tt.method, tt.body)
		}
		if tt.method == "GET" && e.Headers["content-type"] != "" {
			t.Errorf("%s after a POST kept content-type %q", tt.code, e.Headers["content-type"])
		}
	}
	got := runProgram(t, fetchProgram(
		[]string{base + "/chain?n=20", base + "/chain?n=21", base + "/redirect?code=301", base + "/redirect?code=301"},
		[]*FetchInit{nil, nil, {Redirect: "manual"}, {Redirect: "error"}},
		func(_ *Domain, _ *Co, r *Response, log func(string)) {
			log(fmt.Sprintf("%v %v %v", r.Status(), r.Redirected(), MaybeToString(r.Headers().Get("location", nil), func(s string) string { return s })))
		}))
	want := "200 true null\nrejected TypeError: fetch failed\n301 false /echo?after=301\nrejected TypeError: fetch failed\n"
	if got != want {
		t.Errorf("printed\n%s\nwant\n%s", got, want)
	}
}

// gzip decoded, its headers as sent (R5); text() as UTF-8, a BOM dropped,
// each ill-formed subpart one U+FFFD (measured on Node 24.18).
func TestFetchBodies(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	base := fetchServer(t)
	got := runProgram(t, func(d *Domain, log func(string)) {
		Async(d, func(co *Co) struct{} {
			r := Await(co, Fetch(d, base+"/gzip", nil, nil))
			str := func(s string) string { return s }
			log(MaybeToString(r.Headers().Get("content-encoding", nil), str) + " " + MaybeToString(r.Headers().Get("content-length", nil), str))
			log(Await(co, r.Text(d, nil)))
			for _, hex := range []string{"61e28262", "efbbbf61", "eda08061", "f09f98", "c0af", "636166e9"} {
				r := Await(co, Fetch(d, base+"/bytes?hex="+hex, nil, nil))
				log(fmt.Sprintf("%+q", Await(co, r.Text(d, nil))))
			}
			again := Await(co, Fetch(d, base+"/hello", nil, nil))
			Await(co, again.Text(d, nil))
			if t := Try(func() { Await(co, again.Text(d, nil)) }); t != nil {
				log(UnknownToString(t.V))
			}
			return struct{}{}
		})
	})
	// content-length is the compressed length, as the server sent it.
	want := "gzip " + strconv.Itoa(gzippedLen("this body was gzipped by the server")) + "\nthis body was gzipped by the server\n" +
		"\"a\\ufffdb\"\n\"a\"\n\"\\ufffd\\ufffd\\ufffda\"\n\"\\ufffd\"\n\"\\ufffd\\ufffd\"\n\"caf\\ufffd\"\n" +
		"TypeError: Body is unusable: Body has already been read\n"
	if got != want {
		t.Errorf("printed\n%s\nwant\n%s", got, want)
	}
}

// Requests Node refuses before sending, as rejections (R7, R11).
func TestFetchRefusals(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	base := fetchServer(t)
	got := runProgram(t, fetchProgram(
		[]string{"not a url", "/echo", base + "/echo", base + "/echo", base + "/echo", base + "/echo", base + "/echo", "ftp://127.0.0.1/x", "http://127.0.0.1:1/"},
		[]*FetchInit{nil, nil, {Body: Some("x")}, {Method: "BAD METHOD"}, {Method: "connect"}, {Pairs: [][2]string{{"bad name", "x"}}}, {Pairs: [][2]string{{"x", "a\nb"}}}, nil, nil},
		showText))
	want := strings.Join([]string{
		"rejected TypeError: Failed to parse URL from not a url",
		"rejected TypeError: Failed to parse URL from /echo",
		"rejected TypeError: Request with GET/HEAD method cannot have body.",
		"rejected TypeError: 'BAD METHOD' is not a valid HTTP method.",
		"rejected TypeError: 'connect' HTTP method is unsupported.",
		"rejected TypeError: Headers.append: \"bad name\" is an invalid header name.",
		"rejected TypeError: Headers.append: \"a\nb\" is an invalid header value.",
		"rejected TypeError: fetch failed",
		"rejected TypeError: fetch failed",
	}, "\n") + "\n"
	if got != want {
		t.Errorf("printed\n%s\nwant\n%s", got, want)
	}
}
