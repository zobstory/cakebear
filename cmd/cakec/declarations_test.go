package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/internal/bundled"
	"github.com/zobstory/cakebear/internal/tspath"
	"github.com/zobstory/cakebear/internal/vfs/osvfs"
)

// M3 plan D4 and check 8: cakebear's declarations in tsc mode, for a project
// with a "cakebear" block, and always in the language server.

// usesCakebear needs every one of cakebear's declarations files: an i32, a
// spawn and node:http. tsc alone reports each as unknown.
const usesCakebear = `import { createServer } from "node:http";
const n: i32 = i32(41);
export const p: Promise<i32> = spawn(() => n);
createServer((req, res) => {
  res.end(req.url);
});
`

// unknownToTsc are the codes tsc gives usesCakebear: TS2304 for i32 and spawn,
// TS2591 for node:http, TS7006 for the handler's untyped parameters.
var unknownToTsc = []string{"2304", "2591", "7006"}

func TestDeclaresCakebear(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, map[string]string{
		"app/tsconfig.json":   `{ "include": ["src"], "cakebear": { "main": "src/main.ts" } }`,
		"app/src/main.ts":     "",
		"plain/tsconfig.json": `{ "include": ["src"] }`,
		"plain/src/main.ts":   "",
		"based/base.json":     `{ "cakebear": { "main": "src/main.ts" } }`,
		"based/tsconfig.json": `{ "extends": "./base.json", "include": ["src"] }`,
		"none/main.ts":        "",
	})
	fs := bundled.WrapFS(osvfs.FS())
	at := func(sub string) tspath.RootedDirectoryPath {
		return tspath.RootedDirectoryPathFromAbsolute(tspath.NormalizePath(filepath.Join(dir, sub)))
	}

	for _, tt := range []struct {
		name string
		cwd  string
		args []string
		want bool
	}{
		{"a block in the working directory's config", "app", nil, true},
		{"no block", "plain", nil, false},
		{"the nearest config above", "app/src", nil, true},
		{"-p a directory", "", []string{"-p", "app"}, true},
		{"-p a file", "plain", []string{"-p", "../app/tsconfig.json"}, true},
		{"-p a config without one", "app", []string{"-p", "../plain"}, false},
		{"--watch", "app", []string{"--watch"}, true},
		{"files with --ignoreConfig", "app", []string{"--ignoreConfig", "src/main.ts"}, false},
		{"a block only in a base config", "based", nil, false},
		{"no config at all", "none", []string{"main.ts"}, false},
		{"--build in the working directory", "app", []string{"--build"}, true},
		{"-b naming a project with one", "", []string{"-b", "plain", "app"}, true},
		{"-b naming none with one", "", []string{"-b", "plain"}, false},
	} {
		if got := declaresCakebear(tt.args, fs, at(tt.cwd)); got != tt.want {
			t.Errorf("%s: declaresCakebear(%q) in %s = %v, want %v", tt.name, tt.args, tt.cwd, got, tt.want)
		}
	}
}

// TestCakecSeesDeclarationsWithABlock runs tsc mode on a project with a
// "cakebear" block: cakec type-checks usesCakebear clean where tsc can't, in a
// plain build and in --build, and emits the same JavaScript.
func TestCakecSeesDeclarationsWithABlock(t *testing.T) {
	t.Parallel()
	c := tscAndCakec(t)
	project := map[string]string{
		"tsconfig.json": `{ "compilerOptions": { "strict": true }, "include": ["src"], "cakebear": { "main": "src/main.ts" } }`,
		"src/main.ts":   usesCakebear,
	}

	for _, args := range [][]string{nil, {"-b"}} {
		t.Run(strings.Join(append([]string{"cakec"}, args...), " "), func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "project")
			tsc := runCompiler(t, c.tsc, dir, project, args...)
			for _, code := range unknownToTsc {
				if !strings.Contains(tsc.stdout, "TS"+code) {
					t.Errorf("tsc doesn't report TS%s, so this proves nothing:\n%s", code, tsc.stdout)
				}
			}
			got := runCompiler(t, c.cakec, dir, project, args...)
			if got.exit != 0 || got.stdout != "" || got.stderr != "" {
				t.Errorf("cakec exited %d:\n%s%s", got.exit, got.stdout, got.stderr)
			}
			if got.files["src/main.js"] == "" || got.files["src/main.js"] != tsc.files["src/main.js"] {
				t.Errorf("the emitted src/main.js differs from tsc's:\n%s", firstDifference(tsc.files["src/main.js"], got.files["src/main.js"]))
			}
		})
	}
}

// TestLSPSeesCakebearDeclarations (check 8): cakec's language server knows
// cakebear's names in a project with no "cakebear" block, where tsc's server
// reports them unknown, and go-to-definition on spawn lands in the bundled lib
// carrying them.
func TestLSPSeesCakebearDeclarations(t *testing.T) {
	t.Parallel()
	c := tscAndCakec(t)

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeProject(t, dir, map[string]string{
		"tsconfig.json": `{ "compilerOptions": { "strict": true }, "include": ["src"] }`,
		"src/main.ts":   usesCakebear,
	})
	main := filepath.Join(dir, "src", "main.ts")

	unknown := func(bin string) (codes []string, definition []string) {
		s := startLSP(t, bin, dir)
		defer s.close()
		s.initialize(dir)
		s.open(main, usesCakebear)
		for _, d := range s.diagnostics(main) {
			if code := string(d.Code); slices.Contains(unknownToTsc, code) {
				codes = append(codes, code+" "+d.Message)
			}
		}
		// Line 3 is `export const p: Promise<i32> = spawn(() => n);`.
		return codes, s.definition(main, 2, strings.Index(strings.Split(usesCakebear, "\n")[2], "spawn"))
	}

	if codes, _ := unknown(c.tsc); len(codes) == 0 {
		t.Fatal("tsc's server reports none of cakebear's names unknown, so this proves nothing")
	}
	codes, definition := unknown(c.cakec)
	if len(codes) > 0 {
		t.Errorf("cakec's server reports cakebear's names unknown:\n%s", strings.Join(codes, "\n"))
	}
	if want := "bundled:///libs/lib.es5.d.ts"; !slices.Contains(definition, want) {
		t.Errorf("go-to-definition on spawn gives %q, want %s", definition, want)
	}
}
