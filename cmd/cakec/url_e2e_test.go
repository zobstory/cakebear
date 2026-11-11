package main

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// S1 step 5 (plan D6, s1-node R24, R25, R38–R43): URL and URLSearchParams
// lowered onto the runtime's URL Standard (runtime/url*.go). The runtime's
// goldens hold the parser, the setters and the form encoding to Node 24.18
// case by case; these programs hold the lowering of each member, printing
// and fetch's URL input and form body (urlPrograms join the fetch goldens).

func init() {
	for name, src := range urlPrograms {
		fetchPrograms[name] = src
	}
}

var urlPrograms = map[string]string{
	"url": `const u = new URL("HTTPS://user:pw@Example.COM:443/a/../b/./c d?q=a b&r=é#frag");
console.log("parts", u.href, u.origin, u.protocol, u.username, u.password, u.host, u.hostname, u.port, u.pathname, u.search, u.hash);
const rel = new URL("../up?x=1", "https://example.com/dir/page.html");
console.log("relative", rel.href, new URL("other.html", u).href, new URL("?q=2", rel).href);
u.pathname = "b c/d";
u.search = "q=a b&r=é";
u.hash = "x y";
u.port = "8080";
u.username = "me@x";
u.port = "abc";
console.log("set", u.href);
const params = u.searchParams;
params.append("z", "1");
params.set("q", "new");
params.delete("r");
console.log("live", u.href, u.search, params.toString(), params.size);
console.log("strings", String(u), ` + "`${u}`" + `, u.toString(), u.toJSON(), JSON.stringify(u), JSON.stringify(params));
console.log(u);
console.log(params);
console.log("canParse", URL.canParse("not a url"), URL.canParse("x", "https://a.test/"));
const bad = URL.parse("nope");
console.log("parse", bad === null);
const good = URL.parse("p?q", "https://a.test/dir/");
if (good !== null) {
  console.log("parsed", good.href);
}
try {
  new URL("not a url");
} catch (e) {
  console.log(e);
}
try {
  new URL("x", "also bad");
} catch (e) {
  console.log(e);
}
try {
  u.href = "not a url";
} catch (e) {
  console.log(e);
}
const q = new URLSearchParams("?b=2&a=1&b=3&c=&d&=e&a=4");
console.log("params", q.toString(), q.size, q.get("b"), q.get("missing"), q.getAll("b"), q.has("a"), q.has("a", "9"), q.has("a", "4"));
q.delete("a", "1");
q.sort();
console.log("sorted", q.toString());
q.forEach((value, key) => {
  console.log("pair", key, value);
});
const fromObj = new URLSearchParams({ a: "1", "b c": "x y", e: "é&=+~!'()*" });
console.log("from an object", fromObj.toString(), ` + "`${fromObj}`" + `, String(fromObj));
const copy = new URLSearchParams(fromObj);
copy.append("z", "2");
console.log("copied", copy.toString(), fromObj.toString());
const empty = new URLSearchParams();
console.log("empty", empty.size, empty.toString(), empty);
const r = await fetch(new URL("BASE/echo"), { method: "POST", body: new URLSearchParams({ a: "1 2", b: "é" }) });
const echo = (await r.json()) as { type: string; body: string };
console.log("form body", echo.type, echo.body);
export {};
`,
	// URLs in arrays and objects, printed and serialised, at depth too, and
	// compound assignment through a setter.
	"url-nested": `interface Link { name: string; to: URL; params: URLSearchParams }
const urls: URL[] = [new URL("http://a.test/"), new URL("https://b.test/x?y=1")];
console.log(urls, JSON.stringify(urls));
const l: Link = { name: "x", to: urls[1], params: urls[1].searchParams };
console.log(l, JSON.stringify(l));
const deep = { a: { b: { c: l } } };
console.log(deep);
const u = new URL("http://a.test/p?q=1#h");
u.pathname += "/x";
u.search += "&r=2";
u.hash += "i";
console.log(u.href);
for (const each of urls) {
  console.log("each", each.host);
}
const live = new URLSearchParams("a=1&b=2&c=3");
live.forEach((value, key, all) => {
  console.log("live", key, value, all.size);
  if (key === "a") {
    live.delete("b");
  }
});
export {};
`,
}

// urlServer is the reference server's URL work: req.url parsed against a
// base, a query read from it, an upstream URL built with searchParams and
// fetched under a deadline.
const urlServer = `import { createServer } from "node:http";
const upstreamBase: string = "BASE";
createServer(async (req, res) => {
  const here = new URL(req.url, "http://localhost");
  const id = here.searchParams.get("id") ?? "none";
  const up = new URL(upstreamBase + "/echo");
  up.searchParams.set("id", id);
  up.searchParams.append("from", here.pathname);
  const r = await fetch(up, { signal: AbortSignal.timeout(5000) });
  const echo = (await r.json()) as { url: string };
  res.setHeader("Content-Type", "text/plain");
  res.end(echo.url);
}).listen(PORT, () => {
  console.log("listening");
});
`

// TestURLsInHandlersUnderRace: 200 requests, 8 at a time, each parsing its
// own URL and building an upstream one, from a race-detector build: every
// reply right and no race found.
func TestURLsInHandlersUnderRace(t *testing.T) {
	t.Parallel()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}
	port := strconv.Itoa(freePort(t))
	src := strings.Replace(withBase(urlServer, upstream(t)), "PORT", port, 1)
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
				id := strconv.Itoa(w*25 + i)
				p := "/items/" + id + "?id=a b " + id
				want := "/echo?id=a+b+" + id + "&from=%2Fitems%2F" + id
				status, _, body, err := request(client, http.MethodGet, "http://127.0.0.1:"+port+strings.ReplaceAll(p, " ", "%20"))
				switch {
				case err != nil:
					errs <- err
				case status != 200 || body != want:
					errs <- fmt.Errorf("GET %s = %d %q, want %q", p, status, body, want)
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
