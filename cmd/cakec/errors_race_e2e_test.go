package main

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// throwingServer's handler throws at once, rejects after an await, catches a
// rejection and answers 500, reads an Error created outside it, and catches
// a spawned function's throw of that Error. Node 24.18, the same file with
// CLAUDE.md's spawn shim, answered /page, /caught, /shared and /spawn as the
// test expects, and died on /sync or /async; cakebear drops only those
// connections (M6 plan D8).
const throwingServer = `import { createServer } from "node:http";
const notFound: Error = new RangeError("not found");
function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}
async function load(path: string): Promise<string> {
  await delay(1);
  if (path === "/async") {
    throw new TypeError("rejected after an await");
  }
  return "page " + path;
}
const server = createServer(async (req, res) => {
  const url: string = req.url;
  if (url === "/sync") {
    throw new Error("thrown at once");
  }
  if (url === "/caught") {
    try {
      await load("/async");
    } catch (e) {
      res.writeHead(500);
      res.end(String(e));
      return;
    }
  }
  if (url === "/shared") {
    res.end(String(notFound));
    return;
  }
  if (url === "/spawn") {
    try {
      await spawn((): number => {
        throw notFound;
      });
    } catch (e) {
      res.end("spawn " + String(e));
      return;
    }
  }
  res.end(await load(url));
});
server.listen(3000, () => console.log("listening"));
`

// TestThrowingHandlersUnderRace (M6 done-check 7): a race-detector build
// serves requests that throw, reject and catch, 50 at a time, with no race
// and without stopping. A handler reading an Error made outside it is no
// race, since an Error never changes once made (plan D10, amended), and a
// spawn's rejection reaches its caller as its result does.
func TestThrowingHandlersUnderRace(t *testing.T) {
	t.Parallel()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}
	bin, port := buildServer(t, throwingServer, true)
	if info, err := exec.Command("go", "version", "-m", bin).Output(); err != nil || !strings.Contains(string(info), "-race=true") {
		t.Fatalf("the server was not built with the race detector (go version -m: %v):\n%s", err, info)
	}
	server := startServer(t, bin)
	server.waitForLine(t, "listening", 30*time.Second)

	paths := []struct {
		path   string
		status int // 0: the connection is dropped
		body   string
	}{
		{"/sync", 0, ""},
		{"/async", 0, ""},
		{"/caught", 500, "TypeError: rejected after an await"},
		{"/shared", 200, "RangeError: not found"},
		{"/spawn", 200, "spawn RangeError: not found"},
		{"/page", 200, "page /page"},
	}
	client := &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 50}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []string
	sem := make(chan struct{}, 50)
	for i := range 200 {
		want := paths[i%len(paths)]
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			got, err := fetch(client, fmt.Sprintf("http://127.0.0.1:%d%s", port, want.path))
			var bad string
			switch {
			case want.status == 0 && err == nil:
				bad = fmt.Sprintf("%s answered %s; its connection should have been dropped", want.path, got)
			case want.status != 0 && got != fmt.Sprintf("%d %s", want.status, want.body):
				bad = fmt.Sprintf("%s: got %q (%v), want %d %q", want.path, got, err, want.status, want.body)
			}
			if bad != "" {
				mu.Lock()
				failures = append(failures, bad)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(failures) > 0 {
		t.Errorf("%d of 200 requests went wrong, first: %s", len(failures), failures[0])
	}
	if got, err := fetch(client, fmt.Sprintf("http://127.0.0.1:%d/shared", port)); got != "200 RangeError: not found" {
		t.Errorf("after the burst, /shared gave %q (%v): the server should serve on", got, err)
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
	stderr := server.stderr.String()
	if n := strings.Count(stderr, "WARNING: DATA RACE"); n > 0 {
		t.Fatalf("%d data races:\n%s", n, stderr)
	}
	for _, want := range []string{"[Error: thrown at once]", "[TypeError: rejected after an await]"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr has no report of %s", want)
		}
	}
	if strings.Contains(stderr, "goroutine ") {
		t.Errorf("stderr holds a Go trace:\n%s", stderr)
	}
}

// fetch is "<status> <body>" for url, or the error that ended it.
func fetch(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return fmt.Sprintf("%d %s", resp.StatusCode, b), err
}
