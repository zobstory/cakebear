package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// compileAndRun builds a TypeScript source to a native binary and runs it,
// returning the program's stdout.
//
// This is the end-to-end path the whole project exists for, so the tests that
// use it are the ones worth trusting when something regresses.
func compileAndRun(t *testing.T, src string) (stdout string, buildOut string, code int) {
	t.Helper()

	dir := t.TempDir()
	tsPath := filepath.Join(dir, "prog.ts")
	if err := os.WriteFile(tsPath, []byte(src), 0o600); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	out := filepath.Join(dir, "prog")

	var stderr bytes.Buffer
	code = runBuild(buildOptions{
		files:  []string{tsPath},
		output: out,
		color:  false,
	}, &stderr)
	if code != exitOK {
		return "", stderr.String(), code
	}

	run := exec.Command(out)
	var runOut bytes.Buffer
	run.Stdout = &runOut
	run.Stderr = &runOut
	if err := run.Run(); err != nil {
		t.Fatalf("running compiled binary: %v\n%s", err, runOut.String())
	}
	return runOut.String(), stderr.String(), exitOK
}

func TestCompileAndRunHelloArithmetic(t *testing.T) {
	t.Parallel()

	stdout, buildOut, code := compileAndRun(t, `
function add(a: number, b: number): number {
  return a + b;
}
const x: number = add(2, 3);
console.log(x);
`)

	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	if stdout != "5\n" {
		t.Errorf("program printed %q, want %q", stdout, "5\n")
	}
}

func TestCompileControlFlowAndRecursion(t *testing.T) {
	t.Parallel()

	stdout, buildOut, code := compileAndRun(t, `
function fib(n: number): number {
  if (n < 2) {
    return n;
  }
  return fib(n - 1) + fib(n - 2);
}
let i: number = 0;
while (i < 6) {
  console.log(fib(i));
  i = i + 1;
}
`)

	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	if want := "0\n1\n1\n2\n3\n5\n"; stdout != want {
		t.Errorf("program printed %q, want %q", stdout, want)
	}
}

// TestNumberPrintingMatchesJavaScript is the one that catches semantic drift.
//
// Every expectation is what Node prints, including the two places Go would
// disagree if left to itself: 1e-7 (Go pads the exponent to 1e-07) and -0
// (Go's own float formatting drops the sign in a constant, and its constant
// arithmetic cannot express the value at all).
func TestNumberPrintingMatchesJavaScript(t *testing.T) {
	t.Parallel()

	stdout, buildOut, code := compileAndRun(t, `
console.log(1 / 3);
console.log(5 / 2);
console.log(-0);
console.log(1e21);
console.log(0.0000001);
console.log(100);
`)

	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	want := "0.3333333333333333\n2.5\n-0\n1e+21\n1e-7\n100\n"
	if stdout != want {
		t.Errorf("number printing drifted from JavaScript:\ngot  %q\nwant %q", stdout, want)
	}
}

func TestCompileStringsAndBooleans(t *testing.T) {
	t.Parallel()

	stdout, buildOut, code := compileAndRun(t, `
const greeting: string = "hello" + " " + "world";
console.log(greeting);
console.log(true && !false);
console.log("a" === "a");
`)

	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	if want := "hello world\ntrue\ntrue\n"; stdout != want {
		t.Errorf("program printed %q, want %q", stdout, want)
	}
}

// Identifiers that are Go keywords must survive, and Unicode identifiers must
// pass through untouched — the latter being something the Rust lexer could
// never have parsed in the first place.
func TestCompileManglesKeywordsAndKeepsUnicode(t *testing.T) {
	t.Parallel()

	stdout, buildOut, code := compileAndRun(t, `
function type(func: number): number {
  return func * 2;
}
const range: number = type(21);
const café: string = "unicode";
console.log(range);
console.log(café);
`)

	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	if want := "42\nunicode\n"; stdout != want {
		t.Errorf("program printed %q, want %q", stdout, want)
	}
}

// A program that never logs must still compile: the runtime import has to be
// omitted, because Go rejects an unused import.
func TestCompileProgramWithNoOutput(t *testing.T) {
	t.Parallel()

	stdout, buildOut, code := compileAndRun(t, "const x: number = 1;\n")

	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	if stdout != "" {
		t.Errorf("program printed %q, want nothing", stdout)
	}
}

// Unsupported constructs are refused with a span and a name, and all of them
// are collected in one run rather than stopping at the first.
func TestUnsupportedConstructsAreNamed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "unsupported.ts")
	src := "class Cake { }\nconst nums: number[] = [1];\nconst s: string = \"n=\" + 1;\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	var stderr bytes.Buffer
	code := runBuild(buildOptions{files: []string{path}, output: filepath.Join(dir, "out"), color: false}, &stderr)

	if code != exitErrors {
		t.Errorf("exit code = %d, want %d", code, exitErrors)
	}
	out := stderr.String()
	for _, want := range []string{"class declaration", "cannot represent yet", "both operands must have the same type"} {
		if !strings.Contains(out, want) {
			t.Errorf("diagnostics missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "unsupported.ts:1:1") {
		t.Errorf("diagnostics missing a source span:\n%s", out)
	}
}

// --check stops after type checking, which is what editors and CI want.
func TestCheckOnlyProducesNoBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "prog.ts")
	if err := os.WriteFile(path, []byte("console.log(1);\n"), 0o600); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	out := filepath.Join(dir, "prog")

	var stderr bytes.Buffer
	if code := runBuild(buildOptions{files: []string{path}, output: out, checkOnly: true, color: false}, &stderr); code != exitOK {
		t.Fatalf("--check failed (%d):\n%s", code, stderr.String())
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("--check produced a binary; it should only type-check")
	}
}

func TestParseTarget(t *testing.T) {
	t.Parallel()

	goos, goarch, err := parseTarget("linux/amd64")
	if err != nil {
		t.Fatalf("parseTarget: %v", err)
	}
	if goos != "linux" || goarch != "amd64" {
		t.Errorf("parseTarget = %q/%q, want linux/amd64", goos, goarch)
	}

	for _, bad := range []string{"linux", "", "/amd64", "linux/"} {
		if _, _, err := parseTarget(bad); err == nil {
			t.Errorf("parseTarget(%q) succeeded, want an error", bad)
		}
	}
}

// The extension types exist so the backend can use a native machine type
// instead of float64, and this proves the whole path: brand recognised,
// lowered to int32/int64/uint32/string, printed as the integers they are.
func TestCompileExtensionTypes(t *testing.T) {
	t.Parallel()

	stdout, buildOut, code := compileAndRun(t, `
const small: i32 = i32(42);
const big: i64 = i64(9007199254740991);
const un: u32 = u32(4294967295);
const enc: base64 = base64("Y2FrZWJlYXI=");
console.log(small);
console.log(big);
console.log(un);
console.log(enc);
`)

	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	want := "42\n9007199254740991\n4294967295\nY2FrZWJlYXI=\n"
	if stdout != want {
		t.Errorf("program printed %q, want %q", stdout, want)
	}
}

// A call returning an extension type is a call, not a conversion.
//
// Resolving conversions by result type instead of declaration site silently
// replaced twice(small) with int32(small), so the program printed its input
// back. This is the regression test for that.
func TestCallReturningExtensionIsNotAConversion(t *testing.T) {
	t.Parallel()

	stdout, buildOut, code := compileAndRun(t, `
function twice(n: i32): i32 {
  return i32(n + n);
}
console.log(twice(i32(21)));
`)

	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	if stdout != "42\n" {
		t.Errorf("program printed %q, want %q — the call was replaced by a cast", stdout, "42\n")
	}
}

// Brand enforcement is TypeScript's own checker doing the work, with no edit
// inside internal/checker. That is the whole reason for choosing branded types.
func TestExtensionBrandsAreEnforcedByTheChecker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "brand.ts")
	if err := os.WriteFile(path, []byte("const x: i32 = 5;\nconst s: base64 = \"plain\";\n"), 0o600); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	var stderr bytes.Buffer
	code := runBuild(buildOptions{files: []string{path}, output: filepath.Join(dir, "out"), color: false}, &stderr)

	if code != exitErrors {
		t.Errorf("exit code = %d, want %d", code, exitErrors)
	}
	out := stderr.String()
	// TS2322 is the checker's own "not assignable" diagnostic.
	if !strings.Contains(out, "TS2322") {
		t.Errorf("expected the checker to reject an unbranded value:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
		t.Error("a binary was produced despite type errors")
	}
}

// Out-of-range literals are permanent errors, not phase limitations, and the
// diagnostics have to say so differently — one asks the reader to change their
// code, the other to wait for a release.
func TestInvalidLiteralsReadAsErrorsNotLimitations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.ts")
	src := "const a: i32 = i32(3000000000);\nconst b: base64 = base64(\"!!!\");\nclass Later { }\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	var stderr bytes.Buffer
	if code := runBuild(buildOptions{files: []string{path}, output: filepath.Join(dir, "out"), color: false}, &stderr); code != exitErrors {
		t.Errorf("exit code = %d, want %d", code, exitErrors)
	}

	out := stderr.String()
	if !strings.Contains(out, "error: 3000000000 is outside the range of i32") {
		t.Errorf("range failure did not read as an error:\n%s", out)
	}
	if !strings.Contains(out, "error: \"!!!\" is not valid base64") {
		t.Errorf("base64 failure did not read as an error:\n%s", out)
	}
	if !strings.Contains(out, "cannot compile yet: class declaration") {
		t.Errorf("phase limitation did not read as one:\n%s", out)
	}
	// Counted separately, because they mean opposite things.
	if !strings.Contains(out, "2 errors") || !strings.Contains(out, "1 construct") {
		t.Errorf("the two kinds were not tallied separately:\n%s", out)
	}
}
