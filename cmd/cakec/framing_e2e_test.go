package main

import (
	"bytes"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// framingServer answers each path with a different res.end, before and after
// writeHead, so the response's framing is Node's in each case.
const framingServer = `import { createServer } from "node:http";
const big: string = "x".repeat(5000);
createServer((req, res) => {
  const url: string = req.url;
  if (url === "/end-big") {
    res.end(big);
  } else if (url === "/end-small") {
    res.end("ok\n");
  } else if (url === "/end-empty") {
    res.end();
  } else if (url === "/end-empty-string") {
    res.end("");
  } else if (url === "/head-big") {
    res.writeHead(200);
    res.end(big);
  } else if (url === "/head-small") {
    res.writeHead(200);
    res.end("ok\n");
  } else if (url === "/head-empty") {
    res.writeHead(200);
    res.end();
  } else if (url === "/head-204") {
    res.writeHead(204);
    res.end();
  } else if (url === "/head-304") {
    res.writeHead(304);
    res.end();
  } else if (url === "/own-length") {
    res.setHeader("Content-Length", "3");
    res.end("ok\n");
  } else if (url === "/own-length-head") {
    res.setHeader("Content-Length", "3");
    res.writeHead(200);
    res.end("ok\n");
  } else if (url === "/surrogate") {
    res.end("a\uD800b");
  } else if (url === "/emoji") {
    res.end("😀");
  } else {
    res.writeHead(404);
    res.end("no\n");
  }
}).listen(3000, () => {
  console.log("listening");
});
`

// framings is what Node 24.18 sent for framingServer's paths, GET and HEAD,
// read off the socket: the status, Content-Length and Transfer-Encoding ("" for
// none) and the body's bytes as they went, chunk framing included.
//
//   - res.end(body) before writeHead: a Content-Length, 0 for no body;
//   - after writeHead: chunked, an empty body too, the body in one chunk;
//   - 204 and 304: neither;
//   - a Content-Length the program set: kept;
//   - HEAD: neither, but a program's own, and no body.
var framings = func() []framing {
	x := strings.Repeat("x", 5000)
	get := []framing{
		{"GET", "/end-big", 200, "5000", "", x},
		{"GET", "/end-small", 200, "3", "", "ok\n"},
		{"GET", "/end-empty", 200, "0", "", ""},
		{"GET", "/end-empty-string", 200, "0", "", ""},
		{"GET", "/head-big", 200, "", "chunked", "1388\r\n" + x + "\r\n0\r\n\r\n"},
		{"GET", "/head-small", 200, "", "chunked", "3\r\nok\n\r\n0\r\n\r\n"},
		{"GET", "/head-empty", 200, "", "chunked", "0\r\n\r\n"},
		{"GET", "/head-204", 204, "", "", ""},
		{"GET", "/head-304", 304, "", "", ""},
		{"GET", "/own-length", 200, "3", "", "ok\n"},
		{"GET", "/own-length-head", 200, "3", "", "ok\n"},
		{"GET", "/surrogate", 200, "5", "", "a�b"},
		{"GET", "/emoji", 200, "4", "", "😀"},
		{"GET", "/nope", 404, "", "chunked", "3\r\nno\n\r\n0\r\n\r\n"},
	}
	out := append([]framing{}, get...)
	for _, f := range get {
		f.method, f.te, f.body = "HEAD", "", ""
		if !strings.HasPrefix(f.path, "/own-length") {
			f.length = ""
		}
		out = append(out, f)
	}
	return out
}()

type framing struct {
	method, path string
	status       int
	length, te   string
	body         string
}

// TestResponseFramingMatchesNode: each response is framed as Node frames it.
// Before this was compared, a body over net/http's 2 KB buffer went out
// chunked where Node sends a Content-Length, in 4 KB chunks where Node sends
// one, a small one after writeHead with a Content-Length where Node chunks,
// and a HEAD with a Content-Length Node doesn't send: 10 of these 28.
func TestResponseFramingMatchesNode(t *testing.T) {
	t.Parallel()

	bin, port := buildServer(t, framingServer, false)
	server := startServer(t, bin)
	server.waitForLine(t, "listening", 30*time.Second)
	for _, want := range framings {
		status, length, te, body, err := rawRequest(port, want.method, want.path)
		if err != nil {
			t.Fatalf("%s %s: %v", want.method, want.path, err)
		}
		if status != want.status || length != want.length || te != want.te || body != want.body {
			t.Errorf("%s %s = %d, Content-Length %q, Transfer-Encoding %q, body %q\nwant, as Node sent, %d, %q, %q, %q",
				want.method, want.path, status, length, te, abbreviate(body), want.status, want.length, want.te, abbreviate(want.body))
		}
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
}

// rawRequest sends one HTTP/1.1 request on a connection of its own and reads
// the response as it came, so chunk framing stays in the body.
func rawRequest(port int, method, path string) (status int, length, te, body string, err error) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second)
	if err != nil {
		return 0, "", "", "", err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, method+" "+path+" HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"); err != nil {
		return 0, "", "", "", err
	}
	raw, err := io.ReadAll(conn)
	if err != nil {
		return 0, "", "", "", err
	}
	head, rest, ok := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !ok {
		return 0, "", "", "", io.ErrUnexpectedEOF
	}
	lines := strings.Split(string(head), "\r\n")
	if fields := strings.Fields(lines[0]); len(fields) > 1 {
		status, _ = strconv.Atoi(fields[1])
	}
	for _, l := range lines[1:] {
		name, value, _ := strings.Cut(l, ":")
		switch strings.ToLower(name) {
		case "content-length":
			length = strings.TrimSpace(value)
		case "transfer-encoding":
			te = strings.TrimSpace(value)
		}
	}
	return status, length, te, string(rest), nil
}

// abbreviate is s for a message, its middle elided when it is long.
func abbreviate(s string) string {
	if len(s) <= 40 {
		return s
	}
	return s[:20] + "…" + s[len(s)-20:]
}
