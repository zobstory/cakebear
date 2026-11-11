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

// S1 step 4 (plan D5): Promise.all over an array and destructured from a
// literal, Promise.race, Promise.resolve and reject, and an async
// function's ` + "`return p`" + ` of a promise, which adopts it. Node 24.18 printed
// the same for each program (promisePrograms join the fetch goldens), every
// microtask hop included: promises-order logs a tick per hop beside them.

func init() {
	for name, src := range promisePrograms {
		fetchPrograms[name] = src
	}
}

var promisePrograms = map[string]string{
	// Where each settles against a ticker that logs a line per microtask:
	// all of none at once, of values and promises a hop after the last,
	// race a hop after the first, and an async function's return of a
	// promise two hops after it, where return await takes one.
	"promises-order": `const settled: Promise<void> = Promise.resolve();
async function ticker(): Promise<void> {
  for (let i = 0; i < 10; i++) {
    console.log("tick", i);
    await settled;
  }
}
async function plain(): Promise<number> {
  return 1;
}
async function oneAwait(): Promise<number> {
  await settled;
  return 2;
}
async function adoptSettled(): Promise<number> {
  return Promise.resolve(3);
}
async function awaitSettled(): Promise<number> {
  return await Promise.resolve(4);
}
async function adoptPending(): Promise<number> {
  return oneAwait();
}
async function adoptPlain(): Promise<number> {
  return plain();
}
async function report(name: string, p: Promise<number>): Promise<void> {
  const v = await p;
  console.log("got", name, v);
}
async function reportAll(name: string, p: Promise<number[]>): Promise<void> {
  const v = await p;
  console.log("got", name, v);
}
async function reportVoid(name: string, p: Promise<void>): Promise<void> {
  await p;
  console.log("got", name);
}
ticker();
report("resolve", Promise.resolve(5));
report("adopt-settled", adoptSettled());
report("return-await", awaitSettled());
report("adopt-pending", adoptPending());
report("adopt-plain", adoptPlain());
reportAll("all-empty", Promise.all([]));
reportAll("all-two", Promise.all([plain(), oneAwait()]));
reportAll("all-values", Promise.all([1, 2]));
const ps: Promise<number>[] = [oneAwait(), plain()];
reportAll("all-array", Promise.all(ps));
report("race", Promise.race([oneAwait(), plain()]));
async function allOf(): Promise<void> {
  const [a, b] = await Promise.all([plain(), Promise.resolve("s")]);
  console.log("allof", a, b);
}
allOf();
console.log("sync end");
`,
	// Values in their array's order, the first rejection in time and a later
	// one handled, race either way, resolve and reject caught, and the
	// destructured form at the top level, as a let, with a hole, in a
	// function and rejected; a statement's all, an empty literal typed by
	// its slot, and a literal of one type as an array.
	"promises-results": `interface Item { id: number; name: string }
function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}
async function after(ms: number, v: number): Promise<number> {
  await sleep(ms);
  console.log("settled", v);
  return v;
}
// quiet is a loser's: one that logs would race the next section's timers,
// its deadline set before theirs by however long the section took.
async function quiet(ms: number, v: number): Promise<number> {
  await sleep(ms);
  return v;
}
async function failAfter(ms: number, m: string): Promise<number> {
  await sleep(ms);
  throw new RangeError(m);
}
async function item(id: number): Promise<Item> {
  await sleep(5);
  return { id: id, name: "item" + String(id) };
}
async function adoptItem(id: number): Promise<Item> {
  return item(id);
}
async function adoptReject(): Promise<number> {
  return Promise.reject(new TypeError("adopted"));
}
async function adoptFail(): Promise<number> {
  return failAfter(3, "late");
}
const xs: number[] = await Promise.all([after(30, 1), after(10, 2), after(20, 3)]);
console.log("in order", xs);
try {
  await Promise.all([quiet(10, 4), failAfter(5, "first"), failAfter(15, "second")]);
} catch (e) {
  console.log("all rejected", e);
}
const fast = await Promise.race([quiet(20, 5), after(5, 6)]);
console.log("race", fast);
const early = await Promise.race([after(5, 15), quiet(20, 16)]);
console.log("race first", early);
try {
  const never: number[] = await Promise.all([after(1, 17), failAfter(3, "an array's")]);
  console.log("never", never);
} catch (e) {
  console.log("array rejected", e);
}
try {
  await Promise.race([quiet(20, 7), failAfter(5, "race")]);
} catch (e) {
  console.log("race rejected", e);
}
const r = await Promise.resolve("value");
console.log("resolved", r);
try {
  await Promise.reject(new Error("rejected"));
} catch (e) {
  console.log("caught", e);
}
try {
  const n: number = await Promise.reject<number>("a string");
  console.log("never", n);
} catch (e) {
  console.log("caught reason", e);
}
const [a, b, c] = await Promise.all([item(1), after(1, 8), "plain"]);
console.log("destructured", a, b, c);
let [d, , f] = await Promise.all([adoptItem(2), sleep(1), Promise.resolve(true)]);
d = { id: 9, name: "changed" };
console.log("let", d, f);
async function inside(): Promise<string> {
  const [p, q] = await Promise.all([item(3), adoptItem(4)]);
  return p.name + q.name;
}
console.log("inside", await inside());
try {
  await adoptReject();
} catch (e) {
  console.log("adopted rejection", e);
}
try {
  await adoptFail();
} catch (e) {
  console.log("adopted failure", e);
}
try {
  const [g, h] = await Promise.all([item(5), failAfter(2, "destructured")]);
  console.log("never", g, h);
} catch (e) {
  console.log("destructured rejected", e);
}
await Promise.all([sleep(2), sleep(1)]);
console.log("statement");
const ys: number[] = await Promise.all([]);
console.log("empty", ys);
const twice: number[] = await Promise.all([after(1, 10), after(2, 11)]);
console.log("same types", twice, twice.length);
console.log("logged", await Promise.all([after(1, 12), after(1, 13)]));
async function waitThen(): Promise<void> {
  return sleep(1);
}
await waitThen();
console.log("adopted nothing");
const maybe: Promise<number | undefined> = Promise.resolve(14);
const none: Promise<number | undefined> = Promise.resolve(undefined);
console.log("maybes", await maybe, await none);
export {};
`,
	// The reference server's shape: a fan-out to upstream GETs, each an
	// async function returning what res.json() or res.text() gives, a
	// checked cast of each json() into a typed pattern, a race, and a
	// refused connection rejecting all.
	"promises-fetch": `interface Item { id: number; name: string; price: number }
interface Page { status: string; count: number }
async function getItem(id: number): Promise<Item> {
  const body: string = '{"id":' + String(id) + ',"name":"n' + String(id) + '","price":' + String(id * 1.5) + ',"extra":true}';
  const r = await fetch("BASE/data?body=" + encodeURIComponent(body));
  return r.json();
}
async function getText(path: string): Promise<string> {
  const r = await fetch("BASE" + path);
  return r.text();
}
async function status(code: number): Promise<number> {
  const r = await fetch("BASE/status?code=" + String(code));
  return r.status;
}
const [a, b, hello, s] = await Promise.all([getItem(1), getItem(2), getText("/hello"), status(404)]);
console.log("fan-out", a, b, hello, s);
const r1 = await fetch("BASE/data?body=" + encodeURIComponent('{"id":3,"name":"x","price":1}'));
const r2 = await fetch("BASE/data?body=" + encodeURIComponent('{"status":"ok","count":2}'));
const [item, page]: [Item, Page] = await Promise.all([r1.json(), r2.json()]);
console.log("typed", item, page);
const first = await Promise.race([getText("/slow?ms=100"), getText("/hello")]);
console.log("race", first);
const items: Item[] = await Promise.all([getItem(4), getItem(5)]);
console.log("items", items.length, items[1].name);
try {
  const dead = async (): Promise<string> => {
    const r = await fetch("DEAD/x");
    return r.text();
  };
  await Promise.all([getText("/hello"), dead()]);
} catch (e) {
  console.log("caught", e);
}
export {};
`,
	// An async function's return of a promise where nothing of it runs
	// after: after a try, in a catch with no finally, in a finally. Inside
	// a try it is refused, since Node adopts it once the try has run.
	// An executor's resolve(p) of a promise: each settled, pending and
	// rejected p's hops against the ticker, resolved for good once it is
	// called (a later resolve, reject or throw does nothing), from a timer,
	// a promise of nothing, and resolved with itself.
	"promises-resolve": `const settled: Promise<void> = Promise.resolve();
async function ticker(): Promise<void> {
  for (let i = 0; i < 8; i++) {
    console.log("tick", i);
    await settled;
  }
}
function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}
async function plain(): Promise<number> {
  return 1;
}
async function oneAwait(): Promise<number> {
  await settled;
  return 2;
}
async function failing(): Promise<number> {
  throw new RangeError("adopted rejection");
}
async function report(name: string, p: Promise<number>): Promise<void> {
  try {
    console.log("got", name, await p);
  } catch (e) {
    console.log("rejected", name, e);
  }
}
ticker();
report("settled", new Promise((resolve) => resolve(plain())));
report("pending", new Promise((resolve) => resolve(oneAwait())));
report("rejected", new Promise((resolve) => resolve(failing())));
report("locked", new Promise((resolve, reject) => {
  resolve(oneAwait());
  resolve(9);
  reject(new Error("ignored"));
}));
report("thrown after", new Promise((resolve) => {
  resolve(plain());
  throw new Error("ignored too");
}));
report("value first", new Promise((resolve) => {
  resolve(3);
  resolve(plain());
}));
report("later", new Promise((resolve) => {
  setTimeout(() => resolve(oneAwait()), 1);
}));
const v: Promise<void> = new Promise((resolve) => resolve(sleep(2)));
await v;
console.log("void adopted");
const p: Promise<number> = new Promise((resolve) => {
  setTimeout(() => resolve(p), 1);
});
try {
  await p;
} catch (e) {
  console.log("cycle", e);
}
async function inHandlerShape(): Promise<string> {
  const inner: Promise<string> = new Promise((resolve, reject) => {
    const q: Promise<string> = Promise.resolve("q");
    resolve(q);
  });
  return await inner;
}
console.log(await inHandlerShape());
console.log("sync end");
export {};
`,
	"promises-try": `function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}
async function slowFail(m: string): Promise<number> {
  await sleep(2);
  console.log("failing", m);
  throw new Error(m);
}
async function after(): Promise<number> {
  try {
    console.log("in try");
  } finally {
    console.log("after's finally");
  }
  return slowFail("after");
}
async function inCatch(): Promise<number> {
  try {
    throw new TypeError("first");
  } catch (e) {
    console.log("caught", e);
    return slowFail("from catch");
  }
}
async function inFinally(): Promise<number> {
  try {
    console.log("body");
  } finally {
    return slowFail("from finally");
  }
}
const fns: (() => Promise<number>)[] = [after, inCatch, inFinally];
for (const f of fns) {
  try {
    console.log(await f());
  } catch (e) {
    console.log("outside", e);
  }
}
export {};
`,
}

// TestUnhandledThroughPromisesEndAsNode: a rejection that reaches no await
// through Promise.all, race or reject ends the program as Node's, after
// the output so far, with the error Node 24.18 printed under
// --stack-trace-limit=0. Node's header is its own internals, an accepted
// difference (rejection_e2e_test.go).
func TestUnhandledThroughPromisesEndAsNode(t *testing.T) {
	t.Parallel()
	const sleep = "function sleep(ms: number): Promise<void> {\n  return new Promise((resolve) => {\n    setTimeout(resolve, ms);\n  });\n}\n"
	for _, tt := range []struct{ name, src, body string }{
		{"all", "async function fail(): Promise<number> {\n  throw new RangeError(\"through all\");\n}\n" +
			"async function fine(): Promise<number> {\n  return 1;\n}\nPromise.all([fine(), fail()]);\n", "[RangeError: through all]"},
		{"reject_string", "Promise.reject(\"a reason\");\n", "[UnhandledPromiseRejection: This error originated either by throwing inside of an async function without a catch block, or by rejecting a promise which was not handled with .catch(). The promise rejected with the reason \"a reason\".] {\n  code: 'ERR_UNHANDLED_REJECTION'\n}"},
		{"reject_error", "const p: Promise<number> = Promise.reject(new Error(\"made here\"));\n", "[Error: made here]"},
		{"race", sleep + "async function failSoon(): Promise<number> {\n  await sleep(1);\n  throw new TypeError(\"lost the race\");\n}\n" +
			"async function slow(): Promise<number> {\n  await sleep(30);\n  console.log(\"slow, never\");\n  return 2;\n}\nPromise.race([failSoon(), slow()]);\n", "[TypeError: lost the race]"},
		{"adopted", "async function fail(): Promise<number> {\n  throw new URIError(\"adopted\");\n}\nasync function adopt(): Promise<number> {\n  return fail();\n}\nadopt();\n", "[URIError: adopted]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkUncaught(t, buildProgram(t, "unhandled_"+tt.name, "export {};\n"+tt.src+"console.log(\"sync\");\n"), "sync\n", tt.body)
		})
	}
}

// fanOutServer is the reference server's handler: three upstream GETs at
// once, destructured from Promise.all, one through an async function that
// returns res.json() cast to its type.
const fanOutServer = `import { createServer } from "node:http";
interface Item { id: number; name: string }
const upstreamBase: string = "BASE";
async function getItem(id: number): Promise<Item> {
  const r = await fetch(upstreamBase + "/data?body=" + encodeURIComponent('{"id":' + String(id) + ',"name":"n"}'));
  return r.json();
}
async function getText(path: string): Promise<string> {
  const r = await fetch(upstreamBase + path);
  return r.text();
}
createServer(async (req, res) => {
  const [hello, slow, item] = await Promise.all([getText("/hello"), getText("/slow?ms=2"), getItem(7)]);
  const all: string[] = await Promise.all([getText("/hello"), Promise.resolve(req.url)]);
  res.setHeader("Content-Type", "text/plain");
  res.end(hello + "|" + slow + "|" + String(item.id) + item.name + "|" + all.join(","));
}).listen(PORT, () => {
  console.log("listening");
});
`

// TestPromiseFanOutUnderRace: 200 requests, 8 at a time, each fanning out
// to upstream with Promise.all from its handler, from a race-detector
// build: every reply right and no race found.
func TestPromiseFanOutUnderRace(t *testing.T) {
	t.Parallel()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}
	port := strconv.Itoa(freePort(t))
	src := strings.Replace(withBase(fanOutServer, upstream(t)), "PORT", port, 1)
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
				want := "hello, world|slow 2|7n|hello, world," + p
				status, _, body, err := request(client, http.MethodGet, "http://127.0.0.1:"+port+p)
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
