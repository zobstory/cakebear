package main

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// S1 step 1 (plan D2): fetch, a Response and Headers, held to what Node
// 24.18 printed for the same programs against the same server
// (fetch_server_test.go), 20 runs each. BASE is the server's address and
// DEAD one nothing listens on.

var fetchPrograms = map[string]string{
	// R1, R2, R3: a Response's fields and headers, and text().
	"basic": `const res = await fetch("BASE/hello");
console.log(res.status, res.ok, res.statusText, res.redirected, res.type, res.url === "BASE/hello");
const h = res.headers;
console.log("get", h.get("X-Custom"), h.get("x-multi"), h.get("set-cookie"), h.get("missing"), h.has("X-CUSTOM"));
console.log("getSetCookie", h.getSetCookie().join(" | "));
console.log(res.bodyUsed);
const text: string = await res.text();
console.log("text", text, res.bodyUsed);
export {};
`,
	// R4, R6: what a request sends, with Next.js's options as the no-ops
	// Node makes them.
	"send": `declare global {
  interface RequestInit {
    next?: { revalidate?: number };
  }
}
const res = await fetch("BASE/echo", {
  method: "POST",
  headers: { "Content-Type": "application/json", "X-Custom": "yes", "User-Agent": "mine" },
  body: JSON.stringify({ a: 1, b: "two" }),
  next: { revalidate: 60 },
  cache: "no-store",
});
console.log(await res.text());
const bare = await fetch("BASE/echo");
console.log(await bare.text());
const plain = await fetch("BASE/echo", { method: "PUT", body: "hé 😀" });
console.log(await plain.text());
const none = await fetch("BASE/echo", { method: "post" });
console.log(await none.text());
async function viaCache(mode: RequestCache): Promise<void> {
  const r = await fetch("BASE/echo", { cache: mode });
  console.log("cache", mode, await r.text());
}
await viaCache("default");
await viaCache("no-store");
await viaCache("reload");
await viaCache("no-cache");
await viaCache("force-cache");
try {
  await viaCache("only-if-cached");
} catch (e) {
  console.log(String(e));
}
const own = await fetch("BASE/echo", { cache: "no-store", headers: { "Cache-Control": "max-age=5" } });
console.log(await own.text());
export {};
`,
	// R8: statuses never throw.
	"statuses": "for (const code of [200, 201, 204, 404, 500]) {\n  const r = await fetch(`BASE/status?code=${code}`);\n  console.log(r.status, r.ok, r.statusText, JSON.stringify(await r.text()));\n}\nexport {};\n",
	// R7, R9, R11, R14: failures are rejections.
	"failures": `try {
  await fetch("DEAD/x");
} catch (e) {
  console.log("refused", String(e), e instanceof TypeError);
}
try {
  await fetch("not a url");
} catch (e) {
  console.log(String(e));
}
try {
  await fetch("BASE/echo", { body: "x" });
} catch (e) {
  console.log(String(e));
}
try {
  await fetch("BASE/echo", { method: "BAD METHOD" });
} catch (e) {
  console.log(String(e));
}
try {
  await fetch("BASE/echo", { headers: { "bad name": "x" } });
} catch (e) {
  console.log(String(e));
}
const used = await fetch("BASE/hello");
await used.text();
try {
  await used.text();
} catch (e) {
  console.log(String(e));
}
export {};
`,
	// R15: redirects.
	"redirects": `const a = await fetch("BASE/redirect?code=301", { method: "POST", body: "x" });
console.log(a.status, a.redirected, a.url, await a.text());
const b = await fetch("BASE/redirect?code=307", { method: "POST", body: "x" });
console.log(b.status, b.redirected, await b.text());
const c = await fetch("BASE/redirect?code=302", { redirect: "manual" });
console.log(c.status, c.redirected, c.headers.get("location"));
try {
  await fetch("BASE/redirect?code=302", { redirect: "error" });
} catch (e) {
  console.log(String(e));
}
const d = await fetch("BASE/chain?n=20");
console.log(d.status, d.redirected, d.url);
try {
  await fetch("BASE/chain?n=21");
} catch (e) {
  console.log(String(e));
}
export {};
`,
	// R17, R18, R19: a fetch settles as I/O, after timers already due; its
	// body before a timer set when it resolved.
	"order": `const p = fetch("BASE/slow?ms=30");
setTimeout((): void => console.log("timeout 0"), 0);
setTimeout((): void => console.log("timeout 100"), 100);
console.log("sync");
const r = await p;
console.log("fetch resolved", r.status);
setTimeout((): void => console.log("timeout set when it resolved"), 0);
console.log("text", await r.text());
export {};
`,
	// R9, R11, R15: what each failure prints, its cause too.
	"causes": `try {
  await fetch("DEAD/x");
} catch (e) {
  console.log(e);
}
try {
  await fetch("not a url");
} catch (e) {
  console.log(e);
}
try {
  await fetch("BASE/chain?n=21");
} catch (e) {
  console.log(e);
}
try {
  await fetch("BASE/redirect?code=302", { redirect: "error" });
} catch (e) {
  console.log(e);
}
try {
  await fetch("http://127.0.0.1:1/");
} catch (e) {
  console.log(e);
}
try {
  await fetch("ftp://127.0.0.1/x");
} catch (e) {
  console.log(e);
}
try {
  await fetch("BASE/echo", { cache: "only-if-cached" });
} catch (e) {
  console.log(e);
}
export {};
`,
	// R12: a failure nothing catches ends the program.
	"uncaught": `console.log("before");
await fetch("DEAD/x");
export {};
`,
	// A program's own Headers, and one as init's headers.
	"headers": `const h = new Headers({ "X-B": "2", "x-a": "1" });
h.append("x-a", "again");
h.set("X-C", "  padded  ");
console.log("get", h.get("x-a"), h.get("X-C"), h.has("x-b"), h.get("nope"));
h.delete("X-B");
console.log(h.has("x-b"));
const sc = new Headers([["set-cookie", "a=1"], ["set-cookie", "b=2"]]);
console.log("set-cookie", sc.getSetCookie().join("|"), sc.get("set-cookie"));
const r = await fetch("BASE/echo", { method: "POST", headers: h, body: "x" });
console.log(await r.text());
try {
  h.get("bad name");
} catch (e) {
  console.log(String(e));
}
export {};
`,
}

// deadAddress is an address nothing listens on: one a listener had.
func deadAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr
}

func TestFetchMatchesNode(t *testing.T) {
	t.Parallel()
	goldens := readFetchGoldens(t)
	base, dead := upstream(t), deadAddress(t)
	for name, src := range fetchPrograms {
		want, ok := goldens[name]
		if !ok {
			t.Errorf("%s has no golden: record them again", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bin := buildProgram(t, name, strings.ReplaceAll(withBase(src, base), "DEAD", dead))
			for run := range 20 {
				stdout, stderr, code := runOutput(exec.Command(bin))
				if got := unaddress(stdout, base, dead); got != want.Stdout || code != want.Exit {
					t.Fatalf("run %d: exit %d, printed\n%s\nwant exit %d, as Node prints\n%s\nstderr:\n%s", run, code, got, want.Exit, want.Stdout, stderr)
				}
				switch {
				case strings.HasPrefix(name, "uncaught-json"):
					// Into the data: the whole report is Node's, but its
					// last line (json_e2e_test.go).
					got := unaddress(stderr, base, dead)
					if node := strings.Replace(want.Stderr, "\n\nNode.js v24.18.0\n", "", 1); got != node+"\n\ncakebear "+version+"\n" {
						t.Fatalf("the report is\n%s\nwant, as Node's\n%s", got, node+"\n\ncakebear "+version+"\n")
					}
				case want.Exit != 0:
					checkUncaughtBody(t, unaddress(stderr, base, dead), want.Stderr)
				}
			}
		})
	}
}

// fetchingServer answers each request with what it fetched upstream: each
// request's fetch runs in that request's domain, in parallel with the rest.
const fetchingServer = `import { createServer } from "node:http";
const upstreamBase: string = "BASE";
createServer(async (req, res) => {
  const r = await fetch(upstreamBase + "/hello");
  const body: string = await r.text();
  res.setHeader("Content-Type", "text/plain");
  res.end(` + "`${r.status} ${body} ${req.url}`" + `);
}).listen(PORT, () => {
  console.log("listening");
});
`

// TestFetchFromHandlersUnderRace: 200 requests, 8 at a time, each fetching
// upstream from its handler, from a race-detector build: every reply right
// and no race found.
func TestFetchFromHandlersUnderRace(t *testing.T) {
	t.Parallel()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}
	port := strconv.Itoa(freePort(t))
	src := strings.Replace(withBase(fetchingServer, upstream(t)), "PORT", port, 1)
	path := writeTS(t, "server.ts", src)
	bin := filepath.Join(filepath.Dir(path), "server")
	var stderr strings.Builder
	if code := runBuild(buildOptions{files: []string{path}, output: bin, race: true}, &stderr); code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, stderr.String())
	}
	server := startServer(t, bin)
	server.waitForLine(t, "listening", 30*time.Second)
	client := &http.Client{Timeout: 30 * time.Second}
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for w := range 8 {
		wg.Go(func() {
			for i := range 25 {
				p := "/r" + strconv.Itoa(w*25+i)
				status, _, body, err := request(client, http.MethodGet, "http://127.0.0.1:"+port+p)
				switch {
				case err != nil:
					errs <- err
				case status != 200 || body != "200 hello, world "+p:
					errs <- fmt.Errorf("GET %s = %d %q", p, status, body)
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

// checkUncaughtBody holds an uncaught report's error to Node's: the same
// text between the header and the last line. Node's header points into its
// own run_main, and cakebear's at the program's call (an accepted
// difference); the last line names the runtime.
func checkUncaughtBody(t *testing.T, got, node string) {
	t.Helper()
	body := func(report, trailer string) string {
		_, rest, _ := strings.Cut(report, "\n\n")
		b, _, _ := strings.Cut(rest, "\n\n"+trailer)
		return b
	}
	if g, w := body(got, "cakebear "), body(node, "Node.js "); g != w || g == "" {
		t.Fatalf("the report's error is\n%s\nwant, as Node reports it\n%s\nwhole report:\n%s", g, w, got)
	}
	if !strings.Contains(got, "await fetch(\"DEAD/x\");\n      ^\n") {
		t.Errorf("the report doesn't point at the fetch call:\n%s", got)
	}
}
