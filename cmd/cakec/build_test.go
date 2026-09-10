package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTS drops a TypeScript file into a fresh temp dir and returns its path.
func writeTS(t *testing.T, name, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// build runs the whole pipeline the way the CLI does, returning exit code and
// the diagnostics text. Colour is off so assertions match plain strings.
func build(t *testing.T, path string) (int, string) {
	t.Helper()
	var stderr bytes.Buffer
	code := runBuild(buildOptions{files: []string{path}, color: false, checkOnly: true}, &stderr)
	return code, stderr.String()
}

func TestBuildCleanProgram(t *testing.T) {
	t.Parallel()

	path := writeTS(t, "main.ts", `
function add(a: number, b: number): number {
  return a + b;
}
const x: number = add(2, 3);
console.log(x);
`)

	code, out := build(t, path)

	if code != exitOK {
		t.Errorf("exit code = %d, want %d\n%s", code, exitOK, out)
	}
	if !strings.Contains(out, "type-checks clean") {
		t.Errorf("output = %q, want it to report a clean check", out)
	}
}

func TestBuildReportsTypeErrors(t *testing.T) {
	t.Parallel()

	path := writeTS(t, "bad.ts", `
function add(a: number, b: number): number {
  return a + b;
}
const x: number = add(2, "three");
const y: string = 42;
`)

	code, out := build(t, path)

	if code != exitErrors {
		t.Errorf("exit code = %d, want %d", code, exitErrors)
	}
	// TS2345: bad argument type. TS2322: bad assignment.
	for _, want := range []string{"TS2345", "TS2322", "cakec: 2 errors"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// A parse failure makes the tree unreliable, so semantic diagnostics derived
// from it are noise. collectDiagnostics suppresses them; this pins that down.
func TestBuildSuppressesSemanticsAfterSyntaxError(t *testing.T) {
	t.Parallel()

	path := writeTS(t, "syntax.ts", "const x: number = ;\nconst y: string = 42;\n")

	code, out := build(t, path)

	if code != exitErrors {
		t.Errorf("exit code = %d, want %d", code, exitErrors)
	}
	if !strings.Contains(out, "TS1109") {
		t.Errorf("output missing the syntax error TS1109:\n%s", out)
	}
	if strings.Contains(out, "TS2322") {
		t.Errorf("semantic diagnostic TS2322 leaked past a syntax error:\n%s", out)
	}
}

// The Rust lexer accepted ASCII identifiers only. Upstream's scanner handles
// the full Unicode identifier grammar, and this is the regression test for the
// capability the fork bought us.
func TestBuildAcceptsUnicodeIdentifiers(t *testing.T) {
	t.Parallel()

	path := writeTS(t, "unicode.ts", "const café: string = \"ok\";\nconst 日本: number = 1;\nconsole.log(café);\nconsole.log(日本);\n")

	code, out := build(t, path)

	if code != exitOK {
		t.Errorf("exit code = %d, want %d\n%s", code, exitOK, out)
	}
}

// Strict mode is cakebear's default because the backend cannot pin down a
// representation for implicit any. This asserts the default is actually on,
// rather than inherited from whatever tsc would do.
func TestBuildDefaultsToStrict(t *testing.T) {
	t.Parallel()

	path := writeTS(t, "implicit.ts", "function f(x) { return x; }\nf(1);\n")

	code, out := build(t, path)

	if code != exitErrors {
		t.Errorf("exit code = %d, want %d — strict should reject implicit any\n%s", code, exitErrors, out)
	}
	if !strings.Contains(out, "TS7006") {
		t.Errorf("output missing implicit-any diagnostic TS7006:\n%s", out)
	}
}

func TestBuildMissingFile(t *testing.T) {
	t.Parallel()

	code, out := build(t, filepath.Join(t.TempDir(), "nope.ts"))

	if code != exitBadArgs {
		t.Errorf("exit code = %d, want %d", code, exitBadArgs)
	}
	if !strings.Contains(out, "no such file") {
		t.Errorf("output = %q, want a plain not-found error", out)
	}
}

func TestDefaultCompilerOptions(t *testing.T) {
	t.Parallel()

	opts := defaultCompilerOptions()

	if !opts.Strict.IsTrue() {
		t.Error("Strict is not true; cakebear compiles AOT and needs the stricter type surface")
	}
	if !opts.NoEmit.IsTrue() {
		t.Error("NoEmit is not true; upstream's emit produces JavaScript, which cakebear replaces")
	}
}
