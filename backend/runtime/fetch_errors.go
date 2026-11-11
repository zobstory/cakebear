package runtime

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"syscall"
)

// fetch's failures, as Node's fetch rejects with them (R7, R9, R11, R15).

// withRedirectMode carries init's redirect to checkRedirect.
func withRedirectMode(mode string) context.Context {
	return context.WithValue(context.Background(), redirectMode{}, mode)
}

// fetchFailed is `TypeError: fetch failed`, whose cause says why (R9). It
// has no frames even without --stack-trace-limit=0, and none here.
func fetchFailed(cause *Error, site *Site) *Error {
	return NewErrorWith(ErrorType, "fetch failed", ErrorDetails{Cause: cause, HasCause: true}, site)
}

// invalidFetchURL is Node's rejection of an input that isn't a URL (R11),
// whose cause is the URL parser's TypeError. Its code is a property, not
// the bracketed code of Node's own errors: it comes from Node's C++
// (s1-errors, measured).
func invalidFetchURL(input string, site *Site) *Error {
	cause := NewErrorWith(ErrorType, "Invalid URL", ErrorDetails{Props: []ErrorProp{
		StringProp("code", "ERR_INVALID_URL"), StringProp("input", input),
	}}, nil)
	return NewErrorWith(ErrorType, "Failed to parse URL from "+input, ErrorDetails{Cause: cause, HasCause: true}, site)
}

// fetchCause is the cause Node gives a failure Go's client returned: its
// system error for the ones measured, and Go's own words for the rest, an
// accepted difference (S1 plan D9).
func fetchCause(err error, req *http.Request) *Error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	switch {
	case errors.Is(err, errRedirectCount):
		return NewError(ErrorBase, "redirect count exceeded", nil)
	case errors.Is(err, errUnexpectedRedirect):
		return NewError(ErrorBase, "unexpected redirect", nil)
	}
	var dns *net.DNSError
	if errors.As(err, &dns) && dns.IsNotFound {
		// errno -3008 is libuv's UV_EAI_NONAME, on every platform.
		return systemError("getaddrinfo ENOTFOUND "+dns.Name, -3008, "ENOTFOUND", "getaddrinfo", "hostname", dns.Name, "", 0)
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" && errors.Is(err, syscall.ECONNREFUSED) {
		host, port := req.URL.Hostname(), req.URL.Port()
		if a, ok := op.Addr.(*net.TCPAddr); ok {
			host, port = a.IP.String(), strconv.Itoa(a.Port)
		}
		n, _ := strconv.Atoi(port)
		return systemError("connect ECONNREFUSED "+net.JoinHostPort(host, port), -float64(syscall.ECONNREFUSED), "ECONNREFUSED", "connect", "address", host, "port", float64(n))
	}
	return NewError(ErrorBase, err.Error(), nil)
}

// systemError is one of Node's system errors, as its uvException makes
// them: the message, then errno, code and syscall, and what it concerns, a
// host's name or an address and a port, in the order Node lists them (R9).
func systemError(message string, errno float64, code, syscall, key, value, numKey string, num float64) *Error {
	props := []ErrorProp{NumberProp("errno", errno), StringProp("code", code), StringProp("syscall", syscall), StringProp(key, value)}
	if numKey != "" {
		props = append(props, NumberProp(numKey, num))
	}
	return NewErrorWith(ErrorBase, message, ErrorDetails{Props: props}, nil)
}
