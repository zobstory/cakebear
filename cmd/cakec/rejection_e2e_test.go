package main

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rejectionsProgram covers M6 step 4, rejection through async functions,
// await and executors (plan D7):
//   - a throw before an async function's first await rejects, and its caller
//     carries on (R15); one after an await; an executor's throw;
//   - a promise settles once, whatever comes after (R16);
//   - every kind of reason, and a catch getting the rejected value itself
//     (R18);
//   - rejections and resolutions in Node's order, tick for tick (R17);
//   - a rejection handled later in the same drain (R11);
//   - a spawned function's throw, and an async one's after a timer, caught
//     around the caller's await.
//
// Node 24.18 ran this same file under --stack-trace-limit=0, with spawn as
// CLAUDE.md's shim, and printed nodeRejections, the same in 4 of 4 runs.
const rejectionsProgram = "export {};\n" +
	"function delay(ms: number): Promise<void> {\n" +
	"  return new Promise((resolve) => {\n" +
	"    setTimeout(resolve, ms);\n" +
	"  });\n" +
	"}\n" +
	"function rejectWith(reason: unknown, ms: number): Promise<number> {\n" +
	"  return new Promise((resolve, reject) => {\n" +
	"    setTimeout(() => reject(reason), ms);\n" +
	"  });\n" +
	"}\n" +
	"async function afterAwait(): Promise<number> {\n" +
	"  await delay(1);\n" +
	"  throw new RangeError(\"after await\");\n" +
	"}\n" +
	"async function beforeAwait(): Promise<number> {\n" +
	"  console.log(\"before await: runs at once\");\n" +
	"  throw new TypeError(\"before await\");\n" +
	"}\n" +
	"function executorThrows(): Promise<number> {\n" +
	"  return new Promise((resolve) => {\n" +
	"    throw new URIError(\"in the executor\");\n" +
	"  });\n" +
	"}\n" +
	"function settlesOnce(): Promise<number> {\n" +
	"  return new Promise((resolve, reject) => {\n" +
	"    resolve(1);\n" +
	"    reject(new Error(\"ignored\"));\n" +
	"    resolve(2);\n" +
	"    throw new Error(\"ignored as well\");\n" +
	"  });\n" +
	"}\n" +
	"function rejectsOnce(): Promise<number> {\n" +
	"  return new Promise((resolve, reject) => {\n" +
	"    reject(\"the first reason\");\n" +
	"    resolve(3);\n" +
	"    reject(\"ignored\");\n" +
	"  });\n" +
	"}\n" +
	"async function describe(name: string, p: Promise<number>): Promise<void> {\n" +
	"  try {\n" +
	"    const v: number = await p;\n" +
	"    console.log(`${name} resolved ${v}`);\n" +
	"  } catch (e) {\n" +
	"    if (e instanceof Error) {\n" +
	"      console.log(`${name} caught ${e.name} ${e.message}`);\n" +
	"    } else if (e === undefined) {\n" +
	"      console.log(`${name} caught undefined`);\n" +
	"    } else if (e === null) {\n" +
	"      console.log(`${name} caught null`);\n" +
	"    } else {\n" +
	"      console.log(`${name} caught ${String(e)}`);\n" +
	"    }\n" +
	"  }\n" +
	"}\n" +
	"\n" +
	"// A throw before the first await rejects, and the caller carries on at once.\n" +
	"const early: Promise<number> = beforeAwait();\n" +
	"console.log(\"the caller carries on\");\n" +
	"await describe(\"before\", early);\n" +
	"await describe(\"after\", afterAwait());\n" +
	"await describe(\"executor\", executorThrows());\n" +
	"await describe(\"settles once\", settlesOnce());\n" +
	"await describe(\"rejects once\", rejectsOnce());\n" +
	"const reasons: unknown[] = [new Error(\"an Error\"), \"a string\", 5, undefined, null, { code: 1 }];\n" +
	"for (const reason of reasons) {\n" +
	"  await describe(\"reason\", rejectWith(reason, 1));\n" +
	"}\n" +
	"\n" +
	"// What a catch gets is the rejected value itself.\n" +
	"const same: Error = new Error(\"same\");\n" +
	"try {\n" +
	"  await rejectWith(same, 1);\n" +
	"} catch (e) {\n" +
	"  console.log(\"identical\", e === same);\n" +
	"}\n" +
	"\n" +
	"// Rejections and resolutions take the same hops: the order is Node's.\n" +
	"async function relay(name: string, p: Promise<number>): Promise<void> {\n" +
	"  try {\n" +
	"    await p;\n" +
	"    console.log(`${name} resolved`);\n" +
	"  } catch (e) {\n" +
	"    console.log(`${name} rejected`);\n" +
	"  }\n" +
	"}\n" +
	"const ok: Promise<number> = new Promise((resolve) => resolve(1));\n" +
	"const bad: Promise<number> = new Promise((resolve, reject) => reject(new Error(\"bad\")));\n" +
	"const r1: Promise<void> = relay(\"A\", ok);\n" +
	"const r2: Promise<void> = relay(\"B\", bad);\n" +
	"const r3: Promise<void> = relay(\"C\", ok);\n" +
	"console.log(\"sync end\");\n" +
	"await r1;\n" +
	"await r2;\n" +
	"await r3;\n" +
	"\n" +
	"// A rejection handled later in the same drain is handled.\n" +
	"const late: Promise<number> = new Promise((resolve, reject) => reject(new Error(\"late\")));\n" +
	"await new Promise<void>((resolve) => resolve());\n" +
	"await new Promise<void>((resolve) => resolve());\n" +
	"await describe(\"late\", late);\n" +
	"\n" +
	"// A spawned function's throw rejects the spawn's promise in its caller.\n" +
	"try {\n" +
	"  const n: number = await spawn((): number => {\n" +
	"    throw new RangeError(\"in a spawn\");\n" +
	"  });\n" +
	"  console.log(\"never\", n);\n" +
	"} catch (e) {\n" +
	"  console.log(\"spawn\", String(e));\n" +
	"}\n" +
	"try {\n" +
	"  await spawn(async (): Promise<number> => {\n" +
	"    await delay(1);\n" +
	"    throw new Error(\"in an async spawn\");\n" +
	"  });\n" +
	"} catch (e) {\n" +
	"  console.log(\"async spawn\", String(e));\n" +
	"}\n" +
	"console.log(\"end\");\n"

const nodeRejections = `before await: runs at once
the caller carries on
before caught TypeError before await
after caught RangeError after await
executor caught URIError in the executor
settles once resolved 1
rejects once caught the first reason
reason caught Error an Error
reason caught a string
reason caught 5
reason caught undefined
reason caught null
reason caught [object Object]
identical true
sync end
A resolved
B rejected
C resolved
late caught Error late
spawn RangeError: in a spawn
async spawn Error: in an async spawn
end
`

func TestRejectionsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "rejections", rejectionsProgram), nodeRejections, 20)
}

// TestUnhandledRejectionsEndAsNode: a rejection nothing handles ends the
// program as it ends Node's: the output so far, then the report, exit 1.
// Each stdout and body is what Node 24.18 printed for the same file under
// --stack-trace-limit=0. Node's header is its own internals for each
// (node:internal/process/promises, or run_main for a top-level await), an
// accepted difference.
func TestUnhandledRejectionsEndAsNode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, src, stdout, body string }{
		// every microtask runs first, no timer (R9)
		{"u1_drain", "export {};\n" +
			"async function step(n: number): Promise<void> {\n" +
			"  console.log(`micro ${n}`);\n" +
			"}\n" +
			"async function fail(): Promise<void> {\n" +
			"  throw new Error(\"unhandled\");\n" +
			"}\n" +
			"async function later(): Promise<void> {\n" +
			"  await step(1);\n" +
			"  await step(2);\n" +
			"  console.log(\"micro 3\");\n" +
			"}\n" +
			"setTimeout(() => console.log(\"timer, never\"), 0);\n" +
			"fail();\n" +
			"console.log(\"sync\");\n" +
			"later();\n", "sync\nmicro 1\nmicro 2\nmicro 3\n", "[Error: unhandled]"},
		// a reason that isn't an Error (R13)
		{"u2_string", "export {};\n" +
			"function rejectNow(): Promise<number> {\n" +
			"  return new Promise((resolve, reject) => reject(\"a reason\"));\n" +
			"}\n" +
			"rejectNow();\n" +
			"console.log(\"sync\");\n", "sync\n", "[UnhandledPromiseRejection: This error originated either by throwing inside of an async function without a catch block, or by rejecting a promise which was not handled with .catch(). The promise rejected with the reason \"a reason\".] {\n  code: 'ERR_UNHANDLED_REJECTION'\n}"},
		// a top-level await's rejection is an uncaught exception (R14)
		{"u3_tla", "export {};\n" +
			"console.log(\"before\");\n" +
			"await new Promise<void>((resolve, reject) => {\n" +
			"  setTimeout(() => reject(new TypeError(\"awaited\")), 1);\n" +
			"});\n" +
			"console.log(\"never\");\n", "before\n", "[TypeError: awaited]"},
		// checked after the timer's work, before the next timer (R10)
		{"u4_timer", "export {};\n" +
			"async function fail(): Promise<void> {\n" +
			"  throw new RangeError(\"in a timer\");\n" +
			"}\n" +
			"setTimeout(() => {\n" +
			"  console.log(\"first\");\n" +
			"  fail();\n" +
			"}, 1);\n" +
			"setTimeout(() => console.log(\"second, never\"), 5);\n", "first\n", "[RangeError: in a timer]"},
		// handled after a timer is too late (R11)
		{"u5_late", "export {};\n" +
			"function delay(ms: number): Promise<void> {\n" +
			"  return new Promise((resolve) => {\n" +
			"    setTimeout(resolve, ms);\n" +
			"  });\n" +
			"}\n" +
			"async function failSoon(): Promise<number> {\n" +
			"  await delay(1);\n" +
			"  throw new Error(\"too late\");\n" +
			"}\n" +
			"async function slowly(): Promise<number> {\n" +
			"  await delay(20);\n" +
			"  return 1;\n" +
			"}\n" +
			"const a: Promise<number> = failSoon();\n" +
			"const b: Promise<number> = slowly();\n" +
			"try {\n" +
			"  await b;\n" +
			"  await a;\n" +
			"} catch (e) {\n" +
			"  console.log(\"caught\", String(e));\n" +
			"}\n", "", "[Error: too late]"},
		// a string there prints as a thrown one (R14, R7)
		{"u6_tlastring", "export {};\n" +
			"console.log(\"before\");\n" +
			"await new Promise<void>((resolve, reject) => reject(\"a string reason\"));\n" +
			"console.log(\"never\");\n", "before\n", "a string reason"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checkUncaught(t, buildProgram(t, "unhandled_"+tt.name, tt.src), tt.stdout, tt.body)
		})
	}
}

// asyncHandlerProgram's handler awaits, and its page load rejects for
// /boom. Node 24.18 served /ok and /caught, then dropped /boom's connection
// and died on the unhandled rejection. cakebear drops only that request's
// connection and serves on (plan D8), with the report on stderr.
const asyncHandlerProgram = `import { createServer } from "node:http";
function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}
async function load(url: string): Promise<string> {
  await delay(1);
  if (url === "/boom") {
    throw new RangeError("no such page");
  }
  return "page " + url;
}
const server = createServer(async (req, res) => {
  const url: string = req.url;
  if (url === "/caught") {
    try {
      await load("/boom");
    } catch (e) {
      res.writeHead(404);
      res.end(String(e));
      return;
    }
  }
  const body: string = await load(url);
  res.end(body);
});
server.listen(3000, () => console.log("listening"));
`

func TestAsyncHandlerRejectionDropsOnlyItsConnection(t *testing.T) {
	t.Parallel()
	port := strconv.Itoa(freePort(t))
	server := startServer(t, buildProgram(t, "async_handler", strings.ReplaceAll(asyncHandlerProgram, "3000", port)))
	server.waitForLine(t, "listening", 10*time.Second)
	base := "http://127.0.0.1:" + port
	if status, _, body := get(t, base+"/ok"); status != 200 || body != "page /ok" {
		t.Errorf("GET /ok = %d %q, want 200 %q", status, body, "page /ok")
	}
	if status, _, body := get(t, base+"/caught"); status != 404 || body != "RangeError: no such page" {
		t.Errorf("GET /caught = %d %q, want 404 %q", status, body, "RangeError: no such page")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	if resp, err := client.Get(base + "/boom"); err == nil {
		_ = resp.Body.Close()
		t.Errorf("GET /boom answered %d; its connection should have been dropped", resp.StatusCode)
	}
	if status, _, body := get(t, base+"/ok"); status != 200 || body != "page /ok" {
		t.Errorf("GET /ok after /boom = %d %q, want 200 %q: the server should serve on", status, body, "page /ok")
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
	if got := server.stderr.String(); !strings.Contains(got, "[RangeError: no such page]") || strings.Contains(got, "goroutine") {
		t.Errorf("stderr is\n%s\nwant the report of /boom's rejection, and no Go trace", got)
	}
}
