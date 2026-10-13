package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// M2 plan checks 1 to 6, each against a cakec-built binary run as a real
// process. The runtime's tests (backend/runtime) replay the same programs by
// hand; these compile them from TypeScript.

const sleepFn = `function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}
`

// syncBuffer is a bytes.Buffer safe to read while a process writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// buildProgram compiles src and returns the binary, failing the test if it
// does not build.
func buildProgram(t *testing.T, name, src string) string {
	t.Helper()
	path := writeTS(t, name+".ts", src)
	bin := filepath.Join(filepath.Dir(path), name)
	var stderr bytes.Buffer
	if code := runBuild(buildOptions{files: []string{path}, output: bin}, &stderr); code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, stderr.String())
	}
	return bin
}

// matchesNode runs bin repeatedly and asserts every run prints want, the
// output Node gives for the same file. Scheduler ordering bugs show up only
// some of the time, so one passing run proves little (M2 plan §6).
func matchesNode(t *testing.T, bin, want string, runs int) {
	t.Helper()
	for run := range runs {
		got, err := exec.Command(bin).Output()
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if string(got) != want {
			t.Fatalf("run %d printed\n%s\nwant, as Node prints\n%s", run, got, want)
		}
	}
}

// Check 1: the part of an async function before its first await runs
// synchronously, so "a" prints before "c", and "b" waits for the timer.
func TestAsyncSleepMatchesNode(t *testing.T) {
	t.Parallel()

	bin := buildProgram(t, "sleep", sleepFn+`
async function main(): Promise<void> {
  console.log("a");
  await sleep(20);
  console.log("b");
}

main();
console.log("c");
`)
	matchesNode(t, bin, "a\nc\nb\n", 20)
}

// Check 2: synchronous code, then microtasks (resuming after an already
// settled promise), then timers in the order they were scheduled.
func TestAsyncOrderingMatchesNode(t *testing.T) {
	t.Parallel()

	bin := buildProgram(t, "ordering", sleepFn+`
async function now(n: number): Promise<number> {
  return n;
}

async function quick(name: string): Promise<number> {
  console.log(name + ": sync part");
  const one: number = await now(1);
  console.log(name + ": resumed (microtask)");
  return one;
}

async function slow(name: string): Promise<number> {
  console.log(name + ": before the timer");
  await sleep(0);
  console.log(name + ": after the timer (macrotask)");
  return 2;
}

async function both(): Promise<void> {
  const a: number = await quick("q");
  const b: number = await slow("s");
  if (a + b === 3) {
    console.log("both: done");
  }
}

setTimeout(() => {
  console.log("timeout 0, scheduled first");
}, 0);
both();
quick("q2");
console.log("top level done");
`)
	// Recorded from Node 24.18, identical over every run (M2 plan §3).
	matchesNode(t, bin, strings.Join([]string{
		"q: sync part",
		"q2: sync part",
		"top level done",
		"q: resumed (microtask)",
		"q2: resumed (microtask)",
		"s: before the timer",
		"timeout 0, scheduled first",
		"s: after the timer (macrotask)",
		"both: done",
	}, "\n")+"\n", 20)
}

// Check 3: an async handler awaits a timer, then does CPU work. The waits
// overlap under Node and cakebear alike; the CPU work after the await runs in
// parallel only here, where each request is its own domain.
func TestAsyncHandlersRunInParallel(t *testing.T) { //nolint:paralleltest // the timing must not share the CPU with parallel tests
	checkParallel(t, `import { createServer } from "node:http";
`+sleepFn+`
function fib(n: number): number {
  if (n < 2) {
    return n;
  }
  return fib(n - 1) + fib(n - 2);
}

createServer(async (req, res) => {
  await sleep(10);
  const result: number = fib(38);
  if (result > 0) {
    res.end("done\n");
  }
}).listen(3000, () => {
  console.log("listening");
});
`, "await then fib(38), Node: ~7.2x")
}

// Check 4: changing shared state after an await is refused exactly once,
// naming the binding, as M1 refuses its synchronous twin.
func TestSharedStateAfterAwaitRefused(t *testing.T) {
	t.Parallel()

	path := writeTS(t, "shared.ts", `import { createServer } from "node:http";

let hits: number = 0;
`+sleepFn+`
createServer(async (req, res) => {
  await sleep(1);
  hits = hits + 1;
  res.end("ok\n");
}).listen(3000);
`)
	var stderr bytes.Buffer
	code := runBuild(buildOptions{files: []string{path}, output: filepath.Join(filepath.Dir(path), "shared")}, &stderr)
	out := stderr.String()
	if code != exitErrors {
		t.Errorf("exit code = %d, want %d\n%s", code, exitErrors, out)
	}
	want := "shared.ts:12:3: cannot compile yet: this handler runs on many requests at once, so it can't change `hits`, which they all share"
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
	if n := strings.Count(out, "cannot compile yet:"); n != 1 {
		t.Errorf("got %d diagnostics, want exactly 1:\n%s", n, out)
	}
}

// Check 5: 200 concurrent requests to async handlers that await a timer, log,
// and call shared pure code, under Go's race detector. Each request's awaits
// hand its domain's token between goroutines; a broken handoff is a race.
func TestAsyncRaceFree(t *testing.T) {
	t.Parallel()
	checkRaceFree(t, `import { createServer } from "node:http";
`+sleepFn+`
const greeting: string = "hello ";

function shout(s: string): string {
  return s + "!";
}

async function wait(ms: number): Promise<number> {
  await sleep(ms);
  return ms;
}

createServer(async (req, res) => {
  const waited: number = await wait(1);
  console.log(req.method + " " + req.url);
  await sleep(1);
  if (waited === 1) {
    res.setHeader("Content-Type", "text/plain");
  }
  res.end(shout(greeting + req.url));
}).listen(3000, () => {
  console.log("listening");
});
`)
}

// Check 6: a program whose last work is a timer exits when it fires, and
// shows what it printed before the wait without waiting for the exit.
//
// The flush is judged against the exit, not the launch: "early" must be
// visible well before the process ends. How soon after launch it appears
// depends on how busy the machine is, which this package's parallel builds
// make it.
func TestAsyncProcessExitsWhenIdle(t *testing.T) {
	t.Parallel()

	bin := buildProgram(t, "idle", `console.log("early");
setTimeout(() => {
  console.log("late");
}, 300);
`)
	cmd := exec.Command(bin)
	var out syncBuffer
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var sawEarly time.Time
	for sawEarly.IsZero() {
		select {
		case err := <-done:
			t.Fatalf("exited (%v) before stdout showed anything; output %q was held until exit", err, out.String())
		case <-time.After(time.Millisecond):
			if out.String() == "early\n" {
				sawEarly = time.Now()
			}
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the program did not exit after its last timer")
	}
	if gap := time.Since(sawEarly); gap < 150*time.Millisecond {
		t.Errorf("\"early\" appeared only %v before the exit; it should show while the 300 ms timer is pending", gap)
	}
	if got := out.String(); got != "early\nlate\n" {
		t.Errorf("stdout = %q, want early then late", got)
	}
	if code := cmd.ProcessState.ExitCode(); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

// A request's setTimeout fires in that request, after its handler's
// synchronous code, and the timer's id is a number clearTimeout accepts.
func TestAsyncTimersInAHandler(t *testing.T) {
	t.Parallel()

	bin, port := buildServer(t, `import { createServer } from "node:http";

createServer((req, res) => {
  const never: number = setTimeout(() => {
    res.end("cleared timer fired\n");
  }, 5);
  clearTimeout(never);
  setTimeout(() => {
    res.end("timer\n");
  }, 5);
}).listen(3000, () => {
  console.log("listening");
});
`, false)
	server := startServer(t, bin)
	server.waitForLine(t, "listening", 10*time.Second)
	if status, _, body := get(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/"); status != 200 || body != "timer\n" {
		t.Errorf("got %d %q, want 200 \"timer\\n\"", status, body)
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
}
