package backend

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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zobstory/cakebear/ir"
)

// optionalServer is a server whose every request makes an object, writes an
// optional property it lacks, deletes it and writes it back, with another
// between them for /b, and replies with the object's JSON. Node 24.18
// replied {"path":"/a","tag":"t","seen":3} to /a, {"path":"/b","seen":3,"tag":"t"}
// to /b, and to any other path as to /a.
//
//	import { createServer } from "node:http";
//	interface Reply { path: string; seen?: number; tag?: string }
//	createServer((req, res) => {
//	  const r: Reply = { path: req.url };
//	  r.seen = 1;
//	  delete r.seen;
//	  if (req.url === "/b") {
//	    r.seen = 2;
//	  }
//	  r.tag = "t";
//	  r.seen = 3;
//	  res.setHeader("Content-Type", "application/json");
//	  res.end(JSON.stringify(r));
//	}).listen(3000, () => {
//	  console.log("listening");
//	});
func optionalServer(shapes *ir.Shapes, port int) *ir.Module {
	numM := ir.MaybeOf(ir.Number)
	s := shapes.Intern("Reply", []ir.Field{{Name: "path", Type: ir.String}, {Name: "seen", Type: numM, Optional: true}, {Name: "tag", Type: ir.MaybeOf(ir.String), Optional: true}})
	server, req, res := ir.HostOf(ir.HostHTTPServer), ir.HostOf(ir.HostIncomingMessage), ir.HostOf(ir.HostServerResponse)
	url := &ir.HostProp{Prop: ir.HostURL, Recv: stable("req", req), Typ: ir.String, Span: span}
	r := stable("r", ir.ObjectOf(s))
	handler := &ir.FuncLit{
		Params: []ir.Param{{Name: "req", Type: req, Span: span}, {Name: "res", Type: res, Span: span}},
		Result: ir.Void, Typ: ir.FuncType([]ir.Type{req, res}, ir.Void), Concurrent: true, Span: span,
		Body: []ir.Stmt{
			decl("r", ir.ObjectOf(s), lit(s, "path", url)),
			set(r, "seen", some(num(1))),
			del(r, "seen"),
			&ir.If{Cond: binary(ir.OpEqual, url, str("/b"), ir.Boolean), Then: []ir.Stmt{set(r, "seen", some(num(2)))}, Span: span},
			set(r, "tag", some(str("t"))),
			set(r, "seen", some(num(3))),
			do(&ir.HostCall{Op: ir.HostSetHeader, Recv: stable("res", res), Args: []ir.Expr{str("Content-Type"), str("application/json")}, Typ: res, Span: span}),
			do(&ir.HostCall{Op: ir.HostEnd, Recv: stable("res", res), Args: []ir.Expr{jsonOf(r)}, Typ: res, Span: span}),
		},
	}
	created := &ir.HostCall{Op: ir.HostCreateServer, Args: []ir.Expr{handler}, Typ: server, Span: span}
	return &ir.Module{Name: "t", Main: []ir.Stmt{do(&ir.HostCall{
		Op: ir.HostListen, Recv: created, Typ: server, Span: span,
		Args: []ir.Expr{num(float64(port)), arrow(nil, ir.Void, logStr("listening"))},
	})}}
}

// TestOptionalPropertiesServeUnderRace (M5 plan risk 5): key tables are
// package-level, so every request's goroutine steps from the same ones, and
// the first requests take each step at once. Built with Go's race detector,
// the server answers 200 requests, 8 at a time, each as Node answered it,
// and the detector reports nothing.
func TestOptionalPropertiesServeUnderRace(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("stops the server with SIGINT, which a Windows process cannot receive")
	}
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}

	port, cmd, stderr, exited := serveOptional(t)

	replies := map[string]string{
		"/a": `{"path":"/a","tag":"t","seen":3}`,
		"/b": `{"path":"/b","seen":3,"tag":"t"}`,
	}
	client := &http.Client{Timeout: 30 * time.Second}
	errs := make(chan error, 200)
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Go(func() {
			for i := range 25 {
				path := []string{"/a", "/b", fmt.Sprintf("/r%d", w*25+i)}[i%3]
				want, ok := replies[path]
				if !ok {
					want = `{"path":"` + path + `","tag":"t","seen":3}`
				}
				resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
				if err != nil {
					errs <- err
					continue
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				switch {
				case err != nil:
					errs <- fmt.Errorf("GET %s: reading the body: %w", path, err)
				case resp.StatusCode != http.StatusOK || string(body) != want:
					errs <- fmt.Errorf("GET %s = %d %q, want 200 %q", path, resp.StatusCode, body, want)
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	select {
	case err := <-exited:
		if !errors.As(err, &exit) || exit.ExitCode() != 130 {
			t.Errorf("exit after SIGINT: %v, want status 130 (the race detector exits 66 when it found a race)", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the server was still running 30s after SIGINT")
	}
	if strings.Contains(stderr.String(), "DATA RACE") {
		t.Errorf("the race detector found a race:\n%s", stderr.String())
	}
}

// serveOptional builds optionalServer with the race detector and starts it,
// listening. The port is chosen before the build, which takes seconds, and
// another test's server can take it meanwhile: a server that finds it in use
// is built again on another, up to three times. The port is probed on every
// interface, as the server listens, since one free on 127.0.0.1 alone may be
// taken on the rest.
func serveOptional(t *testing.T) (int, *exec.Cmd, *bytes.Buffer, chan error) {
	t.Helper()
	for attempt := 1; ; attempt++ {
		ln, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		var shapes ir.Shapes
		bin := filepath.Join(t.TempDir(), "server")
		if src, err := Build(withShapes(optionalServer(&shapes, port), &shapes), Options{Output: bin, Race: true}); err != nil {
			t.Fatalf("Build: %v\n%s", err, src)
		}
		// A clean run proves nothing unless the detector was on: Go records it
		// in the binary's build settings.
		if info, err := exec.Command("go", "version", "-m", bin).Output(); err != nil || !strings.Contains(string(info), "-race=true") {
			t.Fatalf("the server was not built with the race detector (go version -m: %v):\n%s", err, info)
		}

		cmd := exec.Command(bin)
		stderr := &bytes.Buffer{}
		cmd.Stderr = stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan error, 1)
		listening := make(chan bool, 1)
		go func() {
			sc := bufio.NewScanner(stdout)
			for sc.Scan() {
				if sc.Text() == "listening" {
					listening <- true
				}
			}
			_, _ = io.Copy(io.Discard, stdout)
			exited <- cmd.Wait()
		}()
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		select {
		case <-listening:
			return port, cmd, stderr, exited
		case err := <-exited:
			if strings.Contains(stderr.String(), "EADDRINUSE") && attempt < 3 {
				t.Logf("port %d was taken before the server listened; building again on another", port)
				continue
			}
			t.Fatalf("the server exited before listening: %v\n%s", err, stderr.String())
		case <-time.After(30 * time.Second):
			t.Fatalf("the server wasn't listening after 30s\n%s", stderr.String())
		}
	}
}
