package runtime

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// fetch (S1 plan D2), on Go's net/http client, set to send and read as
// Node 24.18's fetch (spikes/s1-go, part 1, against s1-node's R4–R15):
//   - Node's default request headers, set explicitly, a program's own in
//     their place (R4, R6);
//   - `content-type: text/plain;charset=UTF-8` for a string body without
//     one (R6);
//   - no Referer on a redirect, which Go's client adds, and at most 20
//     redirects, with Node's error (R15);
//   - compression decoded here, not by the Transport, so content-encoding
//     and content-length read as sent (R5).
//
// Go writes a request's headers sorted and in canonical case, where Node
// writes the program's order and case: an accepted difference (D9), since
// header names are case-insensitive and order-free (RFC 9110).
//
// A fetch runs on a goroutine of its own and posts what it learns to its
// domain as I/O (domain.go, postIO): the headers, which settle the fetch,
// then the whole body. Only the domain's driver touches the Response.

// fetchClient is every domain's: Go's client is goroutine-safe, and its
// connections are the process's, as Node's are.
var fetchClient = sync.OnceValue(func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	return &http.Client{Transport: transport, CheckRedirect: checkRedirect}
})

// maxRedirects is Node's: the 21st fails (R15).
const maxRedirects = 20

// errRedirectCount and errUnexpectedRedirect carry Node's causes out of
// CheckRedirect, which Go wraps in a *url.Error.
var (
	errRedirectCount      = errors.New("redirect count exceeded")
	errUnexpectedRedirect = errors.New("unexpected redirect")
)

type redirectMode struct{}

func checkRedirect(req *http.Request, via []*http.Request) error {
	switch req.Context().Value(redirectMode{}) {
	case "manual":
		return http.ErrUseLastResponse
	case "error":
		return errUnexpectedRedirect
	}
	if len(via) > maxRedirects {
		return errRedirectCount
	}
	req.Header.Del("Referer") // Go adds one; Node sends none (spike part 1)
	return nil
}

// FetchInit is fetch's init, as lowering hands it over: the method ("" for
// GET); the headers, as pairs written in an object literal or an array, or
// a Headers; a body or none; and redirect ("" for follow).
type FetchInit struct {
	Method   string
	Pairs    [][2]string
	Headers  *Headers
	Body     Maybe[string]
	Redirect string
}

// The methods Node upper-cases (the Fetch Standard's normalisation); any
// other token is sent as written.
var normalizedMethods = map[string]string{"delete": "DELETE", "get": "GET", "head": "HEAD", "options": "OPTIONS", "post": "POST", "put": "PUT"}

// badPorts are the Fetch Standard's, which no fetch may reach.
var badPorts = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range strings.Fields("0 1 7 9 11 13 15 17 19 20 21 22 23 25 37 42 43 53 69 77 79 87 95 101 102 103 104 109 110 " +
		"111 113 115 117 119 123 135 137 139 143 161 179 389 427 465 512 513 514 515 526 530 531 532 540 548 554 556 563 587 " +
		"601 636 989 990 993 995 1719 1720 1723 2049 3659 4045 4190 5060 5061 6000 6566 6665 6666 6667 6668 6669 6679 6697 10080") {
		m[p] = true
	}
	return m
}()

// nodeDefaults are the headers Node's fetch adds that a program didn't set,
// in the order Node sends them (R4).
var nodeDefaults = [][2]string{
	{"accept", "*/*"},
	{"accept-language", "*"},
	{"sec-fetch-mode", "cors"},
	{"user-agent", "node"},
	{"accept-encoding", "gzip, deflate"},
}

// Fetch is `fetch(input, init)` from domain d, its call at site. Every
// failure is a rejection, never a throw (R7).
func Fetch(d *Domain, input string, init *FetchInit, site *Site) *Promise[*Response] {
	p := &Promise[*Response]{settlement: settlement{d: d}}
	if init == nil {
		init = &FetchInit{}
	}
	var req *http.Request
	var fail *Error
	if init.Pairs != nil {
		// A bad name or value in the init's headers rejects, with the
		// message Headers.append throws (R7).
		pairs := init.Pairs
		if t := Try(func() {
			init = &FetchInit{Method: init.Method, Headers: NewHeaders(pairs, site), Body: init.Body, Redirect: init.Redirect}
		}); t != nil {
			fail, _ = t.V.(*Error)
		}
	}
	if fail == nil {
		req, fail = newFetchRequest(input, init, site)
	}
	if fail != nil {
		// Node rejects a bad request two microtask hops after the call
		// (R7): the reject runs in a microtask, and its reactions after.
		d.queueMicrotask(func() { p.reject(&Thrown{V: fail, Site: site}) })
		return p
	}
	d.inflight++
	go func() {
		res, failure := roundTrip(req, input, site)
		d.postIO(func() {
			d.inflight--
			if failure != nil {
				p.reject(&Thrown{V: failure, Site: site})
				return
			}
			res.d = d
			p.resolve(res)
			go res.readBody()
		})
	}()
	return p
}

// newFetchRequest builds the request, or the TypeError Node rejects with
// before sending anything (R7, R11).
func newFetchRequest(input string, init *FetchInit, site *Site) (*http.Request, *Error) {
	u, err := url.Parse(input)
	if err != nil || !u.IsAbs() || u.Host == "" && (u.Scheme == "http" || u.Scheme == "https") {
		return nil, invalidFetchURL(input, site)
	}
	method := init.Method
	if method == "" {
		method = "GET"
	}
	if up, ok := normalizedMethods[strings.ToLower(method)]; ok {
		method = up
	}
	switch {
	case !headerToken(method):
		return nil, NewError(ErrorType, "'"+method+"' is not a valid HTTP method.", site)
	case method == "CONNECT" || method == "TRACE" || method == "TRACK" ||
		strings.EqualFold(method, "connect") || strings.EqualFold(method, "trace") || strings.EqualFold(method, "track"):
		return nil, NewError(ErrorType, "'"+method+"' HTTP method is unsupported.", site)
	case (method == "GET" || method == "HEAD") && !init.Body.IsEmpty():
		return nil, NewError(ErrorType, "Request with GET/HEAD method cannot have body.", site)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fetchFailed(NewError(ErrorBase, "unknown scheme", nil), site)
	}
	if badPorts[u.Port()] {
		// The Fetch Standard's port blocking (measured: port 1 is `fetch
		// failed`, cause `Error: bad port`).
		return nil, fetchFailed(NewError(ErrorBase, "bad port", nil), site)
	}
	ctx := withRedirectMode(init.Redirect)
	var body io.Reader
	if !init.Body.IsEmpty() {
		body = strings.NewReader(init.Body.v)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, invalidFetchURL(input, site)
	}
	if init.Body.IsEmpty() && (method == "POST" || method == "PUT") {
		// Node sends `content-length: 0` for a POST with no body, and none
		// for a DELETE (R6); Go does the same for these.
		req.ContentLength = 0
		req.Body = http.NoBody
	}
	setRequestHeaders(req, init)
	return req, nil
}

// setRequestHeaders sends the program's headers, then Node's defaults it
// didn't set (R4, R6), and the default content type for a string body.
func setRequestHeaders(req *http.Request, init *FetchInit) {
	set := map[string]bool{}
	if init.Headers != nil {
		for _, e := range init.Headers.entries {
			// Canonical keys, as Go reads them itself: under another
			// spelling it would add its own User-Agent beside the program's.
			req.Header.Set(e.name, e.value)
			set[e.lower] = true
		}
	}
	if !init.Body.IsEmpty() && !set["content-type"] {
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	}
	if !set["connection"] {
		req.Header.Set("Connection", "keep-alive")
	}
	for _, kv := range nodeDefaults {
		if !set[kv[0]] {
			req.Header.Set(kv[0], kv[1])
		}
	}
}

// statusText is the reason phrase as it came over the wire, never one
// derived from the code (R8): Go keeps it in Status, after the code.
func statusText(resp *http.Response) string {
	code := strconv.Itoa(resp.StatusCode)
	text, _ := strings.CutPrefix(resp.Status, code)
	return strings.TrimPrefix(text, " ")
}
