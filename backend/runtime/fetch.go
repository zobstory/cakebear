package runtime

import (
	"context"
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
	Cache    string // "" for default
	// Form marks a body that was a URLSearchParams, whose default content
	// type is the form encoding's; Signal is init's signal, or nil.
	Form   bool
	Signal *AbortSignal
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
	if init.Pairs != nil && init.Headers == nil {
		// A bad name or value in the init's headers rejects, with the
		// message Headers.append throws (R7).
		pairs := init.Pairs
		if t := Try(func() {
			with := *init
			with.Pairs, with.Headers = nil, NewHeaders(pairs, site)
			init = &with
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
	signal := init.Signal
	if signal != nil && signal.reason != nil {
		p.reject(&Thrown{V: signal.reason, Site: site}) // aborted already
		return p
	}
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	var waiting *abortWait
	if signal != nil {
		// An abort rejects at once, in the timer that fired it, as Node's
		// abort event does (R16); what the cancelled request posts later
		// is ignored.
		waiting = signal.wait(d, func(reason *Error) {
			cancel()
			p.reject(&Thrown{V: reason, Site: site})
		})
	}
	d.inflight++
	go func() {
		res, failure := roundTrip(req, input, site)
		d.postIO(func() {
			d.inflight--
			if p.settled {
				if res != nil {
					_ = res.resp.Body.Close()
				}
				cancel()
				return
			}
			waiting.release()
			if failure != nil {
				cancel()
				p.reject(&Thrown{V: failure, Site: site})
				return
			}
			res.d, res.cancel = d, cancel
			if signal != nil {
				res.waiting = signal.wait(d, res.abort)
			}
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
	switch init.Cache {
	case "", "default", "no-store", "reload", "no-cache", "force-cache":
	case "only-if-cached":
		return nil, NewError(ErrorType, "'only-if-cached' can be set only with 'same-origin' mode", site)
	default:
		return nil, NewError(ErrorType, "undefined: "+init.Cache+" is not an accepted type. Expected one of default, no-store, reload, no-cache, force-cache, only-if-cached.", site)
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
	switch {
	case init.Body.IsEmpty() || set["content-type"]:
	case init.Form:
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	default:
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	}
	if !set["connection"] {
		req.Header.Set("Connection", "keep-alive")
	}
	// The cache modes that bypass a cache say so to the server, as the
	// Fetch Standard has it (measured on Node 24.18).
	switch init.Cache {
	case "no-store", "reload":
		if !set["pragma"] {
			req.Header.Set("Pragma", "no-cache")
		}
		if !set["cache-control"] {
			req.Header.Set("Cache-Control", "no-cache")
		}
	case "no-cache":
		if !set["cache-control"] {
			req.Header.Set("Cache-Control", "max-age=0")
		}
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

// FetchOption is one property of fetch's init, as the emitter writes them in
// the order the program's literal does, so they are evaluated in its order.
type FetchOption func(*FetchInit)

// FetchInitOf is init built from its properties.
func FetchInitOf(opts ...FetchOption) *FetchInit {
	init := &FetchInit{Pairs: [][2]string{}}
	for _, o := range opts {
		o(init)
	}
	if len(init.Pairs) == 0 {
		init.Pairs = nil
	}
	return init
}

func FetchMethod(m string) FetchOption { return func(i *FetchInit) { i.Method = m } }

// FetchHeader is one property of an object literal of headers.
func FetchHeader(name, value string) FetchOption {
	return func(i *FetchInit) { i.Pairs = append(i.Pairs, [2]string{name, value}) }
}

// FetchHeaders is a Headers value as init's headers.
func FetchHeaders(h *Headers) FetchOption { return func(i *FetchInit) { i.Headers = h } }

func FetchBody(b string) FetchOption { return func(i *FetchInit) { i.Body = Some(b) } }

// FetchFormBody is a URLSearchParams as the body, b its form encoding.
func FetchFormBody(b string) FetchOption {
	return func(i *FetchInit) { i.Body, i.Form = Some(b), true }
}

// FetchSignal is init's signal.
func FetchSignal(s *AbortSignal) FetchOption { return func(i *FetchInit) { i.Signal = s } }

func FetchRedirect(mode string) FetchOption { return func(i *FetchInit) { i.Redirect = mode } }

func FetchCache(mode string) FetchOption { return func(i *FetchInit) { i.Cache = mode } }

// FetchDropped is a property Node's fetch ignores, Next.js's `next` say: its
// value was evaluated for its effects, and nothing more.
func FetchDropped(any) FetchOption { return func(*FetchInit) {} }
