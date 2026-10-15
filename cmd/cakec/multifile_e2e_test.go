package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// M3 plan checks 4 to 7, end to end: TypeScript projects of several modules,
// built from a tsconfig.json by cakec. The sources import with .ts
// extensions, so Node 24 runs the very same files, stripping their types, and
// node is what Node 24.18 printed for them (5 runs each, all identical).

// moduleProject is a project of files in src/, whose main is src/main.ts.
func moduleProject(files map[string]string) map[string]string {
	project := map[string]string{
		"tsconfig.json": `{ "compilerOptions": { "strict": true, "noEmit": true, "allowImportingTsExtensions": true }, ` +
			`"include": ["src"], "cakebear": { "main": "src/main.ts", "output": "app" } }`,
	}
	for name, src := range files {
		project["src/"+name] = src
	}
	return project
}

var multiModulePrograms = []struct {
	name  string
	files map[string]string
	node  string
}{
	{
		"check 4: functions and consts across two modules",
		map[string]string{
			"lib.ts":  "let calls: number = 0;\nexport const greeting: string = \"hello\";\n\nexport function count(): number {\n  calls = calls + 1;\n  return calls;\n}\n\nexport function greet(name: string): string {\n  return greeting + \", \" + name;\n}\n\nexport function twice(n: number): number {\n  return n * 2;\n}\n\nconsole.log(\"lib loaded\");\n",
			"main.ts": "import { count, greet, greeting, twice as double } from \"./lib.ts\";\n\nconsole.log(greet(\"cakebear\"));\nconsole.log(count());\nconsole.log(count());\nconsole.log(double(21));\nconsole.log(greeting);\n",
		},
		"lib loaded\nhello, cakebear\n1\n2\n42\nhello\n",
	},
	{
		"check 5: evaluation order, with a cycle",
		map[string]string{
			"a.ts":    "import { b } from \"./b.ts\";\nconsole.log(\"a\");\nexport function a(): number {\n  return b();\n}\n",
			"b.ts":    "import { a } from \"./a.ts\";\nconsole.log(\"b\");\nexport function b(): number {\n  return 1;\n}\nexport function callsA(): number {\n  return a();\n}\n",
			"c.ts":    "import { b } from \"./b.ts\";\nconsole.log(\"c\");\nexport function c(): number {\n  return b() + 1;\n}\n",
			"main.ts": "import { a } from \"./a.ts\";\nimport { c } from \"./c.ts\";\nconsole.log(\"main\");\nconsole.log(a() + c());\n",
		},
		"b\na\nc\nmain\n3\n",
	},
	{
		"check 6: a helper in each of two modules",
		map[string]string{
			"main.ts": "import { helper } from \"./x.ts\";\nimport { helper as other } from \"./y.ts\";\n\nfunction f(helper: number): number {\n  return helper + other();\n}\n\nconsole.log(helper());\nconsole.log(other());\nconsole.log(f(100));\n",
			"x.ts":    "let n: number = 1;\nexport function helper(): number {\n  n = n + 1;\n  return n;\n}\n",
			"y.ts":    "const n: number = 10;\nexport function helper(): number {\n  return n;\n}\n",
		},
		"2\n10\n110\n",
	},
	{
		"async functions across modules",
		map[string]string{
			"lib.ts":  "export function sleep(ms: number): Promise<void> {\n  return new Promise((resolve) => {\n    setTimeout(resolve, ms);\n  });\n}\n\nexport async function step(label: string, ms: number): Promise<string> {\n  await sleep(ms);\n  return label;\n}\n",
			"main.ts": "import { step } from \"./lib.ts\";\n\nconsole.log(\"start\");\nconst slow: Promise<string> = step(\"slow\", 30);\nconst fast: Promise<string> = step(\"fast\", 5);\nconsole.log(await fast);\nconsole.log(await slow);\nconsole.log(\"end\");\n",
		},
		"start\nfast\nslow\nend\n",
	},
	{
		"names Go doesn't allow, across modules",
		map[string]string{
			"fmt.ts":  "export function $label(n: number): string {\n  if (n > 9) {\n    return \"#big\";\n  }\n  return \"#small\";\n}\nexport const $base: number = 7;\nconsole.log(\"fmt.ts\");\n",
			"main.ts": "import { $label, $base as base$ } from \"./fmt.ts\";\n\nconsole.log($label(base$));\nconsole.log($label(base$ * 2));\n",
		},
		"fmt.ts\n#small\n#big\n",
	},
	{
		"one shape across modules, each literal with its own key order",
		map[string]string{
			"types.ts": "export interface User { id: number; name: string }\n",
			"users.ts": "import type { User } from \"./types.ts\";\n\nexport function make(id: number, name: string): User {\n  return { name: name, id: id };\n}\nexport function rename(u: User, name: string): void {\n  u.name = name;\n}\n",
			"main.ts":  "import type { User } from \"./types.ts\";\nimport { make, rename } from \"./users.ts\";\n\nconst ann: User = make(1, \"Ann\");\nconst bob: User = { id: 2, name: \"Bob\" };\nrename(ann, \"Anne\");\nconsole.log(ann, bob);\nconsole.log(ann === make(1, \"Anne\"));\n",
		},
		"{ name: 'Anne', id: 1 } { id: 2, name: 'Bob' }\nfalse\n",
	},
}

// TestMultiFileMatchesNode (checks 4, 5 and 6): each program prints what Node
// printed, 20 runs out of 20.
func TestMultiFileMatchesNode(t *testing.T) {
	t.Parallel()

	for _, p := range multiModulePrograms {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeProject(t, dir, moduleProject(p.files))
			if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
				t.Fatalf("build: exit %d\n%s", code, stderr)
			}
			matchesNode(t, filepath.Join(dir, "app"), p.node, 20)
		})
	}
}

// TestImportedSharedStateRefused (check 7): a request handler calling an
// imported function that changes its own module's let is refused, and the
// refusal names both.
func TestImportedSharedStateRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, moduleProject(map[string]string{
		"counter.ts": "let hits: number = 0;\nexport function hit(): void {\n  hits = hits + 1;\n}\n",
		"main.ts": "import { createServer } from \"node:http\";\nimport { hit } from \"./counter.ts\";\n\n" +
			"createServer((req, res) => {\n  hit();\n  res.end(\"ok\");\n}).listen(3000);\n",
	}))
	code, stderr := projectBuild(t, dir, buildOptions{})
	want := "main.ts:5:3: cannot compile yet: this handler runs on many requests at once, so it can't call `hit()`, which changes `hits` in counter.ts, shared by them all"
	if code != exitErrors || !strings.Contains(stderr, want) {
		t.Errorf("exit %d, want %d with\n%s\ngot:\n%s", code, exitErrors, want, stderr)
	}
}

// TestMultiFileServerServes: examples/http-project, a node:http server in
// three modules, builds from its tsconfig.json and serves, as it does under
// Node.
func TestMultiFileServerServes(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sends SIGINT, which a Windows process cannot receive")
	}

	dir := filepath.Join(t.TempDir(), "http-project")
	example := filepath.Join("..", "..", "examples", "http-project")
	project := map[string]string{}
	for _, name := range []string{"tsconfig.json", "src/main.ts", "src/routes.ts", "src/greeting.ts"} {
		src, err := os.ReadFile(filepath.Join(example, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		project[name] = string(src)
	}
	port := strconv.Itoa(freePort(t))
	project["src/main.ts"] = strings.ReplaceAll(project["src/main.ts"], "3000", port)
	writeProject(t, dir, project)
	if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}

	server := startServer(t, filepath.Join(dir, "bin", "server"))
	want := "listening on http://localhost:" + port
	select {
	case line := <-server.lines:
		if line != want {
			t.Fatalf("first line = %q, want %q", line, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no %q within 10s", want)
	}
	base := "http://127.0.0.1:" + port
	if status, typ, body := get(t, base+"/"); status != 200 || typ != "text/plain" || body != "hello, cakebear\n" {
		t.Errorf("GET / = %d %q %q", status, typ, body)
	}
	if _, _, body := get(t, base+"/health"); body != "ok\n" {
		t.Errorf("GET /health body = %q, want %q", body, "ok\n")
	}
	if code := server.interrupt(t); code != 130 {
		t.Errorf("exit code after SIGINT = %d, want 130", code)
	}
}
