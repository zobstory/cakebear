package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/internal/bundled"
	"github.com/zobstory/cakebear/internal/vfs/osvfs"
	cakebeartypes "github.com/zobstory/cakebear/types"
)

// M3 plan check 3: `cakec build` builds the "cakebear" block's main into its
// output, using the tsconfig.json's files and compiler options.

// projectMain uses cakebear's own types, so a project build that lost its
// declarations would fail to type-check.
const projectMain = `function double(n: i32): i32 {
  return i32(n + n);
}
console.log(double(i32(21)));
`

// sampleProject has a module to build and one it never imports, under block.
func sampleProject(block string) map[string]string {
	return map[string]string{
		"tsconfig.json": `{ "compilerOptions": { "strict": true }, "include": ["src"], "cakebear": ` + block + ` }`,
		"src/main.ts":   projectMain,
		"src/unused.ts": "export const n: number = 1;\n",
	}
}

// TestNativeBuildFromTsconfig runs `cakec build` with no arguments in a
// directory below a project's, which finds the tsconfig.json above it as tsc
// does, and builds main into the block's output.
func TestNativeBuildFromTsconfig(t *testing.T) {
	t.Parallel()
	c := tscAndCakec(t)

	dir := t.TempDir()
	writeProject(t, dir, sampleProject(`{ "main": "src/main.ts", "output": "bin/app" }`))
	cmd := exec.Command(c.cakec, "build")
	cmd.Dir = filepath.Join(dir, "src")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cakec build: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "tsconfig.json type-checks clean") {
		t.Errorf("output doesn't name the project:\n%s", out)
	}
	got, err := exec.Command(filepath.Join(dir, "bin", "app")).Output()
	if err != nil {
		t.Fatalf("running the output: %v", err)
	}
	if string(got) != "42\n" {
		t.Errorf("the binary printed %q, want %q", got, "42\n")
	}
}

// projectBuild builds the project at path in process.
func projectBuild(t *testing.T, path string, opts buildOptions) (int, string) {
	t.Helper()
	opts.project = path
	var stderr bytes.Buffer
	code := runBuild(opts, &stderr)
	return code, stderr.String()
}

// TestBuildProjectFlag builds with -p naming the directory and then the file.
// With no output in the block, the binary is main's basename beside the
// config.
func TestBuildProjectFlag(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, sampleProject(`{ "main": "src/main.ts" }`))
	bin := filepath.Join(dir, "main")
	for _, path := range []string{dir, filepath.Join(dir, "tsconfig.json")} {
		if code, stderr := projectBuild(t, path, buildOptions{}); code != exitOK {
			t.Fatalf("-p %s: exit %d\n%s", path, code, stderr)
		}
		got, err := exec.Command(bin).Output()
		if err != nil {
			t.Fatalf("-p %s: running %s: %v", path, bin, err)
		}
		if string(got) != "42\n" {
			t.Errorf("-p %s: the binary printed %q, want %q", path, got, "42\n")
		}
		if err := os.Remove(bin); err != nil {
			t.Fatal(err)
		}
	}
}

// TestProjectUsesItsCompilerOptions: noUnusedLocals, which cakebear's own
// options for loose files don't set, makes an unused local TS6133 in the
// project and nowhere else.
func TestProjectUsesItsCompilerOptions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, map[string]string{
		"tsconfig.json": `{ "compilerOptions": { "noUnusedLocals": true }, "include": ["src"], "cakebear": { "main": "src/main.ts" } }`,
		"src/main.ts":   "function f(): number {\n  const unused: number = 1;\n  return 2;\n}\nconsole.log(f());\n",
	})
	code, stderr := projectBuild(t, dir, buildOptions{checkOnly: true})
	if code != exitErrors || !strings.Contains(stderr, "TS6133") {
		t.Errorf("project build: exit %d, want %d with TS6133:\n%s", code, exitErrors, stderr)
	}

	var loose bytes.Buffer
	if code := runBuild(buildOptions{files: []string{filepath.Join(dir, "src", "main.ts")}, checkOnly: true}, &loose); code != exitOK {
		t.Errorf("the same file alone: exit %d, want %d:\n%s", code, exitOK, loose.String())
	}
}

// TestProjectStrictnessIsPerOption: "strict": false is fine as long as the
// two options cakebear needs are back on.
func TestProjectStrictnessIsPerOption(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := sampleProject(`{ "main": "src/main.ts" }`)
	project["tsconfig.json"] = `{ "compilerOptions": { "strict": false, "strictNullChecks": true, "noImplicitAny": true }, "include": ["src"], "cakebear": { "main": "src/main.ts" } }`
	writeProject(t, dir, project)
	if code, stderr := projectBuild(t, dir, buildOptions{checkOnly: true}); code != exitOK {
		t.Errorf("exit %d, want %d:\n%s", code, exitOK, stderr)
	}
}

func TestProjectErrors(t *testing.T) {
	t.Parallel()

	const include = `"include": ["src"]`
	tests := []struct {
		name     string
		config   string
		extra    map[string]string
		wantCode int
		want     string
	}{
		{"no block", `{ ` + include + ` }`, nil, exitBadArgs, `no "cakebear" block`},
		{"block not an object", `{ ` + include + `, "cakebear": "src/main.ts" }`, nil, exitBadArgs, `"cakebear" must be an object`},
		{"unknown key", `{ ` + include + `, "cakebear": { "main": "src/main.ts", "outptu": "app" } }`, nil, exitBadArgs, `unknown key "outptu"`},
		{"no main", `{ ` + include + `, "cakebear": { "output": "app" } }`, nil, exitBadArgs, `has no "main"`},
		{"main a number", `{ ` + include + `, "cakebear": { "main": 5 } }`, nil, exitBadArgs, `"cakebear.main" must be a non-empty string`},
		{"output a number", `{ ` + include + `, "cakebear": { "main": "src/main.ts", "output": 5 } }`, nil, exitBadArgs, `"cakebear.output" must be a non-empty string`},
		{
			"main outside the project", `{ ` + include + `, "cakebear": { "main": "other/main.ts" } }`,
			map[string]string{"other/main.ts": projectMain}, exitBadArgs, "which is not one of the project's files",
		},
		{
			"main a declaration file", `{ ` + include + `, "cakebear": { "main": "src/types.d.ts" } }`,
			map[string]string{"src/types.d.ts": "declare const x: number;\n"}, exitBadArgs, "not a declaration file",
		},
		{"strict off", `{ "compilerOptions": { "strict": false }, ` + include + `, "cakebear": { "main": "src/main.ts" } }`, nil, exitBadArgs, "cakebear needs strictNullChecks"},
		{"noImplicitAny off", `{ "compilerOptions": { "noImplicitAny": false }, ` + include + `, "cakebear": { "main": "src/main.ts" } }`, nil, exitBadArgs, "cakebear needs noImplicitAny"},
		{
			"block only in a base config", `{ "extends": "./base.json", ` + include + ` }`,
			map[string]string{"base.json": `{ "cakebear": { "main": "src/main.ts" } }`}, exitBadArgs, "a block in a config it extends is not read",
		},
		{"unknown compiler option", `{ "compilerOptions": { "frost": true }, ` + include + `, "cakebear": { "main": "src/main.ts" } }`, nil, exitErrors, "TS5023"},
		{"invalid JSON", `{ ` + include + `, "cakebear": { "main": "src/main.ts" }`, nil, exitErrors, "TS1005"},
		{
			"a type error in a file main never imports", `{ ` + include + `, "cakebear": { "main": "src/main.ts" } }`,
			map[string]string{"src/other.ts": "const n: number = \"x\";\n"}, exitErrors, "TS2322",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			project := map[string]string{"tsconfig.json": tt.config, "src/main.ts": projectMain}
			for path, content := range tt.extra {
				project[path] = content
			}
			writeProject(t, dir, project)
			code, stderr := projectBuild(t, dir, buildOptions{checkOnly: true})
			if code != tt.wantCode || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d, want %d with %q:\n%s", code, tt.wantCode, tt.want, stderr)
			}
		})
	}
}

// TestConfigFileSearch: -p names a config or its directory, relative to the
// working directory; without it, the nearest tsconfig.json at or above the
// working directory is the project, as with tsc.
func TestConfigFileSearch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, map[string]string{"app/tsconfig.json": "{}", "app/src/deep/x.ts": "", "other/x.ts": ""})
	app, other := filepath.Join(dir, "app"), filepath.Join(dir, "other")
	config := filepath.Join(app, "tsconfig.json")

	for _, tt := range []struct {
		name    string
		project string
		cwd     string
		want    string
		wantErr string
	}{
		{name: "in the working directory", cwd: app, want: config},
		{name: "two directories up", cwd: filepath.Join(app, "src", "deep"), want: config},
		{name: "none at or above", cwd: other, wantErr: "missing input file"},
		{name: "-p a directory", project: "app", cwd: dir, want: config},
		{name: "-p a file", project: config, cwd: other, want: config},
		{name: "-p relative to the working directory", project: filepath.Join("..", "app"), cwd: other, want: config},
		{name: "-p a directory without one", project: "other", cwd: dir, wantErr: "-p other: no tsconfig.json there"},
	} {
		got, err := configFile(buildOptions{project: tt.project}, tt.cwd)
		switch {
		case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
			t.Errorf("%s: got %q, %v; want an error containing %q", tt.name, got, err, tt.wantErr)
		case tt.wantErr == "" && (err != nil || got != tt.want):
			t.Errorf("%s: got %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
}

// TestLoadProject: --checkers overrides the config, and cakebear's
// declarations join the project's files.
func TestLoadProject(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, sampleProject(`{ "main": "src/main.ts" }`))
	fs := cakebeartypes.WrapFS(bundled.WrapFS(osvfs.FS()))
	p, diags, err := loadProject(filepath.Join(dir, "tsconfig.json"), buildOptions{checkers: 3}, fs, dir)
	if err != nil || len(diags) > 0 {
		t.Fatalf("loadProject: %v %v", err, diags)
	}
	if n := p.config.CompilerOptions().Checkers; n == nil || *n != 3 {
		t.Errorf("checkers = %v, want 3 from the command line", n)
	}
	for _, root := range cakebeartypes.RootFiles() {
		if !slices.Contains(p.config.FileNames(), root) {
			t.Errorf("the project's files lack %s", root)
		}
	}
	if want := filepath.Join(dir, "src", "main.ts"); p.main != want {
		t.Errorf("main = %q, want %q", p.main, want)
	}
}

func TestRelativeTo(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(string(filepath.Separator)+"work", "app", "src")
	for path, want := range map[string]string{
		filepath.Join(dir, "main.ts"):                                 filepath.Join("main.ts"),
		filepath.Join(dir, "..", "tsconfig.json"):                     filepath.Join("..", "tsconfig.json"),
		filepath.Join(string(filepath.Separator)+"elsewhere", "x.ts"): filepath.Join(string(filepath.Separator)+"elsewhere", "x.ts"),
	} {
		if got := relativeTo(dir, path); got != want {
			t.Errorf("relativeTo(%q, %q) = %q, want %q", dir, path, got, want)
		}
	}
}
