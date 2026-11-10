package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestUncaughtReportsNameTheDeclaringFile (M7 R26): an error thrown in a
// function a module re-exports is reported at the declaring file's line,
// whatever path the call took. Called at the top level through
// `export { boom } from`, the report points at the Error's creation, as
// Node's does without --stack-trace-limit=0 (under it Node points into its
// own loader); from a timer, through `export *`, at the throw, byte for byte
// Node's under --stack-trace-limit=0 but for the last line. Both recorded
// from Node 24.18, 2026-11-10.
func TestUncaughtReportsNameTheDeclaringFile(t *testing.T) {
	t.Parallel()

	lib := map[string]string{
		"lib.ts":  "export function boom(): never {\n  throw new Error(\"boom from lib\");\n}\n",
		"re.ts":   "export { boom } from \"./lib.ts\";\n",
		"star.ts": "export * from \"./lib.ts\";\n",
	}
	for _, tt := range []struct {
		name, main, caret string
	}{
		{"at the top level through export from", "import { boom } from \"./re.ts\";\nconsole.log(\"start\");\nboom();\n", "        ^"},
		{"in a timer through export star", "import { boom } from \"./star.ts\";\nconsole.log(\"start\");\nsetTimeout(() => {\n  boom();\n}, 0);\n", "  ^"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"main.ts": tt.main}
			for k, v := range lib {
				files[k] = v
			}
			dir := t.TempDir()
			writeProject(t, dir, moduleProject(files))
			if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
				t.Fatalf("build: exit %d\n%s", code, stderr)
			}
			path := filepath.Join(dir, "src", "lib.ts")
			if !plainPath.MatchString(path) {
				t.Skipf("%s needs escaping in a file URL", path)
			}
			var stdout, stderr strings.Builder
			cmd := exec.Command(filepath.Join(dir, "app"))
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			var exit *exec.ExitError
			if err := cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("%v, want exit 1; stderr:\n%s", err, stderr.String())
			}
			if stdout.String() != "start\n" {
				t.Errorf("stdout %q, want %q", stdout.String(), "start\n")
			}
			want := "file://" + path + ":2\n  throw new Error(\"boom from lib\");\n" + tt.caret + "\n\n[Error: boom from lib]\n\ncakebear " + version + "\n"
			if got := stderr.String(); got != want {
				t.Errorf("stderr is\n%s\nwant, as Node's report reads\n%s", got, want)
			}
		})
	}
}
