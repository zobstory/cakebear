package runtime

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// TestRequestURLReadsRawBytesAsReplacements: a request line holding raw bytes
// from 0x80 up is served, each byte one U+FFFD in req.url, as the accepted
// differences promise (Node answers 400). It was the bytes as sent, so
// `ED A0 BD` read as a lone surrogate, one code unit, and the two bytes of a
// raw `é` as one character. The answer is checked inside the handler: on its
// way out, res.end writes a lone surrogate as U+FFFD anyway.
func TestRequestURLReadsRawBytesAsReplacements(t *testing.T) { //nolint:paralleltest // isolate swaps process-wide runtime state
	isolate(t)
	const want = "/a���?q=��"
	url := serveWith(t, func(req *IncomingMessage, res *ServerResponse) {
		u := req.URL()
		res.End(NumberToString(StrLength(u)) + " " + BoolToString(u == want))
	})
	conn, err := net.Dial("tcp", strings.TrimPrefix(url, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET /a\xed\xa0\xbd?q=\xc3\xa9 HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "10 true" {
		t.Errorf("got %d %q, want 200 \"10 true\": req.url's length, and whether it is %q", resp.StatusCode, body, want)
	}
	if got := requestURL("/plain?x=%C3%A9"); got != "/plain?x=%C3%A9" {
		t.Errorf("an ASCII target became %q", got)
	}
}
