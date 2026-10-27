package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// M1 plan checks 6, 7 and 8, each against a cakec-built binary run as a real
// process: the runtime's own tests cover the pieces, these cover the program.

// buildServer compiles src, with every 3000 in it replaced by a free port, and
// returns the binary and the port.
func buildServer(t *testing.T, src string, race bool) (string, int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stops the server with SIGINT, which a Windows process cannot receive")
	}
	port := freePort(t)
	path := writeTS(t, "server.ts", strings.ReplaceAll(src, "3000", strconv.Itoa(port)))
	bin := filepath.Join(filepath.Dir(path), "server")
	var stderr bytes.Buffer
	if code := runBuild(buildOptions{files: []string{path}, output: bin, race: race}, &stderr); code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, stderr.String())
	}
	return bin, port
}

// Check 6: a port already in use is reported straight away, with Node's
// message and exit code 1, rather than as a server that never answers.
func TestHTTPPortInUse(t *testing.T) {
	t.Parallel()

	bin, port := buildServer(t, helloServer, false)
	hold, err := net.Listen("tcp", ":"+strconv.Itoa(port)) // every interface, as the server binds
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Close() }()

	server := startServer(t, bin)
	if code := server.exited(t, 30*time.Second); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := fmt.Sprintf("Error: listen EADDRINUSE: address already in use :::%d", port); !strings.Contains(server.stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", server.stderr.String(), want)
	}
}

const fibServer = `import { createServer } from "node:http";

function fib(n: number): number {
  if (n < 2) {
    return n;
  }
  return fib(n - 1) + fib(n - 2);
}

createServer((req, res) => {
  const result: number = fib(38);
  if (result > 0) {
    res.end("done\n");
  }
}).listen(3000, () => {
  console.log("listening");
});
`

// Check 7: handlers run in parallel. Eight requests to a CPU-heavy handler
// overlap (checkParallel's limit: 4.5x with eight cores or more, 5x with
// four). Node takes about 7.7x for the same program,
// because its handlers share one thread; hand-written Go measured 1.8-2.2x on
// the 12-core Mac the plan was written on.
func TestHTTPHandlersRunInParallel(t *testing.T) { //nolint:paralleltest // the timing must not share the CPU with parallel tests
	checkParallel(t, fibServer, "fib(38), Node: ~7.7x")
}

// checkParallel builds a server whose handler is CPU-heavy and asserts that
// eight concurrent requests overlap: they take less than halfway from what
// the machine's cores allow to the 8x one at a time would take.
//
// Timed in-process with Go's client, not curl, and best of three rounds, so
// start-up cost and a noisy core cannot decide the result.
func checkParallel(t *testing.T, src, node string) {
	t.Helper()
	if runtime.NumCPU() < 4 {
		t.Skipf("needs at least 4 CPUs to show parallelism, have %d", runtime.NumCPU())
	}
	bin, port := buildServer(t, src, false)
	server := startServer(t, bin)
	server.waitForLine(t, "listening", 10*time.Second)

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	client := &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 8}}
	request := func() {
		resp, err := client.Get(url)
		if err != nil {
			t.Errorf("GET: %v", err)
			return
		}
		_ = resp.Body.Close()
	}
	timed := func(n int) time.Duration {
		start := time.Now()
		var wg sync.WaitGroup
		for range n {
			wg.Go(request)
		}
		wg.Wait()
		return time.Since(start)
	}

	timed(8) // warm the connections, so neither measurement pays for dialing
	one, eight := time.Hour, time.Hour
	for range 3 {
		one = min(one, timed(1))
		eight = min(eight, timed(8))
	}
	// One at a time, as Node runs them, eight take 8x one. In parallel they
	// take 8/cores at best: 1x with eight cores or more, 2x on a 4-core CI
	// runner, where a fixed 3x failed at 3.27x. Halfway between the two
	// leaves a noisy machine room and still fails handlers that don't overlap.
	ideal := 8 / float64(min(runtime.NumCPU(), 8))
	limit := (ideal + 8) / 2
	ratio := float64(eight) / float64(one)
	t.Logf("1 request %v, 8 concurrent %v, ratio %.2fx, limit %.2fx on %d CPUs (%s)", one, eight, ratio, limit, runtime.NumCPU(), node)
	if ratio > limit {
		t.Errorf("8 concurrent requests took %.2fx one request, want at most %.2fx: handlers are not running in parallel", ratio, limit)
	}

	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
}

// Check 8: a server whose handlers log, and call shared pure code, is free of
// data races under 200 concurrent requests. Built with Go's race detector,
// which needs cgo.
func TestHTTPRaceFree(t *testing.T) {
	t.Parallel()
	checkRaceFree(t, `import { createServer } from "node:http";

const greeting: string = "hello ";

function shout(s: string): string {
  return s + "!";
}

createServer((req, res) => {
  console.log(req.method + " " + req.url);
  res.setHeader("Content-Type", "text/plain");
  res.end(shout(greeting + req.url));
}).listen(3000, () => {
  console.log("listening");
});
`)
}

// checkRaceFree builds src with Go's race detector, which needs cgo, sends
// it 200 requests, 50 at a time, and asserts every one succeeded and the
// detector reported nothing.
func checkRaceFree(t *testing.T, src string) {
	t.Helper()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}
	bin, port := buildServer(t, src, true)
	// A binary built without the detector would report no races whatever it
	// did, so first prove this one has it: Go records the build flag.
	if info, err := exec.Command("go", "version", "-m", bin).Output(); err != nil || !strings.Contains(string(info), "-race=true") {
		t.Fatalf("the server was not built with the race detector (go version -m: %v):\n%s", err, info)
	}
	server := startServer(t, bin)
	server.waitForLine(t, "listening", 30*time.Second)

	client := &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 50}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	failures := 0
	sem := make(chan struct{}, 50)
	for i := range 200 {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/r%d", port, i))
			if err == nil {
				_ = resp.Body.Close()
			}
			if err != nil || resp.StatusCode != http.StatusOK {
				mu.Lock()
				failures++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if failures > 0 {
		t.Errorf("%d of 200 requests failed", failures)
	}

	code := server.interrupt(t)
	if n := strings.Count(server.stderr.String(), "WARNING: DATA RACE"); n > 0 {
		t.Errorf("the race detector reported %d data race(s):\n%s", n, server.stderr.String())
	}
	if code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130 (the race detector exits 66 when it found races)", code)
	}
}
