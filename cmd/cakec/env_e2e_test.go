package main

import (
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

// S1 step 6 (plan D8, s1-node R44): process.env, read-only. A read by name
// or by a computed key is a string or undefined, in tests presence, and a
// handler may read it, the table never changing. Writes are refused by
// name, with @types/node's declarations, whose types allow them.

// envProgram reads process.env every way the subset has.
const envProgram = `const set = process.env.S1_SET;
console.log("set", set, set === undefined, "unset", process.env.S1_UNSET, process.env.S1_UNSET === undefined);
console.log("empty", ` + "`" + `[${process.env.S1_EMPTY}]` + "`" + `, !!process.env.S1_EMPTY, ` + "`" + `[${process.env.S1_EMPTY ?? "d"}]` + "`" + `, process.env.S1_EMPTY || "d");
console.log("exact", ` + "`" + `[${process.env.S1_SPACES}]` + "`" + `, ` + "`" + `[${process.env.S1_EQ}]` + "`" + `);
console.log("unicode", process.env.S1_UNICODE, process.env.S1_UNICODE?.length);
console.log("case-sensitive", process.env.s1_set);
console.log("in", "S1_SET" in process.env, "S1_UNSET" in process.env, "S1_EMPTY" in process.env);
const name: string = "S1" + "_SET";
console.log("computed", process.env[name], process.env["S1_EQ"]);
const port: string = process.env.S1_PORT ?? "8080";
console.log("numbers", Number(port), parseInt(process.env.S1_NUMSTR ?? "", 10), Number(process.env.S1_NUMSTR ?? ""));
if (process.env.S1_SET !== undefined) {
  console.log("narrowed", process.env.S1_SET.toUpperCase());
}
const flag: boolean = process.env.S1_FLAG === "1";
console.log("flag", flag, process.env.S1_MISSING ?? "fallback");
function config(key: string, fallback: string): string {
  return process.env[key] ?? fallback;
}
console.log("config", config("S1_SET", "x"), config("S1_NOPE", "x"));
export {};
`

// testEnv is the environment envProgram ran with under Node 24.18 (env -i):
// set, empty, spaces, '=' in a value and non-ASCII.
var testEnv = []string{"PATH=/usr/bin:/bin", "S1_SET=value", "S1_EMPTY=", "S1_EQ=a=b=c", "S1_UNICODE=café😀",
	"S1_PORT=3000", "S1_NUMSTR=0x1F", "S1_FLAG=1", "S1_SPACES=  two  words "}

// nodeEnv is what Node 24.18 printed for envProgram under testEnv.
const nodeEnv = "set value false unset undefined true\nempty [] false [] d\nexact [  two  words ] [a=b=c]\nunicode café😀 6\ncase-sensitive undefined\nin true false true\ncomputed value a=b=c\nnumbers 3000 0 31\nnarrowed VALUE\nflag true fallback\nconfig value x\n"

func TestEnvMatchesNode(t *testing.T) {
	t.Parallel()
	bin := buildProgram(t, "env", envProgram)
	envMatchesNode(t, bin)
}

func envMatchesNode(t *testing.T, bin string) {
	t.Helper()
	for run := range 5 {
		cmd := exec.Command(bin)
		cmd.Env = testEnv
		got, err := cmd.Output()
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if string(got) != nodeEnv {
			t.Fatalf("run %d printed\n%s\nwant, as Node prints\n%s", run, got, nodeEnv)
		}
	}
}

// envServer reads process.env in its handler, by name and computed, and
// with a fallback: the table never changes, so no request races another.
const envServer = `import { createServer } from "node:http";
createServer((req, res) => {
  const greeting: string = process.env.S1_GREETING ?? "hello";
  const key: string = "S1_" + "TARGET";
  res.setHeader("Content-Type", "text/plain");
  res.end(greeting + " " + (process.env[key] ?? "nobody") + " " + req.url + " " + String("S1_GREETING" in process.env));
}).listen(PORT, () => {
  console.log("listening");
});
`

// TestEnvInHandlersUnderRace: 200 requests, 8 at a time, each reading
// process.env, from a race-detector build: every reply right, no race.
func TestEnvInHandlersUnderRace(t *testing.T) {
	t.Parallel()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}
	port := strconv.Itoa(freePort(t))
	path := writeTS(t, "server.ts", strings.Replace(envServer, "PORT", port, 1))
	bin := filepath.Join(filepath.Dir(path), "server")
	var stderr strings.Builder
	if code := runBuild(buildOptions{files: []string{path}, output: bin, race: true}, &stderr); code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, stderr.String())
	}
	// The server's environment, set by a script that becomes it (exec), so
	// SIGINT still reaches the server.
	script := bin + ".sh"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nS1_GREETING=hi S1_TARGET=there exec "+bin+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	server := startServer(t, script)
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
				case status != 200 || body != "hi there "+p+" true":
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

// TestTypesNodeEnv: with @types/node, process.env reads as with cakebear's
// declaration, and the writes its types allow are refused by name, as
// are process's other members and process.env whole.
func TestTypesNodeEnv(t *testing.T) {
	t.Parallel()
	dir := typesNodeProject(t, map[string]string{"main.ts": envProgram})
	if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	envMatchesNode(t, filepath.Join(dir, "bin", "main"))
	const write = "writing process.env is not supported: the environment would be state every request shares"
	for _, tt := range []struct{ name, body, want string }{
		{"an assignment", "process.env.X = \"1\";\n", write},
		{"a computed assignment", "const k: string = \"X\";\nprocess.env[k] = \"1\";\n", write},
		{"a delete", "delete process.env.X;\n", write},
		{"??=", "process.env.X ??= \"1\";\n", write},
		{"+=", "process.env.X += \"1\";\n", write},
		{"process.env whole", "console.log(process.env);\n", "process.env as a whole is not supported yet: read a variable by name"},
		{"another member", "console.log(process.argv);\n", "process.argv is not supported yet; only process.env's variables are read"},
		{"process.exit", "process.exit(1);\n", "process.exit is not supported yet"},
	} {
		dir := typesNodeProject(t, map[string]string{"main.ts": tt.body + "export {};\n"})
		code, stderr := projectBuild(t, dir, buildOptions{})
		if code == exitOK || !strings.Contains(stderr, tt.want) {
			t.Errorf("%s: exit %d, want a refusal containing %q:\n%s", tt.name, code, tt.want, stderr)
		}
	}
}
