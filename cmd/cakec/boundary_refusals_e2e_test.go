package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeLinks makes each symlink in links under dir, pointing at its target
// as written, relative to the link's own directory.
func writeLinks(t *testing.T, dir string, links map[string]string) {
	t.Helper()
	for link, target := range links {
		full := filepath.Join(dir, filepath.FromSlash(link))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.FromSlash(target), full); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("symlinks need privileges here: %v", err)
			}
			t.Fatal(err)
		}
	}
}

// boundaryConfig is a project's tsconfig.json, in bundler mode, whose main
// is src/main.ts.
func boundaryConfig(paths map[string][]string) string {
	opts := map[string]any{"strict": true, "noEmit": true, "allowImportingTsExtensions": true, "module": "esnext", "moduleResolution": "bundler"}
	if paths != nil {
		opts["paths"] = paths
	}
	b, err := json.Marshal(map[string]any{"compilerOptions": opts, "include": []string{"src"}, "cakebear": map[string]string{"main": "src/main.ts", "output": "app"}})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestBoundaryRefusals (M7 plan D4, D5): what a specifier reaches that is no
// module cakebear compiles is refused by name, with what to do about it.
func TestBoundaryRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		files map[string]string
		links map[string]string
		want  []string
	}{
		{
			"a workspace package typed by its built declarations",
			map[string]string{
				"tsconfig.json":                boundaryConfig(nil),
				"packages/dts/package.json":    `{ "name": "@scope/dts", "type": "module", "exports": { ".": { "types": "./dist/index.d.ts", "default": "./dist/index.js" } } }`,
				"packages/dts/dist/index.d.ts": "export declare function f(): number;\n",
				"packages/dts/dist/index.js":   "export function f() {\n  return 1;\n}\n",
				"packages/dts/src/index.ts":    "export function f(): number {\n  return 1;\n}\n",
				"src/main.ts":                  "import { f } from \"@scope/dts\";\nconsole.log(f());\n",
			},
			map[string]string{"node_modules/@scope/dts": "../../packages/dts"},
			[]string{"main.ts:1:1: cannot compile yet: \"@scope/dts\" resolves to a declaration file, index.d.ts, and cakebear compiles TypeScript source: point the package's `types` at its source, or list a source condition before `types` in its exports (and in `customConditions`)"},
		},
		{
			"TypeScript source really under node_modules",
			map[string]string{
				"tsconfig.json":                 boundaryConfig(nil),
				"node_modules/raw/package.json": `{ "name": "raw", "type": "module", "exports": "./index.ts" }`,
				"node_modules/raw/index.ts":     "export const r: number = 1;\n",
				"src/main.ts":                   "import { r } from \"raw\";\nconsole.log(r);\n",
			},
			nil,
			[]string{"main.ts:1:1: cannot compile yet: \"raw\" is TypeScript source under node_modules, which Node refuses to run (ERR_UNSUPPORTED_NODE_MODULES_TYPE_STRIPPING); link the package from outside node_modules, as a workspace does"},
		},
		{
			"a JSON module",
			map[string]string{
				"tsconfig.json": `{ "compilerOptions": { "strict": true, "noEmit": true, "module": "esnext", "moduleResolution": "bundler", "resolveJsonModule": true }, "include": ["src"], "cakebear": { "main": "src/main.ts", "output": "app" } }`,
				"src/data.json": `{ "n": 1 }`,
				"src/main.ts":   "import data from \"./data.json\";\nconsole.log(data.n);\n",
			},
			nil,
			[]string{"main.ts:1:1: cannot compile yet: data.json is a JSON module, which cakebear doesn't import yet; declare its data as a const in a .ts module"},
		},
		{
			"one file by two paths, one through a symlinked directory",
			map[string]string{
				"tsconfig.json": boundaryConfig(nil),
				"lib/real/x.ts": "console.log(\"x runs\");\nexport const x: number = 1;\n",
				"src/main.ts":   "import { x } from \"../lib/real/x.ts\";\nimport { x as y } from \"../lib/linked/x.ts\";\nconsole.log(x, y);\n",
			},
			map[string]string{"lib/linked": "real"},
			[]string{
				"main.ts:1:1: cannot compile yet: \"../lib/real/x.ts\" reaches ../lib/real/x.ts, and another import reaches the same file as ../lib/linked/x.ts: TypeScript checks one file reached by two paths as two, where Node runs one module; import it by one path",
				"main.ts:2:1: cannot compile yet: \"../lib/linked/x.ts\" reaches ../lib/linked/x.ts, and another import reaches the same file as ../lib/real/x.ts",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeProject(t, dir, tt.files)
			writeLinks(t, dir, tt.links)
			code, stderr := projectBuild(t, dir, buildOptions{})
			for _, want := range tt.want {
				if code != exitErrors || !strings.Contains(stderr, want) {
					t.Errorf("exit %d, want %d with\n%s\ngot:\n%s", code, exitErrors, want, stderr)
				}
			}
		})
	}
}

// TestUncaughtReportsThroughAnAliasAndALink (M7 R26): a throw in a module
// reached through a tsconfig `paths` alias, or through a workspace package's
// node_modules link, is reported at the file that declares it, its line,
// excerpt, caret and error Node's (Node 24.18 under --stack-trace-limit=0,
// with testdata/paths-hook.mjs for the alias, 2026-11-10). For the link the
// path is the package's real path, as TypeScript and Node both take it; for
// the alias it is the source's path as TypeScript has it, where Node writes
// the real one, which differs where a directory above is a symlink (the
// accepted difference for every report: the path at build time).
func TestUncaughtReportsThroughAnAliasAndALink(t *testing.T) {
	t.Parallel()

	const boom = "export function boom(): never {\n  throw new Error(\"boom\");\n}\n"
	const timer = "console.log(\"start\");\nsetTimeout(() => {\n  boom();\n}, 0);\n"
	for _, tt := range []struct {
		name  string
		files map[string]string
		links map[string]string
		lib   string // the declaring file, from the project's root
	}{
		{
			"through a paths alias",
			map[string]string{
				"tsconfig.json":   boundaryConfig(map[string][]string{"@/*": {"./src/*"}}),
				"src/lib/boom.ts": boom,
				"src/main.ts":     "import { boom } from \"@/lib/boom.ts\";\n" + timer,
			},
			nil, "src/lib/boom.ts",
		},
		{
			"through a workspace link",
			map[string]string{
				"tsconfig.json":             boundaryConfig(nil),
				"packages/pkg/package.json": `{ "name": "pkg", "type": "module", "exports": "./src/boom.ts" }`,
				"packages/pkg/src/boom.ts":  boom,
				"src/main.ts":               "import { boom } from \"pkg\";\n" + timer,
			},
			map[string]string{"node_modules/pkg": "../packages/pkg"},
			"packages/pkg/src/boom.ts",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeProject(t, dir, tt.files)
			writeLinks(t, dir, tt.links)
			if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
				t.Fatalf("build: exit %d\n%s", code, stderr)
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
			path := filepath.Join(dir, filepath.FromSlash(tt.lib))
			if tt.links != nil {
				// TypeScript takes a package reached through node_modules at
				// its real path, as Node does.
				resolved, err := filepath.EvalSymlinks(path)
				if err != nil {
					t.Fatal(err)
				}
				path = resolved
			}
			if !plainPath.MatchString(path) {
				t.Skipf("%s needs escaping in a file URL", path)
			}
			want := "file://" + path + ":2\n  throw new Error(\"boom\");\n  ^\n\n[Error: boom]\n\ncakebear " + version + "\n"
			if got := stderr.String(); got != want {
				t.Errorf("stderr is\n%s\nwant, as Node's report reads\n%s", got, want)
			}
		})
	}
}
