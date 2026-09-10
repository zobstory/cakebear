package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// corpus writes a multi-file TypeScript program into a temp dir and returns the
// file paths.
//
// The files import each other so the checker has real cross-file work to do,
// and every fourth one carries a deliberate type error so there are enough
// diagnostics for ordering to be observable. A corpus where all the errors sit
// in one file would pass a determinism test that a real project would fail.
func corpus(t *testing.T, n int) []string {
	t.Helper()

	dir := t.TempDir()
	paths := make([]string, 0, n)

	for i := range n {
		var src bytes.Buffer
		if i > 0 {
			fmt.Fprintf(&src, "import { value%d } from \"./mod%d\";\n", i-1, i-1)
		}
		fmt.Fprintf(&src, "export const value%d: number = %d;\n", i, i)
		fmt.Fprintf(&src, "export function use%d(x: number): number {\n  return x + value%d;\n}\n", i, i)

		if i%4 == 0 {
			// TS2322: the diagnostic this file is here to produce.
			fmt.Fprintf(&src, "export const wrong%d: string = %d;\n", i, i)
		}

		path := filepath.Join(dir, fmt.Sprintf("mod%d.ts", i))
		if err := os.WriteFile(path, src.Bytes(), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		paths = append(paths, path)
	}
	return paths
}

// checkWith runs the pipeline at a given parallelism and returns the exact
// bytes a user would see.
func checkWith(t *testing.T, files []string, checkers int, single bool) string {
	t.Helper()
	var stderr bytes.Buffer
	runBuild(buildOptions{
		files:          files,
		color:          false,
		checkers:       checkers,
		singleThreaded: single,
	}, &stderr)
	return stderr.String()
}

// TestDiagnosticsAreIdenticalAcrossCheckerCounts is the guard on the one real
// hazard in running checkers in parallel: the pool hands files to workers by
// weight, so diagnostics are *produced* in a nondeterministic order. If the
// reporting order followed the production order, CI would flake and no one
// would be able to diff two builds. collectDiagnostics sorts at the end; this
// proves it.
func TestDiagnosticsAreIdenticalAcrossCheckerCounts(t *testing.T) {
	t.Parallel()

	files := corpus(t, 40)
	want := checkWith(t, files, 1, false)

	if want == "" {
		t.Fatal("corpus produced no output at all; the test is not exercising anything")
	}

	for _, checkers := range []int{2, 3, 4, 8, 16} {
		got := checkWith(t, files, checkers, false)
		if got != want {
			t.Errorf("--checkers %d produced different output than --checkers 1\n--- 1 ---\n%s\n--- %d ---\n%s",
				checkers, want, checkers, got)
		}
	}
}

// Single-threaded serialises parse and bind too, not just the checker pool, so
// it is a separate path through the program and deserves its own assertion.
func TestSingleThreadedMatchesParallel(t *testing.T) {
	t.Parallel()

	files := corpus(t, 24)

	single := checkWith(t, files, 0, true)
	parallel := checkWith(t, files, 8, false)

	if single != parallel {
		t.Errorf("--single-threaded disagreed with --checkers 8\n--- single ---\n%s\n--- parallel ---\n%s",
			single, parallel)
	}
}

// Repeated runs at the same parallelism must also agree. A sort that is not a
// total order (ties broken by map iteration, say) would pass the cross-count
// test above by luck and fail here.
func TestRepeatedRunsAgree(t *testing.T) {
	t.Parallel()

	files := corpus(t, 24)
	first := checkWith(t, files, 8, false)

	for i := range 4 {
		if got := checkWith(t, files, 8, false); got != first {
			t.Fatalf("run %d differed from run 0\n--- 0 ---\n%s\n--- %d ---\n%s", i+1, first, i+1, got)
		}
	}
}

func TestParseCheckersFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    int
		wantErr string
	}{
		{name: "separate value", args: []string{"--checkers", "8", "a.ts"}, want: 8},
		{name: "equals form", args: []string{"--checkers=8", "a.ts"}, want: 8},
		{name: "unset", args: []string{"a.ts"}, want: 0},
		{name: "at the clamp", args: []string{"--checkers", "256", "a.ts"}, want: 256},
		{name: "missing value", args: []string{"--checkers"}, wantErr: "needs a number"},
		{name: "not a number", args: []string{"--checkers", "lots", "a.ts"}, wantErr: "wants a number"},
		{name: "zero", args: []string{"--checkers", "0", "a.ts"}, wantErr: "at least 1"},
		{name: "negative", args: []string{"--checkers", "-2", "a.ts"}, wantErr: "at least 1"},
		{name: "over the clamp", args: []string{"--checkers", "257", "a.ts"}, wantErr: "at most 256"},
		{
			name:    "contradicts single-threaded",
			args:    []string{"--checkers", "4", "--single-threaded", "a.ts"},
			wantErr: "contradict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseBuildArgs(tt.args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseBuildArgs(%q) succeeded, want error containing %q", tt.args, tt.wantErr)
				}
				if !bytes.Contains([]byte(err.Error()), []byte(tt.wantErr)) {
					t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseBuildArgs(%q) = %v, want success", tt.args, err)
			}
			if opts.checkers != tt.want {
				t.Errorf("checkers = %d, want %d", opts.checkers, tt.want)
			}
		})
	}
}

func TestMultipleInputFiles(t *testing.T) {
	t.Parallel()

	opts, err := parseBuildArgs([]string{"a.ts", "b.ts", "--no-color", "c.ts"})
	if err != nil {
		t.Fatalf("parseBuildArgs: %v", err)
	}

	want := []string{"a.ts", "b.ts", "c.ts"}
	if len(opts.files) != len(want) {
		t.Fatalf("files = %q, want %q", opts.files, want)
	}
	for i := range want {
		if opts.files[i] != want[i] {
			t.Errorf("files[%d] = %q, want %q", i, opts.files[i], want[i])
		}
	}
}

func TestInputSummary(t *testing.T) {
	t.Parallel()

	if got := inputSummary([]string{"main.ts"}); got != "main.ts" {
		t.Errorf("single file summary = %q, want the path itself", got)
	}
	if got := inputSummary([]string{"a.ts", "b.ts"}); got != "2 files" {
		t.Errorf("multi-file summary = %q, want %q", got, "2 files")
	}
}
