package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMultiModuleProjectBuilds: a project whose main imports another module
// builds into one binary that runs both.
func TestMultiModuleProjectBuilds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, map[string]string{
		"tsconfig.json": `{ "compilerOptions": { "strict": true }, "include": ["src"], "cakebear": { "main": "src/main.ts" } }`,
		"src/lib.ts":    "export function double(n: number): number {\n  return n * 2;\n}\n",
		"src/main.ts":   "import { double } from \"./lib\";\nconsole.log(double(21));\n",
	})
	if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	got, err := exec.Command(filepath.Join(dir, "main")).Output()
	if err != nil {
		t.Fatalf("running the binary: %v", err)
	}
	if string(got) != "42\n" {
		t.Errorf("the binary printed %q, want %q", got, "42\n")
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
