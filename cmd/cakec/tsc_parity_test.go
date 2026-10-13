package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// M3 plan checks 1 and 2: anything but `cakec build` is tsc. These tests run
// upstream's cmd/tsc and cakec, both built from this tree, over the same
// projects and arguments, and require the same stdout, stderr and exit code,
// and the same files on disk afterwards.

// tscProjects are the sample projects, by name: each maps a path to its
// contents. A case lays its project down fresh for each compiler.
var tscProjects = map[string]map[string]string{
	"empty": {},
	"clean": {
		"tsconfig.json": `{ "compilerOptions": { "strict": true, "rootDir": "src" }, "include": ["src"] }`,
		"src/lib.ts":    "export function double(n: number): number { return n * 2; }\n",
		"src/main.ts":   "import { double } from \"./lib\";\nconsole.log(double(21));\n",
	},
	"errors": {
		"tsconfig.json": `{ "compilerOptions": { "strict": true }, "include": ["src"] }`,
		"src/main.ts":   "const n: number = \"forty-two\";\nconsole.log(n);\n",
	},
	// cakebear's own names in a project with no "cakebear" block: tsc doesn't
	// know them, and cakec mustn't either (M3 plan D4).
	"cakebear-names": {
		"tsconfig.json": `{ "compilerOptions": { "strict": true }, "include": ["src"] }`,
		"src/main.ts":   "const n = i32(42);\nvoid spawn(() => n);\nexport {};\n",
	},
	// A "cakebear" block, which tsc ignores.
	"cakebear-block": {
		"tsconfig.json": `{ "compilerOptions": { "strict": true }, "include": ["src"], "cakebear": { "main": "src/main.ts", "output": "app" } }`,
		"src/main.ts":   "console.log(\"hello\");\n",
	},
	// Files named on the command line, with no tsconfig.json.
	"loose": {
		"a.ts": "export const a: number = 1;\n",
		"b.ts": "import { a } from \"./a\";\nconsole.log(a);\n",
	},
	// Project references, for --build.
	"references": {
		"tsconfig.json":      `{ "files": [], "references": [{ "path": "./core" }, { "path": "./app" }] }`,
		"core/tsconfig.json": `{ "compilerOptions": { "composite": true, "strict": true } }`,
		"core/index.ts":      "export function double(n: number): number { return n * 2; }\n",
		"app/tsconfig.json":  `{ "compilerOptions": { "composite": true, "strict": true }, "references": [{ "path": "../core" }] }`,
		"app/main.ts":        "import { double } from \"../core/index\";\nconsole.log(double(21));\n",
	},
}

// tscCases are the invocations compared. exit is tsc's own, pinned so that a
// project can't quietly stop testing what its case is named for.
var tscCases = []struct {
	name    string
	project string
	args    []string
	exit    int
}{
	{"no arguments", "empty", nil, 1},
	{"--version", "empty", []string{"--version"}, 0},
	{"-v", "empty", []string{"-v"}, 0},
	{"--help", "empty", []string{"--help"}, 0},
	{"--help --all", "empty", []string{"--help", "--all"}, 0},
	{"--init", "empty", []string{"--init"}, 0},
	{"unknown flag", "empty", []string{"--frost"}, 1},
	{"missing file", "empty", []string{"nope.ts"}, 2},
	{"a file named help", "empty", []string{"help"}, 2},
	{"project in cwd", "clean", nil, 0},
	{"-p", "clean", []string{"-p", "."}, 0},
	{"-p tsconfig.json", "clean", []string{"-p", "tsconfig.json"}, 0},
	{"--noEmit", "clean", []string{"--noEmit"}, 0},
	{"--declaration", "clean", []string{"--declaration"}, 0},
	{"--outDir", "clean", []string{"--outDir", "dist"}, 0},
	{"--incremental", "clean", []string{"--incremental"}, 0},
	{"--showConfig", "clean", []string{"--showConfig"}, 0},
	{"--listFilesOnly", "clean", []string{"--listFilesOnly"}, 0},
	{"--explainFiles", "clean", []string{"--explainFiles"}, 0},
	{"type errors", "errors", nil, 2},
	{"type errors --pretty", "errors", []string{"--pretty"}, 2},
	{"type errors --noEmitOnError", "errors", []string{"--noEmitOnError"}, 1},
	{"cakebear's names without a block", "cakebear-names", nil, 2},
	{"a cakebear block", "cakebear-block", nil, 0},
	{"files without a tsconfig.json", "loose", []string{"a.ts", "b.ts", "--outDir", "out"}, 0},
	{"--build", "references", []string{"--build"}, 0},
	{"-b --verbose", "references", []string{"-b", "--verbose"}, 0},
	{"--lsp without --stdio", "empty", []string{"--lsp"}, 1},
	{"--api with a bad flag", "empty", []string{"--api", "--frost"}, 2},
}

func TestCakecIsTsc(t *testing.T) {
	t.Parallel()
	c := tscAndCakec(t)

	for _, tc := range tscCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			project, ok := tscProjects[tc.project]
			if !ok {
				t.Fatalf("no project named %q", tc.project)
			}
			// The same path for both, so paths in their output agree.
			dir := filepath.Join(t.TempDir(), "project")
			want := runCompiler(t, c.tsc, dir, project, tc.args...)
			if want.exit != tc.exit {
				t.Fatalf("tsc exited %d, not %d: project %q no longer does what this case is for\n%s%s",
					want.exit, tc.exit, tc.project, want.stdout, want.stderr)
			}
			sameAsTsc(t, want, runCompiler(t, c.cakec, dir, project, tc.args...))
		})
	}
}

// TestCakecWatchIsTsc runs --watch through one edit, which takes upstream's
// file watcher and run loop, not just its compiler. Watch mode can recompile
// more often than it was given reason to, so this compares the first
// compilation and the last complete one rather than every one.
func TestCakecWatchIsTsc(t *testing.T) {
	t.Parallel()
	c := tscAndCakec(t)

	dir := filepath.Join(t.TempDir(), "project")
	want := watchThroughAnEdit(t, c.tsc, dir)
	got := watchThroughAnEdit(t, c.cakec, dir)
	if got.first != want.first {
		t.Errorf("the first compilation differs from tsc's:\n%s", firstDifference(want.first, got.first))
	}
	if got.last != want.last {
		t.Errorf("the compilation after the edit differs from tsc's:\n%s", firstDifference(want.last, got.last))
	}
	sameFiles(t, want.files, got.files)
}

// TestCakecLSPIsTsc compares the language servers' answers to initialize,
// which list everything a server can do, and how each exits.
func TestCakecLSPIsTsc(t *testing.T) {
	t.Parallel()
	c := tscAndCakec(t)

	dir := t.TempDir()
	writeProject(t, dir, tscProjects["clean"])
	want, wantExit := lspSession(t, c.tsc, dir)
	got, gotExit := lspSession(t, c.cakec, dir)
	if !strings.Contains(want, `"capabilities"`) {
		t.Fatalf("tsc's initialize result has no capabilities: %s", want)
	}
	if got != want {
		t.Errorf("cakec's initialize result differs from tsc's:\n  tsc:   %s\n  cakec: %s", want, got)
	}
	if gotExit != wantExit {
		t.Errorf("cakec's server exited %d after shutdown, tsc's %d", gotExit, wantExit)
	}
}

// lspSession opens and closes a session with bin's language server, and
// returns the server's answer to initialize and its exit code.
func lspSession(t *testing.T, bin, dir string) (initialize string, exit int) {
	t.Helper()
	s := startLSP(t, bin, dir)
	initialize = string(s.initialize(dir))
	return initialize, s.close()
}

type compilers struct{ tsc, cakec string }

var (
	compilersDir   string
	buildCompilers = sync.OnceValues(func() (compilers, error) {
		dir, err := os.MkdirTemp("", "cakec-parity-")
		if err != nil {
			return compilers{}, err
		}
		compilersDir = dir
		c := compilers{tsc: filepath.Join(dir, "tsc"), cakec: filepath.Join(dir, "cakec")}
		for bin, pkg := range map[string]string{c.tsc: "../tsc", c.cakec: "."} {
			if out, err := exec.Command("go", "build", "-o", bin, pkg).CombinedOutput(); err != nil {
				return compilers{}, fmt.Errorf("go build %s: %w\n%s", pkg, err, out)
			}
		}
		return c, nil
	})
)

// tscAndCakec builds cmd/tsc and cmd/cakec from this tree, once per test run.
func tscAndCakec(t *testing.T) compilers {
	t.Helper()
	c, err := buildCompilers()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMain(m *testing.M) {
	code := m.Run()
	if compilersDir != "" {
		_ = os.RemoveAll(compilersDir)
	}
	os.Exit(code)
}

// tscResult is everything a compiler run can be seen to do.
type tscResult struct {
	stdout, stderr string
	exit           int
	files          map[string]string
}

// runCompiler lays project down fresh at dir, runs bin there, and reports
// what it did.
func runCompiler(t *testing.T, bin, dir string, project map[string]string, args ...string) tscResult {
	t.Helper()
	writeProject(t, dir, project)
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var r tscResult
	err := cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		r.exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running %s: %v", bin, err)
	}
	r.stdout, r.stderr = withoutClock(stdout.String()), withoutClock(stderr.String())
	r.files = readProject(t, dir)
	return r
}

type watchResult struct {
	first, last string
	files       map[string]string
}

func watchThroughAnEdit(t *testing.T, bin, dir string) watchResult {
	t.Helper()
	writeProject(t, dir, tscProjects["clean"])
	var out syncBuffer
	cmd := exec.Command(bin, "--watch")
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stop := sync.OnceFunc(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	defer stop()

	const watching = "Watching for file changes."
	waitForOutput(t, &out, "the first compilation", func(s string) bool {
		return strings.Contains(s, "Found 0 errors. "+watching)
	})
	// Written beside the project and renamed into place: os.WriteFile
	// truncates first, and a watcher that reads the file in between compiles
	// an empty module.
	edit := filepath.Join(filepath.Dir(dir), "lib.ts")
	if err := os.WriteFile(edit, []byte("export function double(n: number): number { return \"twice\"; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(edit, filepath.Join(dir, "src", "lib.ts")); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, &out, "the edit's type error", func(s string) bool {
		i := strings.LastIndex(s, "error TS2322")
		return i >= 0 && strings.Contains(s[i:], "Found 1 error. "+watching)
	})
	stop()

	text := withoutClock(out.String())
	end := strings.LastIndex(text, watching) + len(watching)
	start := strings.LastIndex(text[:end], "File change detected")
	if start < 0 {
		t.Fatalf("%s: no recompilation after the edit:\n%s", bin, text)
	}
	return watchResult{
		first: text[:strings.Index(text, watching)+len(watching)],
		last:  text[start:end],
		files: readProject(t, dir),
	}
}

// waitForOutput waits up to 30s for done to hold of everything written to out.
func waitForOutput(t *testing.T, out *syncBuffer, what string, done func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !done(out.String()) {
		if time.Now().After(deadline) {
			t.Fatalf("no sign of %s within 30s; the output so far:\n%s", what, out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func sameAsTsc(t *testing.T, want, got tscResult) {
	t.Helper()
	if got.exit != want.exit {
		t.Errorf("exit code %d, tsc's is %d", got.exit, want.exit)
	}
	if got.stdout != want.stdout {
		t.Errorf("stdout differs from tsc's:\n%s", firstDifference(want.stdout, got.stdout))
	}
	if got.stderr != want.stderr {
		t.Errorf("stderr differs from tsc's:\n%s", firstDifference(want.stderr, got.stderr))
	}
	sameFiles(t, want.files, got.files)
}

func sameFiles(t *testing.T, want, got map[string]string) {
	t.Helper()
	paths := slices.Sorted(maps.Keys(want))
	for path := range got {
		if _, ok := want[path]; !ok {
			paths = append(paths, path)
		}
	}
	for _, path := range paths {
		w, inWant := want[path]
		g, inGot := got[path]
		switch {
		case !inGot:
			t.Errorf("tsc left %s, cakec didn't", path)
		case !inWant:
			t.Errorf("cakec left %s, tsc didn't", path)
		case g != w:
			t.Errorf("%s differs from tsc's:\n%s", path, firstDifference(w, g))
		}
	}
}

// firstDifference shows the first line where got departs from want.
func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	line := func(lines []string, i int) string {
		if i < len(lines) {
			return fmt.Sprintf("%q", lines[i])
		}
		return "(end)"
	}
	for i := range max(len(w), len(g)) {
		if line(w, i) != line(g, i) {
			return fmt.Sprintf("line %d:\n  tsc:   %s\n  cakec: %s", i+1, line(w, i), line(g, i))
		}
	}
	return "(no line differs)"
}

// clockTime matches the times --watch and --build --verbose print, which
// differ between two runs a second apart. No \b: --watch prints its time
// straight after a screen clear's "\x1b[H", and H and 1 are both word
// characters.
var clockTime = regexp.MustCompile(`\d{1,2}:\d{2}:\d{2}(\s?[AP]M)?`)

func TestWithoutClock(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"\x1b[2J\x1b[3J\x1b[H11:06:03 AM - Starting compilation in watch mode...": "\x1b[2J\x1b[3J\x1b[H<time> - Starting compilation in watch mode...",
		"9:58:00 PM - Projects in this build:":                                    "<time> - Projects in this build:",
		"[23:05:59] Found 0 errors.":                                              "[<time>] Found 0 errors.",
		"src/main.ts(1,7): error TS2322":                                          "src/main.ts(1,7): error TS2322",
	} {
		if got := withoutClock(in); got != want {
			t.Errorf("withoutClock(%q) = %q, want %q", in, got, want)
		}
	}
}

func withoutClock(s string) string { return clockTime.ReplaceAllString(s, "<time>") }

func writeProject(t *testing.T, dir string, project map[string]string) {
	t.Helper()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range project {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// readProject returns every file under dir, by slash-separated relative path.
func readProject(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
