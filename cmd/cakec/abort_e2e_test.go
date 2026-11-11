package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// S1 step 5 (plan D2, s1-node R16): AbortSignal.timeout as fetch's signal.
// It rejects a fetch waiting for its headers, and a body read waiting for
// its body, with Node's DOMException; abortPrograms join the fetch goldens.

func init() {
	for name, src := range abortPrograms {
		fetchPrograms[name] = src
	}
}

var abortPrograms = map[string]string{
	// Before the headers and during the body, a signal two fetches share,
	// one that fired before the call, one a fetch beats, and the delays
	// AbortSignal.timeout refuses.
	"abort": `function show(label: string, e: unknown): void {
  const err = e as Error;
  console.log("---", label);
  console.log("name", err.name, "message", err.message, "instanceof Error", e instanceof Error);
  console.log("String", String(err));
  console.log(err);
}
try {
  await fetch("BASE/slow?ms=3000", { signal: AbortSignal.timeout(50) });
  console.log("never");
} catch (e) {
  show("before the headers", e);
}
const res = await fetch("BASE/slowbody?ms=3000", { signal: AbortSignal.timeout(150) });
console.log("headers arrived", res.status);
try {
  await res.text();
  console.log("never");
} catch (e) {
  show("during the body", e);
}
const shared = AbortSignal.timeout(50);
try {
  await Promise.all([fetch("BASE/slow?ms=3000", { signal: shared }), fetch("BASE/slow?ms=3000", { signal: shared })]);
  console.log("never");
} catch (e) {
  show("shared", e);
}
const fired = AbortSignal.timeout(1);
await new Promise<void>((resolve) => setTimeout(resolve, 30));
try {
  await fetch("BASE/hello", { signal: fired });
  console.log("never");
} catch (e) {
  show("fired before the call", e);
}
const ok = await fetch("BASE/hello", { signal: AbortSignal.timeout(5000) });
console.log("in time", ok.status, await ok.text());
for (const ms of [1.5, -1, 4294967296, 0 / 0, 1 / 0]) {
  try {
    AbortSignal.timeout(ms);
  } catch (e) {
    console.log(e);
  }
}
console.log("end");
export {};
`,
}

// TestUncaughtAbortEndsAsNode: an abort nothing catches ends the program
// with the DOMException Node 24.18 printed, under --stack-trace-limit=0.
func TestUncaughtAbortEndsAsNode(t *testing.T) {
	t.Parallel()
	bin := buildProgram(t, "uncaught_abort", withBase(`console.log("before");
await fetch("BASE/slow?ms=3000", { signal: AbortSignal.timeout(20) });
export {};
`, upstream(t)))
	checkUncaught(t, bin, "before\n", "[DOMException [TimeoutError]: The operation was aborted due to timeout]")
}

// TestAbortSignalKeepsNothingAlive: a signal's timer is unref'd, as Node's
// is, so a program whose fetch finished long before its signal would fire
// ends at once, and a request's domain is done after its response.
func TestAbortSignalKeepsNothingAlive(t *testing.T) {
	t.Parallel()
	base := upstream(t)
	bin := buildProgram(t, "unref", withBase(`const s = AbortSignal.timeout(60000);
const r = await fetch("BASE/hello", { signal: AbortSignal.timeout(60000) });
console.log(r.status, await r.text());
export {};
`, base))
	start := time.Now()
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "200 hello, world" {
		t.Errorf("printed %q", got)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("took %v: a signal's timer kept the program alive", took)
	}
}
