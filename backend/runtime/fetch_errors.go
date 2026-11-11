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
	_ = cause // TODO(s1-errors): an Error's cause, once the runtime holds one
	return NewError(ErrorType, "fetch failed", site)
}

// invalidFetchURL is Node's rejection of an input that isn't a URL (R11).
func invalidFetchURL(input string, site *Site) *Error {
	// TODO(s1-errors): cause `[TypeError: Invalid URL] { code: 'ERR_INVALID_URL', input }`.
	return NewError(ErrorType, "Failed to parse URL from "+input, site)
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

// systemError is one of Node's system errors: its message, then errno, code
// and syscall, and the address it concerns, in the order Node lists them.
func systemError(message string, errno float64, code, syscall, key, value, numKey string, num float64) *Error {
	// TODO(s1-errors): the properties, once an Error holds them.
	_, _, _, _, _, _, _ = errno, code, syscall, key, value, numKey, num
	return NewError(ErrorBase, message, nil)
}
