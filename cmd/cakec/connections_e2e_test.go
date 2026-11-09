package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// connections is what Node 24.18 sent for framingServer's paths over HTTP/1.0,
// asked to keep the connection or not, and over HTTP/1.1 kept alive, read off
// the socket as exchange reads it: the status, Content-Length,
// Transfer-Encoding and Connection ("" for none), whether the connection
// closed after it, and the body as it went.
//
//   - HTTP/1.0 has no chunks, so unless the program set a length the body
//     runs to the connection's end, before writeHead too, which closes;
//   - an HTTP/1.0 connection is kept only for a length the program set and a
//     request asking for it, a HEAD's or a 204's too;
//   - over HTTP/1.1 every connection is kept, framed as framings says.
var connections = func() []connection {
	x := strings.Repeat("x", 5000)
	return []connection{
		{"1.0", "GET", "/end-small", 200, "", "", "close", true, "ok\n"},
		{"1.0", "GET", "/end-big", 200, "", "", "close", true, x},
		{"1.0", "GET", "/end-empty", 200, "", "", "close", true, ""},
		{"1.0", "GET", "/head-small", 200, "", "", "close", true, "ok\n"},
		{"1.0", "GET", "/head-empty", 200, "", "", "close", true, ""},
		{"1.0", "GET", "/head-204", 204, "", "", "close", true, ""},
		{"1.0", "GET", "/own-length", 200, "3", "", "close", true, "ok\n"},
		{"1.0", "GET", "/own-length-head", 200, "3", "", "close", true, "ok\n"},
		{"1.0", "HEAD", "/end-small", 200, "", "", "close", true, ""},
		{"1.0", "HEAD", "/end-big", 200, "", "", "close", true, ""},
		{"1.0", "HEAD", "/end-empty", 200, "", "", "close", true, ""},
		{"1.0", "HEAD", "/head-small", 200, "", "", "close", true, ""},
		{"1.0", "HEAD", "/head-empty", 200, "", "", "close", true, ""},
		{"1.0", "HEAD", "/head-204", 204, "", "", "close", true, ""},
		{"1.0", "HEAD", "/own-length", 200, "3", "", "close", true, ""},
		{"1.0", "HEAD", "/own-length-head", 200, "3", "", "close", true, ""},
		{"1.0 keep-alive", "GET", "/end-small", 200, "", "", "close", true, "ok\n"},
		{"1.0 keep-alive", "GET", "/end-big", 200, "", "", "close", true, x},
		{"1.0 keep-alive", "GET", "/end-empty", 200, "", "", "close", true, ""},
		{"1.0 keep-alive", "GET", "/head-small", 200, "", "", "close", true, "ok\n"},
		{"1.0 keep-alive", "GET", "/head-empty", 200, "", "", "close", true, ""},
		{"1.0 keep-alive", "GET", "/head-204", 204, "", "", "close", true, ""},
		{"1.0 keep-alive", "GET", "/own-length", 200, "3", "", "keep-alive", false, "ok\n"},
		{"1.0 keep-alive", "GET", "/own-length-head", 200, "3", "", "keep-alive", false, "ok\n"},
		{"1.0 keep-alive", "HEAD", "/end-small", 200, "", "", "close", true, ""},
		{"1.0 keep-alive", "HEAD", "/end-big", 200, "", "", "close", true, ""},
		{"1.0 keep-alive", "HEAD", "/end-empty", 200, "", "", "close", true, ""},
		{"1.0 keep-alive", "HEAD", "/head-small", 200, "", "", "close", true, ""},
		{"1.0 keep-alive", "HEAD", "/head-empty", 200, "", "", "close", true, ""},
		{"1.0 keep-alive", "HEAD", "/head-204", 204, "", "", "close", true, ""},
		{"1.0 keep-alive", "HEAD", "/own-length", 200, "3", "", "keep-alive", false, ""},
		{"1.0 keep-alive", "HEAD", "/own-length-head", 200, "3", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "GET", "/end-small", 200, "3", "", "keep-alive", false, "ok\n"},
		{"1.1 keep-alive", "GET", "/end-big", 200, "5000", "", "keep-alive", false, x},
		{"1.1 keep-alive", "GET", "/end-empty", 200, "0", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "GET", "/head-small", 200, "", "chunked", "keep-alive", false, "3\r\nok\n\r\n0\r\n\r\n"},
		{"1.1 keep-alive", "GET", "/head-empty", 200, "", "chunked", "keep-alive", false, "0\r\n\r\n"},
		{"1.1 keep-alive", "GET", "/head-204", 204, "", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "GET", "/own-length", 200, "3", "", "keep-alive", false, "ok\n"},
		{"1.1 keep-alive", "GET", "/own-length-head", 200, "3", "", "keep-alive", false, "ok\n"},
		{"1.1 keep-alive", "HEAD", "/end-small", 200, "", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "HEAD", "/end-big", 200, "", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "HEAD", "/end-empty", 200, "", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "HEAD", "/head-small", 200, "", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "HEAD", "/head-empty", 200, "", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "HEAD", "/head-204", 204, "", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "HEAD", "/own-length", 200, "3", "", "keep-alive", false, ""},
		{"1.1 keep-alive", "HEAD", "/own-length-head", 200, "3", "", "keep-alive", false, ""},
	}
}()

type connection struct {
	proto, method, path string
	status              int
	length, te, conn    string
	closed              bool
	body                string
}

// connectionRequests ends each kind's request line, with its headers.
var connectionRequests = map[string]string{
	"1.0":            " HTTP/1.0\r\n\r\n",
	"1.0 keep-alive": " HTTP/1.0\r\nConnection: keep-alive\r\n\r\n",
	"1.1 keep-alive": " HTTP/1.1\r\nHost: x\r\n\r\n",
}

// TestConnectionsMatchNode: over HTTP/1.0 and over a kept HTTP/1.1
// connection, each response is framed as Node frames it, and its connection
// is kept where Node keeps it. Before, net/http sent a length wherever it
// could and kept every HTTP/1.0 connection asked to, so ab -k, which asks,
// measured cakebear on one connection against Node on one per request: 33 of
// these 48 differed, and 28 with the framing as TestResponseFramingMatchesNode
// first held it, over HTTP/1.1 alone.
//
// Over HTTP/1.1 Node also sends `Connection: keep-alive`, with a Keep-Alive
// timeout, where net/http leaves it implicit: the keep-alive headers and
// timeouts aren't compared yet, so there the header isn't held to Node's.
func TestConnectionsMatchNode(t *testing.T) {
	t.Parallel()

	bin, port := buildServer(t, framingServer, false)
	server := startServer(t, bin)
	server.waitForLine(t, "listening", 30*time.Second)
	for _, want := range connections {
		got, err := exchange(port, want.method+" "+want.path+connectionRequests[want.proto], want.method == "HEAD")
		if err != nil {
			t.Fatalf("%s %s over %s: %v", want.method, want.path, want.proto, err)
		}
		connOK := got.conn == want.conn || want.proto == "1.1 keep-alive"
		if got.status != want.status || got.length != want.length || got.te != want.te || !connOK ||
			got.closed != want.closed || got.body != want.body {
			t.Errorf("%s %s over %s = %d, Content-Length %q, Transfer-Encoding %q, Connection %q, closed %v, body %q\n"+
				"want, as Node sent, %d, %q, %q, %q, %v, %q",
				want.method, want.path, want.proto, got.status, got.length, got.te, got.conn, got.closed, abbreviate(got.body),
				want.status, want.length, want.te, want.conn, want.closed, abbreviate(want.body))
		}
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
}

// exchange sends req on a connection of its own and reads one response as its
// headers frame it: a Content-Length's bytes, chunks to the last, or all there
// is. Then it asks again on the same connection, to see whether the server
// kept it: an answer says it did, and the stream's end that it didn't.
func exchange(port int, req string, head bool) (connection, error) {
	var got connection
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second)
	if err != nil {
		return got, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, req); err != nil {
		return got, err
	}
	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil {
		return got, err
	}
	if fields := strings.Fields(line); len(fields) > 1 {
		got.status, _ = strconv.Atoi(fields[1])
	}
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return got, err
		}
		if l = strings.TrimRight(l, "\r\n"); l == "" {
			break
		}
		name, value, _ := strings.Cut(l, ":")
		switch value = strings.TrimSpace(value); strings.ToLower(name) {
		case "content-length":
			got.length = value
		case "transfer-encoding":
			got.te = value
		case "connection":
			got.conn = value
		}
	}
	var body []byte
	switch {
	case head || got.status == 204 || got.status == 304:
	case got.length != "":
		n, _ := strconv.Atoi(got.length)
		body = make([]byte, n)
		_, err = io.ReadFull(r, body)
	case got.te == "chunked":
		var chunks []byte
		for err == nil && !bytes.HasSuffix(chunks, []byte("0\r\n\r\n")) {
			var b byte
			if b, err = r.ReadByte(); err == nil {
				chunks = append(chunks, b)
			}
		}
		body = chunks
	default:
		body, err = io.ReadAll(r)
		got.closed = true
	}
	if err != nil {
		return got, err
	}
	got.body = string(body)
	if !got.closed {
		_, err := io.WriteString(conn, "GET /end-small HTTP/1.1\r\nHost: x\r\n\r\n")
		if err == nil {
			line, err = r.ReadString('\n')
		}
		got.closed = err != nil || !strings.HasPrefix(line, "HTTP/")
	}
	return got, nil
}
