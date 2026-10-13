package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestMultiModuleProjectReachesTheBackend: a project whose main imports
// another module type-checks and lowers, so the only thing in the way is the
// backend, which compiles one module until M3 step 6.
func TestMultiModuleProjectReachesTheBackend(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, map[string]string{
		"tsconfig.json": `{ "compilerOptions": { "strict": true }, "include": ["src"], "cakebear": { "main": "src/main.ts" } }`,
		"src/lib.ts":    "export function double(n: number): number {\n  return n * 2;\n}\n",
		"src/main.ts":   "import { double } from \"./lib\";\nconsole.log(double(21));\n",
	})
	code, stderr := projectBuild(t, dir, buildOptions{})
	if code != exitErrors || !strings.Contains(stderr, "the backend compiles one module so far, and this program has 2") {
		t.Errorf("exit %d, want %d from the backend:\n%s", code, exitErrors, stderr)
	}
	if strings.Contains(stderr, "cannot compile yet") || strings.Contains(stderr, "library function") {
		t.Errorf("lowering refused part of the program:\n%s", stderr)
	}
}

// TestBuildNamesOneEntry: a binary runs one module, which brings in what it
// imports, so naming two files is a question cakec can't answer.
func TestBuildNamesOneEntry(t *testing.T) {
	t.Parallel()

	a, b := writeTS(t, "a.ts", "console.log(1);\n"), writeTS(t, "b.ts", "console.log(2);\n")
	var stderr bytes.Buffer
	code := runBuild(buildOptions{files: []string{a, b}, output: filepath.Join(t.TempDir(), "out")}, &stderr)
	if code != exitBadArgs || !strings.Contains(stderr.String(), "name one file, the module the binary runs") {
		t.Errorf("exit %d, want %d naming one file:\n%s", code, exitBadArgs, stderr.String())
	}
}
