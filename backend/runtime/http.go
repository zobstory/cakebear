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
//	createServer(listener)       CreateServer(func(*IncomingMessage, *ServerResponse))
//	server.listen(port, cb?)     (*Server).Listen(float64, ...func())
//	res.setHeader(name, value)   (*ServerResponse).SetHeader(string, string)
//	res.writeHead(status)        (*ServerResponse).WriteHead(float64)
//	res.end(chunk?)              (*ServerResponse).End(...string)
//	req.url, req.method          (*IncomingMessage).URL(), .Method()
//
// Methods that return `this` in TypeScript return their receiver, so a chained
// call emits as written. The optional arguments are variadic so a call emits
// with the arguments the source gave; lower/ has already checked the arity.
//
// Each request is handled on the goroutine net/http gives it, so handlers run
// genuinely in parallel. Where Node would throw (headers set after they were
// sent, a status code out of range) the runtime panics with Node's message.
// net/http recovers a handler's panic and drops only that connection, so one
// bad request cannot take the server down.

// listenHost is the address servers bind: every interface, as Node's default
// is. Tests narrow it to loopback.
var listenHost = ""

// Server is node:http's Server.
type Server struct {
	listener func(*IncomingMessage, *ServerResponse)
	ln       net.Listener
	srv      *http.Server
}

// CreateServer is node:http's createServer.
func CreateServer(listener func(*IncomingMessage, *ServerResponse)) *Server {
	return &Server{listener: listener}
}

// Listen is server.listen(port, callback?).
//
// The port is bound before Listen returns, so a port already in use is
// reported straight away, with the exit code 1 Node's EADDRINUSE crash gives.
// Serving and the callback wait until the program's top-level code is done:
// both are queued for Run, the callback first, so "listening on …" is printed
// before any handler runs, as under Node.
func (s *Server) Listen(port float64, callbacks ...func()) *Server {
	p, ok := portNumber(port)
	if !ok {
		fatal("RangeError [ERR_SOCKET_BAD_PORT]: port should be >= 0 and < 65536. Received %s.", NumberToString(port))
		return s
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(listenHost, strconv.Itoa(p)))
	if err != nil {
		host := listenHost
		if host == "" {
			host = "::"
		}
		if errors.Is(err, syscall.EADDRINUSE) {
			fatal("Error: listen EADDRINUSE: address already in use %s:%d", host, p)
		} else {
			fatal("Error: listen %s:%d: %v", host, p, err)
		}
		return s
	}

	s.ln = ln
	s.srv = &http.Server{Handler: http.HandlerFunc(s.serve)}
	ref()
	for _, cb := range callbacks {
		if cb != nil {
			enqueue(cb)
		}
	}
	enqueue(func() { go s.run() })
	return s
}

func (s *Server) run() {
	err := s.srv.Serve(s.ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("Error: %v", err)
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

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	res := &ServerResponse{w: w, status: http.StatusOK}
	s.listener(&IncomingMessage{r: r}, res)
	if !res.ended {
		// Node would leave the request open until it timed out. net/http
		// finishes it instead; say so rather than hide the bug.
		warn("cakebear: the handler for %s %s returned without calling res.end(); sent an empty response", r.Method, r.RequestURI)
	}
}

// IncomingMessage is node:http's IncomingMessage, as a server sees it.
type IncomingMessage struct{ r *http.Request }

// URL is req.url: the request target exactly as the client sent it, path and
// query together.
func (m *IncomingMessage) URL() string { return m.r.RequestURI }

// Method is req.method.
func (m *IncomingMessage) Method() string { return m.r.Method }

// ServerResponse is node:http's ServerResponse.
type ServerResponse struct {
	w           http.ResponseWriter
	status      int
	headersSent bool
	ended       bool
}

// SetHeader is res.setHeader, replacing any value already set. Go canonicalises
// the name's case on the wire (content-type is sent as Content-Type), where
// Node sends it as written; header names are case-insensitive in HTTP.
func (r *ServerResponse) SetHeader(name, value string) *ServerResponse {
	if r.headersSent {
		panic("Error [ERR_HTTP_HEADERS_SENT]: Cannot set headers after they are sent to the client")
	}
	r.w.Header().Set(name, value)
	return r
}

// WriteHead is res.writeHead(status): it sends the status with the headers set
// so far, after which no more can be set.
func (r *ServerResponse) WriteHead(status float64) *ServerResponse {
	if r.headersSent {
		panic("Error [ERR_HTTP_HEADERS_SENT]: Cannot write headers after they are sent to the client")
	}
	if status != math.Trunc(status) || status < 100 || status > 999 {
		panic("RangeError [ERR_HTTP_INVALID_STATUS_CODE]: Invalid status code: " + NumberToString(status))
	}
	r.status = int(status)
	r.sendHeaders()
	return r
}

// End is res.end(chunk?): it sends the headers if they have not gone, then the
// chunk if there is one, and finishes the response. A second End with nothing
// to write does nothing, as in Node; one with a chunk is a write after the end.
func (r *ServerResponse) End(chunk ...string) *ServerResponse {
	if r.ended {
		if len(chunk) > 0 {
			panic("Error [ERR_STREAM_WRITE_AFTER_END]: write after end")
		}
		return r
	}
	r.sendHeaders()
	for _, c := range chunk {
		_, _ = io.WriteString(r.w, c)
	}
	r.ended = true
	return r
}

// sendHeaders commits the status line and headers. net/http still buffers
// them until the handler writes enough body or returns, which is why a short
// response gets a Content-Length, as Node's res.end(chunk) does.
func (r *ServerResponse) sendHeaders() {
	if !r.headersSent {
		r.w.WriteHeader(r.status)
		r.headersSent = true
	}
}
