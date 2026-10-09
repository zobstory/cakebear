package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// helloServer is the M1 acceptance program. It also runs unmodified under
// Node, so it must type-check here with no import beyond node:http.
const helloServer = `import { createServer } from "node:http";

const server = createServer((req, res) => {
  res.setHeader("Content-Type", "text/plain");
  if (req.url === "/health") {
    res.end("ok\n");
  } else {
    res.end("hello from cakebear\n");
  }
});

server.listen(3000, () => {
  console.log("listening on http://localhost:3000");
});
`

// cakec bundles node:http's declarations, so a Node server type-checks with no
// npm install, no @types/node and no reference directive. Strict mode is on,
// so this also proves req and res are typed from createServer's signature
// rather than falling back to an implicit any.
func TestNodeHTTPServerTypeChecks(t *testing.T) {
	t.Parallel()

	code, out := build(t, writeTS(t, "hello.ts", helloServer))
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\n%s", code, exitOK, out)
	}
	if !strings.Contains(out, "type-checks clean") {
		t.Errorf("output = %q, want a clean check", out)
	}
}

// The forms of import a Node program uses all reach the same declarations.
func TestNodeHTTPImportForms(t *testing.T) {
	t.Parallel()

	const body = `((req, res) => { res.end(req.url); }).listen(8080);` + "\n"
	for _, tt := range []struct{ name, src string }{
		{"named from http", `import { createServer } from "http"; createServer` + body},
		{"renamed", `import { createServer as serve } from "node:http"; serve` + body},
		{"namespace node:http", `import * as http from "node:http"; http.createServer` + body},
		{"namespace http", `import * as http from "http"; http.createServer` + body},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if code, out := build(t, writeTS(t, "server.ts", tt.src)); code != exitOK {
				t.Errorf("exit code = %d, want %d\n%s", code, exitOK, out)
			}
		})
	}
}

// The declarations hold only what cakebear's runtime implements, so an API
// it lacks is caught by the type checker at the call site, in TypeScript's
// own words, rather than refused later by lowering.
func TestNodeHTTPUndeclaredAPIIsATypeError(t *testing.T) {
	t.Parallel()

	src := `import { createServer } from "node:http";
createServer((req, res) => { res.flushHeaders(); });
`
	code, out := build(t, writeTS(t, "flush.ts", src))
	if code != exitErrors {
		t.Errorf("exit code = %d, want %d", code, exitErrors)
	}
	for _, want := range []string{"TS2339", "flushHeaders", "ServerResponse"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// A deliberate narrowing from @types/node, which has `string | undefined`
// because IncomingMessage doubles as a client response. A server request
// always has both, and cakebear has no nullable representation yet.
func TestNodeHTTPRequestURLAndMethodAreStrings(t *testing.T) {
	t.Parallel()

	src := `import { createServer } from "node:http";
createServer((req, res) => {
  const path: string = req.url;
  const verb: string = req.method;
  res.end(verb + " " + path);
});
`
	if code, out := build(t, writeTS(t, "narrow.ts", src)); code != exitOK {
		t.Errorf("exit code = %d, want %d\n%s", code, exitOK, out)
	}
}

// The M1 acceptance program, built by cakec and run as a real process, serves
// HTTP: checks 1 to 5 of the M1 plan. Running the binary rather than calling
// the runtime also covers the signal handling and the console flushing.
func TestNodeHTTPHelloServes(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGINT, which a Windows process cannot receive")
	}

	port := freePort(t)
	path := writeTS(t, "hello.ts", strings.ReplaceAll(helloServer, "3000", strconv.Itoa(port)))
	bin := filepath.Join(filepath.Dir(path), "hello")
	var stderr bytes.Buffer
	if code := runBuild(buildOptions{files: []string{path}, output: bin}, &stderr); code != exitOK {
		t.Fatalf("check 1: build failed (%d):\n%s", code, stderr.String())
	}

	server := startServer(t, bin)

	// Check 4: the line arrives while the server is up, not when it exits.
	want := fmt.Sprintf("listening on http://localhost:%d", port)
	select {
	case line := <-server.lines:
		if line != want {
			t.Fatalf("first line = %q, want %q", line, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("check 4: no %q within 10s", want)
	}

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if status, typ, body := get(t, base+"/"); status != 200 || typ != "text/plain" || body != "hello from cakebear\n" {
		t.Errorf("check 2: GET / = %d %q %q", status, typ, body)
	}
	if _, _, body := get(t, base+"/health"); body != "ok\n" {
		t.Errorf("check 3: GET /health body = %q, want %q", body, "ok\n")
	}

	// Check 5: Ctrl-C exits with 130, as Node does.
	if code := server.interrupt(t); code != 130 {
		t.Errorf("check 5: exit code after SIGINT = %d, want 130", code)
	}
}

type runningServer struct {
	cmd   *exec.Cmd
	out   *io.PipeWriter
	lines chan string
	done  bool
}

func startServer(t *testing.T, bin string) *runningServer {
	t.Helper()
	pr, pw := io.Pipe()
	s := &runningServer{cmd: exec.Command(bin), out: pw, lines: make(chan string, 16)}
	s.cmd.Stdout = pw
	if err := s.cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", bin, err)
	}
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
	}()
	t.Cleanup(func() {
		if !s.done {
			_ = s.cmd.Process.Kill()
			_ = s.cmd.Wait()
			_ = pw.Close()
		}
	})
	return s
}

// interrupt sends SIGINT and returns the exit code.
func (s *runningServer) interrupt(t *testing.T) int {
	t.Helper()
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("sending SIGINT: %v", err)
	}
	err := s.cmd.Wait()
	s.done = true
	_ = s.out.Close()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("after SIGINT, Wait = %v, want an exit error", err)
	}
	return exitErr.ExitCode()
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func get(t *testing.T, url string) (status int, contentType, body string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", url, err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
}
