package main

import (
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// M2 stretch step 8: spawn, against cakec-built binaries.

const fibFn = `function fib(n: number): number {
  if (n < 2) {
    return n;
  }
  return fib(n - 1) + fib(n - 2);
}
`

// bestRun runs bin three times and returns its stdout and the fastest time.
func bestRun(t *testing.T, bin string) (string, time.Duration) {
	t.Helper()
	best := time.Hour
	var out []byte
	for range 3 {
		start := time.Now()
		got, err := exec.Command(bin).Output()
		if err != nil {
			t.Fatalf("running %s: %v", bin, err)
		}
		best, out = min(best, time.Since(start)), got
	}
	return string(out), best
}

// Four spawned fib(38) calls take about as long as one: they run in parallel,
// on their own goroutines, which Node can't do without worker threads. Done
// in sequence they would take four times as long.
//
// Four rather than two, with the limit at 2.5x, so the check stays far from
// both outcomes. On a machine with efficiency cores a spawn that lands on one
// runs slower; measured on a 6+6-core Mac, 1, 2 and 4 spawns all took
// 0.12-0.15 s.
func TestSpawnRunsInParallel(t *testing.T) { //nolint:paralleltest // the timing must not share the CPU with parallel tests
	if runtime.NumCPU() < 4 {
		t.Skipf("needs at least 4 CPUs to show parallelism, have %d", runtime.NumCPU())
	}
	one := buildProgram(t, "one", fibFn+`
export {};
const x: number = await spawn(() => fib(38));
console.log(x);
`)
	four := buildProgram(t, "four", fibFn+`
export {};
const a: Promise<number> = spawn(() => fib(38));
const b: Promise<number> = spawn(() => fib(38));
const c: Promise<number> = spawn(() => fib(38));
const d: Promise<number> = spawn(() => fib(38));
const w: number = await a;
const x: number = await b;
const y: number = await c;
const z: number = await d;
console.log(w + x + y + z);
`)
	outOne, tOne := bestRun(t, one)
	outFour, tFour := bestRun(t, four)
	if outOne != "39088169\n" || outFour != "156352676\n" {
		t.Fatalf("printed %q and %q, want fib(38) and four times it", outOne, outFour)
	}
	ratio := float64(tFour) / float64(tOne)
	t.Logf("one spawn %v, four spawns %v, ratio %.2fx (in sequence: ~4x)", tOne, tFour, ratio)
	if ratio > 2.5 {
		t.Errorf("four spawns took %.2fx one; they are not running in parallel", ratio)
	}
}

// A handler can spawn work and await it; the response waits for the result.
func TestSpawnInAHandler(t *testing.T) {
	t.Parallel()

	bin, port := buildServer(t, `import { createServer } from "node:http";
`+fibFn+`
createServer(async (req, res) => {
  const n: number = await spawn(() => fib(25));
  if (n === 75025) {
    res.end("fib(25) done\n");
  }
}).listen(3000, () => {
  console.log("listening");
});
`, false)
	server := startServer(t, bin)
	server.waitForLine(t, "listening", 10*time.Second)
	if status, _, body := get(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/"); status != 200 || body != "fib(25) done\n" {
		t.Errorf("got %d %q, want 200 \"fib(25) done\\n\"", status, body)
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
}

// 200 requests whose handlers each spawn work, log and await it, under Go's
// race detector: the spawn's result is the only thing that crosses between
// domains, through the caller's inbox.
func TestSpawnRaceFree(t *testing.T) {
	t.Parallel()
	checkRaceFree(t, `import { createServer } from "node:http";
`+fibFn+`
createServer(async (req, res) => {
  const depth: number = 15;
  const n: number = await spawn(() => fib(depth));
  console.log(req.method + " " + req.url);
  if (n > 0) {
    res.end("ok\n");
  }
}).listen(3000, () => {
  console.log("listening");
});
`)
}

// The program waits for a spawn it never awaits, as Node waits for a timer.
func TestProgramWaitsForASpawn(t *testing.T) {
	t.Parallel()

	bin := buildProgram(t, "unawaited", `spawn(() => {
  console.log("from the spawn");
});
console.log("main done");
`)
	matchesNode(t, bin, "main done\nfrom the spawn\n", 5)
}
