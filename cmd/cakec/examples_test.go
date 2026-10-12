package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Every program in examples/ compiles to a binary, so the examples the docs
// point at cannot quietly stop working.
func TestExamplesBuild(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "*", "main.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 2 {
		t.Fatalf("found %d examples (%v); the glob is broken, or examples went missing", len(paths), paths)
	}
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			t.Parallel()
			out := filepath.Join(t.TempDir(), "prog")
			var stderr bytes.Buffer
			if code := runBuild(buildOptions{files: []string{path}, output: out}, &stderr); code != exitOK {
				t.Fatalf("build failed (%d):\n%s", code, stderr.String())
			}
			if _, err := os.Stat(out); err != nil {
				t.Errorf("no binary at %s: %v", out, err)
			}
		})
	}
}
