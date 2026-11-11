package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/bundled"
	"github.com/zobstory/cakebear/internal/compiler"
	"github.com/zobstory/cakebear/internal/tspath"
	"github.com/zobstory/cakebear/internal/vfs/osvfs"
	cakebeartypes "github.com/zobstory/cakebear/types"
)

// S1 plan D12: a program with @types/node, as a server project usually is,
// is built with @types/node's node:http and timers rather than cakebear's.
// These tests need @types/node's real files: CI fetches the pinned versions
// (scripts/fetch-types-node.sh) and points CAKEBEAR_TYPES_NODE at them.

// typesNodeRoot is the directory holding the pinned @types/node and
// undici-types under node_modules, or skips the test.
func typesNodeRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("CAKEBEAR_TYPES_NODE")
	if root == "" {
		t.Skip("CAKEBEAR_TYPES_NODE is unset: run scripts/fetch-types-node.sh <dir> and point it there")
	}
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "fetch-types-node.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for pkg, pin := range map[string]string{"@types/node": "TYPES_NODE", "undici-types": "UNDICI_TYPES"} {
		m := regexp.MustCompile(`(?m)^` + pin + `=(\S+)$`).FindSubmatch(script)
		if m == nil {
			t.Fatalf("scripts/fetch-types-node.sh no longer pins %s as %s=", pkg, pin)
		}
		var manifest struct {
			Version string `json:"version"`
		}
		raw, err := os.ReadFile(filepath.Join(root, "node_modules", pkg, "package.json"))
		if err == nil {
			err = json.Unmarshal(raw, &manifest)
		}
		if err != nil || manifest.Version != string(m[1]) {
			t.Fatalf("CAKEBEAR_TYPES_NODE has %s %q (%v), want the pinned %s", pkg, manifest.Version, err, m[1])
		}
	}
	return root
}

// typesNodeProject writes a project of files whose tsconfig.json lists
// @types/node, linked from the pinned copy, and has no skipLibCheck: its
// main is main.ts and its output bin/main.
func typesNodeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := typesNodeRoot(t)
	dir := filepath.Join(t.TempDir(), "project")
	project := map[string]string{
		"tsconfig.json": `{
  "compilerOptions": { "strict": true, "target": "es2022", "module": "nodenext", "types": ["node"], "noEmit": true },
  "files": ["main.ts"],
  "cakebear": { "main": "main.ts", "output": "bin/main" }
}
`,
	}
	for name, src := range files {
		project[name] = src
	}
	writeProject(t, dir, project)
	writeLinks(t, dir, map[string]string{
		"node_modules/@types/node":  filepath.Join(root, "node_modules", "@types", "node"),
		"node_modules/undici-types": filepath.Join(root, "node_modules", "undici-types"),
	})
	return dir
}

// typesNodeServer is a node:http server written against @types/node's
// types: req.url and req.method are `string | undefined` there, and the
// handler is a function declaration typed with @types/node's own classes.
const typesNodeServer = `import { createServer } from "node:http";
import type { IncomingMessage, ServerResponse } from "node:http";

const port: number = PORT;

function reply(req: IncomingMessage, res: ServerResponse): void {
  const path: string = req.url ?? "/";
  const verb: string = req.method ?? "GET";
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.writeHead(path === "/" ? 200 : 404);
  res.end(` + "`${verb} ${path}\\n`" + `);
}

let timer: ReturnType<typeof setTimeout> | undefined = setTimeout((): void => {
  console.log("never");
}, 1);
if (timer) {
  clearTimeout(timer);
  timer = undefined;
}

createServer(reply).listen(port, (): void => {
  console.log(` + "`listening on http://localhost:${port}`" + `);
});
`

// TestTypesNodeServerServes: with @types/node and no skipLibCheck, the
// program type-checks (cakebear's node:http stepped aside, or @types/node's
// own files would fail it), lowers through @types/node's declarations, and
// serves what Node serves.
func TestTypesNodeServerServes(t *testing.T) {
	t.Parallel()
	port := strconv.Itoa(freePort(t))
	dir := typesNodeProject(t, map[string]string{"main.ts": strings.Replace(typesNodeServer, "PORT", port, 1)})
	if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	server := startServer(t, filepath.Join(dir, "bin", "main"))
	server.waitForLine(t, "listening on http://localhost:"+port, 30*time.Second)
	client := &http.Client{Timeout: 10 * time.Second}
	for _, want := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/", "GET /\n", 200},
		{http.MethodGet, "/missing", "GET /missing\n", 404},
		{http.MethodPost, "/", "POST /\n", 200},
	} {
		status, typ, body, err := request(client, want.method, "http://127.0.0.1:"+port+want.path)
		if err != nil {
			t.Errorf("%s %s: %v", want.method, want.path, err)
			continue
		}
		if status != want.status || typ != "text/plain; charset=utf-8" || body != want.body {
			t.Errorf("%s %s = %d %q %q, want %d text/plain; charset=utf-8 %q", want.method, want.path, status, typ, body, want.status, want.body)
		}
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
	// A cleared 1 ms timer would have printed long before now.
	for {
		select {
		case line := <-server.lines:
			if line == "never" {
				t.Error("a cleared timer fired")
			}
			continue
		default:
		}
		break
	}
}

// typesNodeTimers uses @types/node's setTimeout, whose Timeout only
// clearTimeout takes, through every way a program names its type. Node
// 24.18 printed nodeTimers for it, 3 of 3 runs.
const typesNodeTimers = `export {};
const a: NodeJS.Timeout = setTimeout((): void => console.log("a"), 30);
const b = setTimeout((): void => console.log("b"), 20);
clearTimeout(a);
let c: ReturnType<typeof setTimeout> | undefined = setTimeout((): void => console.log("c"), 10);
if (c !== undefined) {
  clearTimeout(c);
  c = undefined;
}
setTimeout((): void => {
  console.log("d", c === undefined);
  clearTimeout(b);
}, 1);
setTimeout((): void => console.log("e"), 40);
console.log("sync");
`

const nodeTimers = "sync\nd true\ne\n"

func TestTypesNodeTimersMatchNode(t *testing.T) {
	t.Parallel()
	dir := typesNodeProject(t, map[string]string{"main.ts": typesNodeTimers})
	if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	matchesNode(t, filepath.Join(dir, "bin", "main"), nodeTimers, 5)
}

// TestTypesNodeRefusals: what @types/node declares and cakebear's runtime
// doesn't have is refused by name, as it is with cakebear's declarations,
// and so is what @types/node's looser signatures let through.
func TestTypesNodeRefusals(t *testing.T) {
	t.Parallel()
	head := "import { createServer } from \"node:http\";\n"
	for _, tt := range []struct{ name, body, want string }{
		{"listening on a host", "createServer((req, res) => res.end(\"x\")).listen(3000, \"127.0.0.1\");\n",
			"Server.listen's second argument must be a callback, not a string"},
		{"listening on a path", "createServer((req, res) => res.end(\"x\")).listen(\"/tmp/s.sock\");\n",
			"Server.listen's first argument must be a port number"},
		{"a number as a header's value", "createServer((req, res) => {\n  res.setHeader(\"X-Count\", 3);\n  res.end();\n}).listen(3000);\n",
			"ServerResponse.setHeader's second argument must be a string, not a number"},
		{"end with a callback", "createServer((req, res) => {\n  res.end((): void => {});\n}).listen(3000);\n",
			"ServerResponse.end's first argument must be a string"},
		{"headers", "createServer((req, res) => {\n  res.end(String(req.headers));\n}).listen(3000);\n",
			"cakebear's runtime does not implement IncomingMessage.headers yet"},
		{"another node:http function", "import { get } from \"node:http\";\nget(\"http://example.com/\");\n",
			"cakebear's runtime does not implement get yet"},
		{"a Timeout's own member", "export {};\nconst t = setTimeout((): void => {}, 10);\nt.unref();\n",
			"Timeout.unref is not supported yet"},
	} {
		dir := typesNodeProject(t, map[string]string{"main.ts": head + tt.body})
		code, stderr := projectBuild(t, dir, buildOptions{})
		if code == exitOK || !strings.Contains(stderr, tt.want) {
			t.Errorf("%s: exit %d, want a refusal containing %q:\n%s", tt.name, code, tt.want, stderr)
		}
	}
}

// countingHost counts the parses it is asked for.
type countingHost struct {
	compiler.CompilerHost
	mu     sync.Mutex
	parses map[tspath.RootedFilePath]int
}

func (h *countingHost) GetSourceFile(opts ast.SourceFileParseOptions) *ast.SourceFile {
	h.mu.Lock()
	h.parses[opts.FileName]++
	h.mu.Unlock()
	return h.CompilerHost.GetSourceFile(opts)
}

// TestReuseHostParsesEachFileOnce: the build a program with @types/node
// makes again reparses nothing, and shares bound files soundly: the second
// program, without cakebear's node:http, checks clean.
func TestReuseHostParsesEachFileOnce(t *testing.T) {
	t.Parallel()
	path := writeTS(t, "main.ts", "const n: number = 1;\nconsole.log(n + 1);\n")
	fs := cakebeartypes.WrapFS(bundled.WrapFS(osvfs.FS()))
	var stderr strings.Builder
	config, _ := filesConfig(buildOptions{files: []string{path}}, fs, filepath.Dir(path), &stderr)
	if config == nil {
		t.Fatal(stderr.String())
	}
	counting := &countingHost{CompilerHost: compiler.NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil), parses: map[tspath.RootedFilePath]int{}}
	host := newReuseHost(counting)
	first := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})
	second := compiler.NewProgram(compiler.ProgramOptions{Config: withoutNodeHTTP(config), Host: host})
	for name, n := range counting.parses {
		if n != 1 {
			t.Errorf("%s parsed %d times, want once", name, n)
		}
	}
	shared := map[*ast.SourceFile]bool{}
	for _, f := range first.SourceFiles() {
		shared[f] = true
	}
	for _, f := range second.SourceFiles() {
		if !shared[f] {
			t.Errorf("the second program has its own %s", f.FileName())
		}
		if string(f.FileName()) == cakebeartypes.NodeHTTPPath() {
			t.Errorf("the second program still has cakebear's node:http")
		}
	}
	if diags := collectDiagnostics(context.Background(), second, withoutNodeHTTP(config)); len(diags) > 0 {
		t.Errorf("the second program has %d diagnostics, want none: %v", len(diags), diags[0])
	}
}
