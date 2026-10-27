package runtime

import (
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"syscall"
)

// node:http, as much of it as types/node-http.d.ts declares. Each exported
// name is what generated code calls for one ir.HostOp:
//
//	createServer(listener)       CreateServer(func(*Domain, *IncomingMessage, *ServerResponse))
//	server.listen(port, cb?)     (*Server).Listen(float64, *Site, ...func(*Domain))
//	res.setHeader(name, value)   (*ServerResponse).SetHeader(string, string, *Site)
//	res.writeHead(status)        (*ServerResponse).WriteHead(float64, *Site)
//	res.end(chunk?)              (*ServerResponse).End(...string)
//	req.url, req.method          (*IncomingMessage).URL(), .Method()
//
// Methods that return `this` in TypeScript return their receiver, so a chained
// call emits as written. The optional arguments are variadic so a call emits
// with the arguments the source gave; lower/ has already checked the arity.
// A *Site is the call's (thrown.go), for the report of the error it may throw.
//
// Each request is its own domain (domain.go), driven by the goroutine net/http
// gives it, so requests run in parallel while the async code within one runs
// in Node's order. Where Node throws (headers set after they were sent, a
// status code out of range, a bad port) the runtime throws Node's error, code
// and all (M6 plan D9, R34), which a try can catch. A throw nothing catches,
// or a rejection nothing handles, ends only its request (D8): before the
// response ends its connection is dropped, after it the response stands, and
// the report goes to stderr either way. Node would end the whole server.

// listenHost is the address servers bind: every interface, as Node's default
// is. Tests narrow it to loopback.
var listenHost = ""

// Server is node:http's Server.
type Server struct {
	listener func(*Domain, *IncomingMessage, *ServerResponse)
	ln       net.Listener
	srv      *http.Server
}

// CreateServer is node:http's createServer. The listener runs in its request's
// domain, which it takes first, as every compiled function does.
func CreateServer(listener func(*Domain, *IncomingMessage, *ServerResponse)) *Server {
	return &Server{listener: listener}
}

// Listen is server.listen(port, callback?).
//
// The port is bound before Listen returns, so a port already in use is
// reported straight away, with the exit code 1 Node's EADDRINUSE crash gives.
// Serving and the callback wait until the program's top-level code is done:
// both are queued for Run, the callback first, so "listening on …" is printed
// before any handler runs, as under Node. The callback runs in the main
// program's domain, wherever listen was called from, because Node runs it on
// the main thread. A port Node refuses is a RangeError it throws there and
// then (R37).
func (s *Server) Listen(port float64, site *Site, callbacks ...func(*Domain)) *Server {
	p, ok := portNumber(port)
	if !ok {
		throwCoded(ErrorRange, "ERR_SOCKET_BAD_PORT", "options.port should be >= 0 and < 65536. Received type number ("+numberForDisplay(port)+").", site)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(listenHost, strconv.Itoa(p)))
	if err != nil {
		host := listenHost
		if host == "" {
			host = "::"
		}
		if errors.Is(err, syscall.EADDRINUSE) {
			fatalf("Error: listen EADDRINUSE: address already in use %s:%d", host, p)
		} else {
			fatalf("Error: listen %s:%d: %v", host, p, err)
		}
		return s
	}

	s.ln = ln
	s.srv = &http.Server{Handler: http.HandlerFunc(s.serve)}
	ref()
	for _, cb := range callbacks {
		if cb != nil {
			enqueue(func() { cb(mainDomain) })
		}
	}
	enqueue(func() { go s.run() })
	return s
}

func (s *Server) run() {
	err := s.srv.Serve(s.ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatalf("Error: %v", err)
	}
	unref()
}

// close stops the server. The declarations have no server.close yet; this is
// the path it will take, and the one tests use.
func (s *Server) close() error { return s.srv.Close() }

// portNumber accepts what Node accepts: an integer from 0 to 65535, where 0
// asks the system for any free port.
func portNumber(f float64) (int, bool) {
	if f != math.Trunc(f) || f < 0 || f >= 65536 {
		return 0, false
	}
	return int(f), true
}

// serve runs one request in a domain of its own.
//
// Until the response ends, this goroutine drives the domain: the listener,
// its microtasks, then its timers as they come due. Go finishes a response
// when its handler returns, while Node finishes one at res.end(), and a Node
// handler may go on working after that. So once the response has ended, serve
// returns, and whatever the domain still has to do carries on on a goroutine
// of its own, which keeps the process alive until it is done.
//
// A throw nothing caught, or a rejection nothing handled (an async
// handler's, after an await say), ends the request, and only it (M6 plan
// D8): the report goes to stderr, and the connection is dropped, quietly,
// unless the response had already ended, in which case it stands. A Go
// panic goes on to net/http, which drops the connection and logs it with its
// stack, as before. Either way the domain is abandoned with whatever it had
// left to do, and so is one that ran out of work, and its async calls still
// parked are ended (parked.go).
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	d := newDomain()
	res := &ServerResponse{w: w, status: http.StatusOK, d: d}
	detached := false
	defer func() {
		p := recover()
		if !detached {
			d.abort()
		}
		if p == nil {
			return
		}
		report, ok := failureReport(p, d.running)
		if !ok {
			panic(p)
		}
		writeErr(report)
		if !res.ended {
			panic(http.ErrAbortHandler) // net/http drops the connection and logs nothing
		}
	}()
	d.running = inHandler
	d.runTask(func() { s.listener(d, &IncomingMessage{r: r, url: requestURL(r.RequestURI)}, res) })
	for !res.ended && d.pending() {
		d.waitNext()
	}
	if !res.ended {
		// Node would leave the request open until it timed out. net/http
		// finishes it instead; say so rather than hide the bug.
		warnf("cakebear: the handler for %s %s returned without calling res.end(); sent an empty response", r.Method, r.RequestURI)
		return
	}
	if d.pending() {
		ref()
		detached = true
		go finishDetached(d)
	}
}

// finishDetached runs what is left of a request's domain after its response.
// Nothing recovers a panic on this goroutine, and the connection it could
// have dropped is already finished, so a panic is reported, not fatal: a
// throw or a rejection as an uncaught one is (D8), and a Go panic with its
// value.
func finishDetached(d *Domain) {
	defer unref()
	defer d.abort()
	defer func() {
		if r := recover(); r != nil {
			if report, ok := failureReport(r, d.running); ok {
				writeErr(report)
				return
			}
			warnf("cakebear: after a response was sent: %v", r)
		}
	}()
	d.finish()
}

// IncomingMessage is node:http's IncomingMessage, as a server sees it.
type IncomingMessage struct {
	r   *http.Request
	url string // requestURL's
}

// URL is req.url: the request target as the client sent it, path and query
// together.
func (m *IncomingMessage) URL() string { return m.url }

// requestURL is req.url for a request target. Node refuses a request line
// holding a raw byte from 0x80 up with a 400; cakebear serves it, each such
// byte read as one U+FFFD (CLAUDE.md, the accepted differences), so req.url is
// always a well-formed string. It was the raw bytes: `ED A0 BD` read as a lone
// surrogate, one code unit where the table promised three, and a stray byte
// was never === to "\uFFFD".
func requestURL(target string) string {
	if isASCII(target) {
		return target
	}
	b := make([]byte, 0, 3*len(target))
	for i := range len(target) {
		if c := target[i]; c < 0x80 {
			b = append(b, c)
		} else {
			b = append(b, "\uFFFD"...)
		}
	}
	return string(b)
}

// Method is req.method.
func (m *IncomingMessage) Method() string { return m.r.Method }

// ServerResponse is node:http's ServerResponse.
type ServerResponse struct {
	w           http.ResponseWriter
	status      int
	headersSent bool
	ended       bool
	d           *Domain // the request's
}

// SetHeader is res.setHeader, replacing any value already set. Go canonicalises
// the name's case on the wire (content-type is sent as Content-Type), where
// Node sends it as written; header names are case-insensitive in HTTP.
//
// Node throws for a name that isn't an HTTP token and for a value holding a
// character it can't send, a lone surrogate or anything past U+00FF among
// them; net/http would drop the first silently and send the second's bytes.
// The value isn't in the message, the name is (R34).
func (r *ServerResponse) SetHeader(name, value string, site *Site) *ServerResponse {
	if r.headersSent {
		throwCoded(ErrorBase, "ERR_HTTP_HEADERS_SENT", "Cannot set headers after they are sent to the client", site)
	}
	if !isHTTPToken(name) {
		throwCoded(ErrorType, "ERR_INVALID_HTTP_TOKEN", `Header name must be a valid HTTP token ["`+name+`"]`, site)
	}
	if !validHeaderValue(value) {
		throwCoded(ErrorType, "ERR_INVALID_CHAR", `Invalid character in header content ["`+name+`"]`, site)
	}
	r.w.Header().Set(name, value)
	return r
}

// WriteHead is res.writeHead(status): it sends the status with the headers set
// so far, after which no more can be set.
func (r *ServerResponse) WriteHead(status float64, site *Site) *ServerResponse {
	if r.headersSent {
		throwCoded(ErrorBase, "ERR_HTTP_HEADERS_SENT", "Cannot write headers after they are sent to the client", site)
	}
	// Node converts the status with `statusCode |= 0`, ToInt32, and checks
	// what that gives: 200.5 sends 200, and so does 2^32 + 200. The message
	// names the status as it was passed, -0 as -0 (measured on Node 24.18).
	code := numToInt32(status)
	if code < 100 || code > 999 {
		throwCoded(ErrorRange, "ERR_HTTP_INVALID_STATUS_CODE", "Invalid status code: "+numberForDisplay(status), site)
	}
	r.status = int(code)
	r.sendHeaders()
	return r
}

// End is res.end(chunk?): it sends the headers if they have not gone, then the
// chunk if there is one, and finishes the response. A second End with nothing
// to write does nothing, as in Node; one with a chunk is a write after the end.
// The chunk goes out as Node encodes it, in UTF-8 with a lone surrogate as
// U+FFFD.
func (r *ServerResponse) End(chunk ...string) *ServerResponse {
	if r.ended {
		if len(chunk) > 0 {
			r.writeAfterEnd()
		}
		return r
	}
	r.sendHeaders()
	for _, c := range chunk {
		_, _ = io.WriteString(r.w, wellFormed(c))
	}
	r.ended = true
	return r
}

// writeAfterEnd is res.end(chunk) after the response has ended. Node doesn't
// throw there: the response emits 'error' on the next tick, nothing listens,
// and that is an uncaught exception no try around the call can catch, once
// the handler has carried on (R36). So it is raised on the request's next
// tick, worded as Node prints it under --stack-trace-limit=0, with no
// excerpt: Node's points into its own events module, and End takes no site.
// Only the first counts: Node is gone by the second.
func (r *ServerResponse) writeAfterEnd() {
	if r.d.tick != nil {
		return
	}
	e := newCodedError(ErrorBase, "ERR_STREAM_WRITE_AFTER_END", "write after end", nil)
	e.stack = "Error [ERR_STREAM_WRITE_AFTER_END]: write after end\nEmitted 'error' event on ServerResponse instance at:\n"
	r.d.tick = &Thrown{V: e}
}

// sendHeaders commits the status line and headers. net/http still buffers
// them until the handler writes enough body or returns, which is why a short
// response gets a Content-Length, as Node's res.end(chunk) does.
//
// net/http would also sniff a Content-Type from the body when the program set
// none (`text/plain; charset=utf-8`, or `text/html` for markup), where Node
// sends no Content-Type at all. A nil value for the key is how net/http is
// told not to.
func (r *ServerResponse) sendHeaders() {
	if !r.headersSent {
		if _, set := r.w.Header()["Content-Type"]; !set {
			r.w.Header()["Content-Type"] = nil
		}
		r.w.WriteHeader(r.status)
		r.headersSent = true
	}
}
