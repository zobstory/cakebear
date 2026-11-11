package runtime

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Response is fetch's Response (R1, R8): data fields, the headers, and the
// body, which a goroutine reads in full after the headers settle the fetch.
// Its fields are touched only by its domain's driver: the reading goroutine
// posts what it read as I/O.
type Response struct {
	d          *Domain
	status     float64
	statusText string
	url        string
	redirected bool
	headers    *Headers

	resp     *http.Response
	body     []byte
	bodyErr  *Error // the body failed after the headers came
	bodyDone bool
	bodyUsed bool
	// readers wait for the body, each settled when it has all arrived.
	readers []func()
	// cancel stops the request, its body too, and waiting is the body's
	// wait under init's signal, if it had one.
	cancel  func()
	waiting *abortWait
}

// abort is the signal's abort while the body is still coming: every read
// waiting, and any made after, rejects with the reason (R16), and what the
// body's goroutine posts later is ignored.
func (r *Response) abort(reason *Error) {
	if r.bodyDone {
		return
	}
	r.body, r.bodyErr, r.bodyDone = nil, reason, true
	r.cancel()
	readers := r.readers
	r.readers = nil
	for _, settle := range readers {
		r.d.inflight--
		settle()
	}
}

// Status, OK, StatusText, URL, Redirected and Headers are the Response's
// fields, as Node reads them (R1, R2, R8).
func (r *Response) Status() float64    { return r.status }
func (r *Response) OK() bool           { return r.status >= 200 && r.status <= 299 }
func (r *Response) StatusText() string { return r.statusText }
func (r *Response) URL() string        { return r.url }
func (r *Response) Redirected() bool   { return r.redirected }
func (r *Response) Headers() *Headers  { return r.headers }
func (r *Response) BodyUsed() bool     { return r.bodyUsed }
func (r *Response) Type() string       { return "basic" }

// roundTrip sends req and makes the Response from what comes back, or the
// failure Node rejects with: `TypeError: fetch failed` and its cause (R9).
func roundTrip(req *http.Request, input string, site *Site) (*Response, *Error) {
	resp, err := fetchClient().Do(req) //nolint:bodyclose // decodedBody closes it, once the body is read
	if err != nil {
		return nil, fetchFailed(fetchCause(err, req), site)
	}
	r := &Response{
		status:     float64(resp.StatusCode),
		statusText: statusText(resp),
		url:        resp.Request.URL.String(),
		redirected: resp.Request.Response != nil,
		headers:    responseHeaders(resp),
		resp:       resp,
	}
	return r, nil
}

// responseHeaders are resp's headers as Node's fetch reads them: names
// lowercased, repeats joined, transfer-encoding where Go took it out of the
// header (R5), and immutable.
func responseHeaders(resp *http.Response) *Headers {
	h := &Headers{immutable: true}
	add := func(name string, values []string) {
		lower := strings.ToLower(name)
		if lower == "set-cookie" {
			h.setCookie = append(h.setCookie, values...)
		}
		h.entries = append(h.entries, headerEntry{name: lower, lower: lower, value: strings.Join(values, ", ")})
	}
	for name, values := range resp.Header {
		add(name, values)
	}
	if len(resp.TransferEncoding) > 0 && h.find("transfer-encoding") < 0 {
		add("transfer-encoding", resp.TransferEncoding)
	}
	h.entries = h.sorted()
	return h
}

// readBody reads the whole body, decoded as its content-encoding says, and
// posts it to the domain, where each read waiting for it settles.
func (r *Response) readBody() {
	data, err := decodedBody(r.resp)
	d := r.d
	d.postIO(func() {
		if r.bodyDone {
			return // aborted while it read
		}
		r.waiting.release()
		r.cancel()
		r.body, r.bodyDone = data, true
		if err != nil {
			r.bodyErr = NewError(ErrorType, "terminated", nil)
		}
		readers := r.readers
		r.readers = nil
		for _, settle := range readers {
			d.inflight--
			settle()
		}
	})
}

// decodedBody is resp's body, gzip and deflate decoded as Node's fetch
// decodes them (R5), the Transport's own decoding being off.
func decodedBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	var src io.Reader = resp.Body
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "gzip", "x-gzip":
		z, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer z.Close()
		src = z
	case "deflate":
		// HTTP's deflate is zlib's format, but some servers send it raw.
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if z, err := zlib.NewReader(bytes.NewReader(raw)); err == nil {
			defer z.Close()
			return io.ReadAll(z)
		}
		return io.ReadAll(flate.NewReader(bytes.NewReader(raw)))
	}
	return io.ReadAll(src)
}

// Text is `res.text()`: the body as UTF-8, whatever the charset says, a BOM
// dropped (R5). A body read twice rejects (R14). A read whose bytes are in
// settles when the microtask queue drains, before any timer (R19).
func (r *Response) Text(d *Domain, site *Site) *Promise[string] {
	p := &Promise[string]{settlement: settlement{d: d}}
	r.read(d, site, &p.settlement, func() { p.resolve(decodeUTF8(r.body)) })
	return p
}

// JSON is `res.json()`: the body as text() reads it, parsed as JSON.parse
// parses it, an unknown, or a rejection with JSON.parse's own SyntaxError
// (R13), which points into the body (R30).
func (r *Response) JSON(d *Domain, site *Site) *Promise[any] {
	p := &Promise[any]{settlement: settlement{d: d}}
	r.read(d, site, &p.settlement, func() {
		v, err := parseJSON(decodeUTF8(r.body))
		if err != nil {
			p.reject(&Thrown{V: err, Site: err.site})
			return
		}
		p.resolve(v)
	})
	return p
}

// read settles a body read: at once with Node's TypeError for a body used
// already, else when the body is in.
func (r *Response) read(d *Domain, site *Site, s *settlement, done func()) {
	if r.bodyUsed {
		s.reject(&Thrown{V: NewError(ErrorType, "Body is unusable: Body has already been read", site), Site: site})
		return
	}
	r.bodyUsed = true
	settle := func() {
		if r.bodyErr != nil {
			s.reject(&Thrown{V: r.bodyErr, Site: site})
			return
		}
		done()
	}
	if r.bodyDone {
		d.afterDrain = append(d.afterDrain, settle)
		return
	}
	d.inflight++ // a read waiting for its body is pending work
	r.readers = append(r.readers, settle)
}

// decodeUTF8 is the Encoding Standard's UTF-8 decode, as Node's text()
// does it: a BOM dropped, and each maximal ill-formed subpart one U+FFFD
// (measured on Node 24.18: e2 82 is one, ed a0 80 three, c0 af two).
func decodeUTF8(b []byte) string {
	b, _ = bytes.CutPrefix(b, []byte("\xef\xbb\xbf")) // a BOM
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			out.WriteByte(c)
			i++
			continue
		}
		need, lo, hi := 0, byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c == 0xE0:
			need, lo = 2, 0xA0
		case c == 0xED:
			need, hi = 2, 0x9F
		case c >= 0xE1 && c <= 0xEF:
			need = 2
		case c == 0xF0:
			need, lo = 3, 0x90
		case c == 0xF4:
			need, hi = 3, 0x8F
		case c >= 0xF1 && c <= 0xF3:
			need = 3
		default:
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j := i + 1
		for range need {
			if j >= len(b) || b[j] < lo || b[j] > hi {
				break
			}
			lo, hi = 0x80, 0xBF
			j++
		}
		if j-i == need+1 {
			out.Write(b[i:j])
		} else {
			out.WriteRune(utf8.RuneError)
		}
		i = j
	}
	return out.String()
}
