package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Every program in examples/ compiles to a binary, so the examples the docs
// point at cannot quietly stop working: a main.ts on its own, or a project
// with a tsconfig.json.
func TestExamplesBuild(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "*", "main.ts"))
	if err != nil {
		t.Fatal(err)
	}
	projects, err := filepath.Glob(filepath.Join("..", "..", "examples", "*", "tsconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 2 || len(projects) < 1 {
		t.Fatalf("found %d file examples and %d project examples; the globs are broken, or examples went missing", len(files), len(projects))
	}
	for _, path := range append(files, projects...) {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			t.Parallel()
			out := filepath.Join(t.TempDir(), "prog")
			opts := buildOptions{files: []string{path}, output: out}
			if filepath.Base(path) == "tsconfig.json" {
				opts = buildOptions{project: path, output: out}
			}
			var stderr bytes.Buffer
			if code := runBuild(opts, &stderr); code != exitOK {
				t.Fatalf("build failed (%d):\n%s", code, stderr.String())
			}
			if _, err := os.Stat(out); err != nil {
				t.Errorf("no binary at %s: %v", out, err)
			}
		})
	}
}
