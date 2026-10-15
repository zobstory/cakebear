package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Bugs M4's re-measurement of wx-next found (check 9), each fixed and held
// end to end here.

// unannotatedProgram: functions with no result type, returning a number, an
// object, a string, an array, and nothing. Each was lowered with a void
// result, and the program failed inside go build ("too many return
// values"). Node 24.18 ran this same file and printed nodeUnannotated, the
// same in 5 of 5 runs.
const unannotatedProgram = `export {};
function one() {
  return 1;
}
function point(x: number) {
  return { x: x, y: x * 2 };
}
function label(n: number) {
  return ` + "`" + `n=${n}` + "`" + `;
}
function nothing() {
  console.log("side effect");
}
function pick(xs: number[]) {
  return xs.filter((x: number): boolean => x > one());
}
console.log(one(), point(2), label(one()), pick([0, 1, 2, 3]));
nothing();
`

const nodeUnannotated = "1 { x: 2, y: 4 } n=1 [ 2, 3 ]\nside effect\n"

func TestUnannotatedFunctionsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "unannotated", unannotatedProgram), nodeUnannotated, 20)
}

// TestExtensionTypesWidenToNumbers: an i32 or a u64 filling a number's slot,
// a variable, an argument or a result, is converted to a number, where each
// failed inside go build ("cannot use x (variable of type int32) as float64
// value"). Node can't run cakebear's extension types, so the output is what
// JavaScript would print for the same numbers.
func TestExtensionTypesWidenToNumbers(t *testing.T) {
	t.Parallel()

	bin := buildProgram(t, "widen", `export {};
function f(x: i32): number {
  return x;
}
function g(n: number): number {
  return n * 2;
}
const i: i32 = i32(3);
const n: number = i;
const big: u64 = u64(5);
console.log(f(i32(7)), g(i), n, g(big), n / 2);
`)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "7 6 3 10 1.5\n"; string(out) != want {
		t.Errorf("printed %q, want %q", out, want)
	}
}

// packageProject is a project whose entry imports types from a package,
// whose declarations are in node_modules, and one module of JavaScript.
func packageProject(main string) map[string]string {
	return map[string]string{
		"tsconfig.json": `{ "compilerOptions": { "strict": true, "noEmit": true, "allowJs": true, "module": "esnext", "moduleResolution": "bundler" }, ` +
			`"include": ["src"], "cakebear": { "main": "` + main + `", "output": "app" } }`,
		"node_modules/pkg/package.json": `{ "name": "pkg", "version": "1.0.0", "types": "index.d.ts" }`,
		"node_modules/pkg/index.d.ts":   "export interface Shape { readonly id: number; readonly name: string }\nexport declare function make(): Shape;\n",
		"src/main.ts":                   "import type { Shape } from \"pkg\";\nfunction describe(s: Shape): string {\n  return `${s.id}:${s.name}`;\n}\nconsole.log(describe({ id: 1, name: \"a\" }));\n",
		"src/js.ts":                     "import { twice } from \"./helper.mjs\";\nconsole.log(twice(2));\n",
		"src/helper.mjs":                "export function twice(n) {\n  return n * 2;\n}\n",
	}
}

// TestImportTypeFromAPackage: an import of types alone evaluates nothing, so
// it may name a package; it was refused, because where it was from was
// checked before whether it evaluates anything.
func TestImportTypeFromAPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, packageProject("src/main.ts"))
	if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	out, err := exec.Command(filepath.Join(dir, "app")).Output()
	if err != nil || string(out) != "1:a\n" {
		t.Errorf("printed %q (%v), want %q", out, err, "1:a\n")
	}
}

// TestJavaScriptImportRefused: a relative import of a JavaScript module is
// refused by name; it was lowered as TypeScript, and failed on its untyped
// parameters, at their spans in the .mjs file.
func TestJavaScriptImportRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, packageProject("src/js.ts"))
	code, stderr := projectBuild(t, dir, buildOptions{})
	for _, want := range []string{
		"js.ts:1:1: cannot compile yet: helper.mjs is a JavaScript module; cakebear compiles TypeScript",
		"js.ts:2:13: cannot compile yet: twice is declared in helper.mjs, a JavaScript module, which cakebear doesn't compile",
	} {
		if code != exitErrors || !strings.Contains(stderr, want) {
			t.Errorf("exit %d, want %d with\n%s\ngot:\n%s", code, exitErrors, want, stderr)
		}
	}
	if strings.Contains(stderr, "helper.mjs:") {
		t.Errorf("a refusal points into the JavaScript module, which was lowered:\n%s", stderr)
	}
}
