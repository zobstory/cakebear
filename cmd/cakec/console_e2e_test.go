package main

import (
	"bytes"
	"os/exec"
	"testing"
)

// consoleProgram logs through each of console's members that cakebear
// lowers (M6 step 6): error and warn write to stderr, info and debug where
// log writes, all formatted as log formats. Node 24.18 ran this same file
// under --stack-trace-limit=0 and wrote nodeConsoleOut to stdout,
// nodeConsoleErr to stderr, and, with both streams one pipe, nodeConsoleBoth.
const consoleProgram = `export {};
console.log("log 1");
console.error("error 1");
console.info("info 1");
console.warn("warn 1");
console.debug("debug 1");
console.error("error", 2, { a: [1, 2], s: "x" }, -0, undefined, null);
console.warn(new RangeError("as a warning"));
console.log("log 2");
console.error();
console.info("info", -0, [1, 2]);
`

const nodeConsoleOut = `log 1
info 1
debug 1
log 2
info -0 [ 1, 2 ]
`

const nodeConsoleErr = `error 1
warn 1
error 2 { a: [ 1, 2 ], s: 'x' } -0 undefined null
[RangeError: as a warning]

`

const nodeConsoleBoth = `log 1
error 1
info 1
warn 1
debug 1
error 2 { a: [ 1, 2 ], s: 'x' } -0 undefined null
[RangeError: as a warning]
log 2

info -0 [ 1, 2 ]
`

// TestConsoleStreamsMatchNode: each stream holds what Node's holds, and one
// holding both has the lines in the program's order, 20 runs: stdout is
// buffered, so console.error writes what it holds first.
func TestConsoleStreamsMatchNode(t *testing.T) {
	t.Parallel()
	bin := buildProgram(t, "console", consoleProgram)
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.String() != nodeConsoleOut || stderr.String() != nodeConsoleErr {
		t.Errorf("stdout is\n%s\nstderr is\n%s\nwant, as Node writes them\n%s\nand\n%s", &stdout, &stderr, nodeConsoleOut, nodeConsoleErr)
	}
	for run := range 20 {
		var both bytes.Buffer
		cmd := exec.Command(bin)
		cmd.Stdout, cmd.Stderr = &both, &both // one pipe
		if err := cmd.Run(); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if both.String() != nodeConsoleBoth {
			t.Fatalf("run %d: one stream holds\n%s\nwant, as Node writes it\n%s", run, &both, nodeConsoleBoth)
		}
	}
}

// TestConsoleInHandlersUnderRace: request handlers on many goroutines log
// through every member, from a race-detector build, with no race: each line
// is written under the runtime's output lock, stderr's inside stdout's.
func TestConsoleInHandlersUnderRace(t *testing.T) {
	t.Parallel()
	checkRaceFree(t, `import { createServer } from "node:http";
const server = createServer((req, res) => {
  const url: string = req.url;
  console.log("log", url);
  console.error("error", url);
  console.info("info", url);
  console.warn("warn", url);
  console.debug("debug", url);
  res.end("ok");
});
server.listen(3000, () => console.log("listening"));
`)
}
