package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// S1 step 7, done-check 10: the race rule for I/O. A race-detector build
// serves a handler that uses every kind of I/O S1 has: it parses req.url,
// reads process.env, fans out to upstream fetches with headers under an
// AbortSignal, casts res.json() into a shape, JSON.parse's a value into an
// unknown and fails one upstream on a path. Every reply is the one Node
// 24.18 gave for the same sources, and the detector finds no race. The
// refusals are lower/race_io_test.go's.
//
//	CAKEBEAR_RECORD_FETCH=<node 24.18> go test ./cmd/cakec -run TestRecordIOReplies
//
// records Node's replies into testdata/io-replies.json.

const ioServer = `import { createServer } from "node:http";
interface Item { id: number; name: string; tags: string[]; price: number | null }
const upstream: string = "BASE";
async function item(id: number, signal: AbortSignal): Promise<Item> {
  const u = new URL(upstream + "/data");
  const body: string = JSON.stringify({ id: id, name: "item " + String(id), tags: ["t" + String(id % 3)], price: id % 2 === 0 ? null : id * 1.25, extra: true });
  u.searchParams.set("body", body);
  const r = await fetch(u, { headers: { accept: "application/json", "x-request": String(id) }, signal: signal });
  if (!r.ok) {
    throw new Error("upstream answered " + String(r.status));
  }
  return r.json();
}
async function checked(path: string, signal: AbortSignal): Promise<string> {
  const r = await fetch(upstream + path, { signal: signal });
  if (!r.ok) {
    throw new Error(path + " answered " + String(r.status));
  }
  return r.text();
}
async function status(code: number): Promise<number> {
  const r = await fetch(upstream + "/status?code=" + String(code));
  return r.status;
}
createServer(async (req, res) => {
  const here = new URL(req.url, "http://localhost");
  const id: number = Number(here.searchParams.get("id") ?? "0");
  const signal = AbortSignal.timeout(10000);
  try {
    const path: string = here.pathname === "/fail" ? "/status?code=503" : "/hello";
    const [a, b, hello, gone] = await Promise.all([item(id, signal), item(id + 1, signal), checked(path, signal), status(404)]);
    const parsed: unknown = JSON.parse(JSON.stringify(b));
    let total: number = 0;
    for (const x of [a, b]) {
      total = total + (x.price ?? 0);
    }
    const reply = { id: id, names: [a.name, b.name], tags: [a.tags[0], b.tags[0]], total: total, hello: hello, gone: gone, who: process.env.S1_WHO ?? "nobody", query: here.search, b: parsed };
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify(reply));
  } catch (e) {
    res.writeHead(502);
    res.end(String(e));
  }
}).listen(PORT, () => {
  console.log("listening");
});
`

// ioPaths are the requests, each answered as Node answered it.
var ioPaths = []string{"/r?id=1", "/r?id=2&q=x%20y", "/fail?id=3", "/r?id=4", "/r", "/r?id=7&q=%C3%A9"}

const ioRepliesPath = "testdata/io-replies.json"

type ioReply struct {
	Status int    `json:"status"`
	Type   string `json:"type"`
	Body   string `json:"body"`
}

// ioEnv is the server's environment beside PATH.
const ioEnv = "S1_WHO=cakebear"

// TestRecordIOReplies runs ioServer under Node, when asked to, and records
// its reply to each of ioPaths.
func TestRecordIOReplies(t *testing.T) {
	t.Parallel()
	node := os.Getenv("CAKEBEAR_RECORD_FETCH")
	if node == "" {
		t.Skip("set CAKEBEAR_RECORD_FETCH to Node 24.18's binary to record the I/O server's replies")
	}
	port := strconv.Itoa(freePort(t))
	path := writeTS(t, "server.ts", strings.Replace(withBase(ioServer, upstream(t)), "PORT", port, 1))
	script := path + ".sh"
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+ioEnv+" exec "+node+" "+path+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	server := startServer(t, script)
	server.waitForLine(t, "listening", 30*time.Second)
	client := &http.Client{Timeout: 30 * time.Second}
	out := map[string]ioReply{}
	for _, p := range ioPaths {
		status, typ, body, err := request(client, http.MethodGet, "http://127.0.0.1:"+port+p)
		if err != nil {
			t.Fatal(err)
		}
		out[p] = ioReply{status, typ, body}
	}
	server.interrupt(t)
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ioRepliesPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestIOServerUnderRace: 240 requests, 8 at a time, from a race-detector
// build of ioServer: every reply Node's, no race found.
func TestIOServerUnderRace(t *testing.T) {
	t.Parallel()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}
	raw, err := os.ReadFile(ioRepliesPath)
	if err != nil {
		t.Fatalf("%v: record them with CAKEBEAR_RECORD_FETCH=<node 24.18>", err)
	}
	var want map[string]ioReply
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(freePort(t))
	path := writeTS(t, "server.ts", strings.Replace(withBase(ioServer, upstream(t)), "PORT", port, 1))
	bin := filepath.Join(filepath.Dir(path), "server")
	var stderr strings.Builder
	if code := runBuild(buildOptions{files: []string{path}, output: bin, race: true}, &stderr); code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, stderr.String())
	}
	script := bin + ".sh"
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+ioEnv+" exec "+bin+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	server := startServer(t, script)
	server.waitForLine(t, "listening", 30*time.Second)
	client := &http.Client{Timeout: 30 * time.Second}
	var wg sync.WaitGroup
	errs := make(chan error, 240)
	for w := range 8 {
		wg.Go(func() {
			for i := range 30 {
				p := ioPaths[(w+i)%len(ioPaths)]
				status, typ, body, err := request(client, http.MethodGet, "http://127.0.0.1:"+port+p)
				switch r := want[p]; {
				case err != nil:
					errs <- err
				case status != r.Status || typ != r.Type || body != r.Body:
					errs <- fmt.Errorf("GET %s = %d %q %q\nwant, as Node answered, %d %q %q", p, status, typ, body, r.Status, r.Type, r.Body)
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
	if strings.Contains(server.stderr.String(), "DATA RACE") {
		t.Errorf("the race detector found a race:\n%s", server.stderr.String())
	}
}
