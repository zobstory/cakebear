package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBuildArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantFile string
		wantErr  string
	}{
		{name: "bare file", args: []string{"main.ts"}, wantFile: "main.ts"},
		{name: "file then flag", args: []string{"main.ts", "--no-color"}, wantFile: "main.ts"},
		{name: "flag then file", args: []string{"--no-color", "main.ts"}, wantFile: "main.ts"},
		{name: "no file", args: nil, wantErr: "missing input file"},
		{name: "only a flag", args: []string{"--no-color"}, wantErr: "missing input file"},
		{name: "unknown flag", args: []string{"--emit-go", "main.ts"}, wantErr: `unknown flag "--emit-go"`},
		// A lone "-" is a filename, not a flag: the flag branch requires
		// len(arg) > 1 precisely so this stays reachable.
		{name: "lone dash is a file", args: []string{"-"}, wantFile: "-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseBuildArgs(tt.args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseBuildArgs(%q) succeeded, want error containing %q", tt.args, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseBuildArgs(%q) error = %q, want it to contain %q", tt.args, err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseBuildArgs(%q) = %v, want success", tt.args, err)
			}
			if len(opts.files) != 1 || opts.files[0] != tt.wantFile {
				t.Errorf("files = %q, want exactly [%q]", opts.files, tt.wantFile)
			}
		})
	}
}

func TestParseBuildArgsNoColorFlagWins(t *testing.T) {
	t.Parallel()

	opts, err := parseBuildArgs([]string{"--no-color", "main.ts"})
	if err != nil {
		t.Fatalf("parseBuildArgs: %v", err)
	}
	if opts.color {
		t.Error("color = true after --no-color, want false")
	}
}

func TestRunTopLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "no args prints usage", args: nil, wantCode: exitOK, wantStdout: "USAGE:"},
		{name: "--help", args: []string{"--help"}, wantCode: exitOK, wantStdout: "USAGE:"},
		{name: "-h", args: []string{"-h"}, wantCode: exitOK, wantStdout: "USAGE:"},
		{name: "help", args: []string{"help"}, wantCode: exitOK, wantStdout: "USAGE:"},
		{name: "--version", args: []string{"--version"}, wantCode: exitOK, wantStdout: "cakec " + version},
		{name: "-v", args: []string{"-v"}, wantCode: exitOK, wantStdout: "cakec " + version},
		{
			name:       "unknown subcommand",
			args:       []string{"frost"},
			wantCode:   exitBadArgs,
			wantStderr: `unknown subcommand "frost"`,
		},
		{
			name:       "build with no file",
			args:       []string{"build"},
			wantCode:   exitBadArgs,
			wantStderr: "missing input file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		errors, warnings int
		want             string
	}{
		{0, 0, "cakec: no errors"},
		{1, 0, "cakec: 1 error"},
		{2, 0, "cakec: 2 errors"},
		{0, 1, "cakec: 1 warning, no errors"},
		{0, 3, "cakec: 3 warnings, no errors"},
		{1, 1, "cakec: 1 error, 1 warning"},
		{2, 3, "cakec: 2 errors, 3 warnings"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := summary(tt.errors, tt.warnings); got != tt.want {
				t.Errorf("summary(%d, %d) = %q, want %q", tt.errors, tt.warnings, got, tt.want)
			}
		})
	}
}

func TestPlural(t *testing.T) {
	t.Parallel()

	// Singular drops the count's plural suffix; zero does not, because
	// "0 errors" reads correctly and "0 error" does not.
	for _, tt := range []struct {
		n    int
		want string
	}{{0, "0 errors"}, {1, "1 error"}, {2, "2 errors"}} {
		if got := plural(tt.n, "error"); got != tt.want {
			t.Errorf("plural(%d, error) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestReadFileList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	list := filepath.Join(dir, "files.txt")
	body := "# a comment\n\na.ts\n  b.ts  \n\n# another\nc.ts\n"
	if err := os.WriteFile(list, []byte(body), 0o600); err != nil {
		t.Fatalf("writing list: %v", err)
	}

	got, err := readFileList(list)
	if err != nil {
		t.Fatalf("readFileList: %v", err)
	}

	want := []string{"a.ts", "b.ts", "c.ts"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReadFileListErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if _, err := readFileList(filepath.Join(dir, "missing.txt")); err == nil {
		t.Error("readFileList on a missing path succeeded, want an error")
	}

	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte("# only comments\n\n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if _, err := readFileList(empty); err == nil {
		t.Error("readFileList on a list with no files succeeded, want an error")
	}
}

func TestFilesFromFlag(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	list := filepath.Join(dir, "files.txt")
	if err := os.WriteFile(list, []byte("x.ts\ny.ts\n"), 0o600); err != nil {
		t.Fatalf("writing list: %v", err)
	}

	// --files-from composes with paths given directly on the command line.
	opts, err := parseBuildArgs([]string{"z.ts", "--files-from", list})
	if err != nil {
		t.Fatalf("parseBuildArgs: %v", err)
	}
	want := []string{"z.ts", "x.ts", "y.ts"}
	if len(opts.files) != len(want) {
		t.Fatalf("files = %q, want %q", opts.files, want)
	}

	if _, err := parseBuildArgs([]string{"--files-from"}); err == nil {
		t.Error("--files-from with no path succeeded, want an error")
	}
}
