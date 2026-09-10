// Package backend compiles cakebear IR into a native executable by lowering it
// to Go source and invoking the Go toolchain.
//
// It imports nothing from the forked TypeScript compiler -- only ir/ and the
// standard library. That constraint is what lets the whole package be lifted
// into a standalone github.com/zobstory/buildbinary later without untangling
// anything, and what lets the Go emitter be swapped for an LLVM one without the
// frontend noticing. Adding an internal/... import here breaks both.
package backend

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// runtimeSrc carries the cakebear runtime's source into the cakec binary.
//
// Embedding rather than depending means a compiled program needs no network and
// no module download: cakec ships as one file and writes everything the
// generated module needs. That is the project's own promise applied to its
// compiler.
//
//go:embed runtime/*.go
var runtimeSrc embed.FS

// Options controls a build.
type Options struct {
	// Output is the executable path to produce.
	Output string
	// KeepGoSource, when set, writes the generated Go beside the output and
	// leaves the temp module in place.
	KeepGoSource bool
	// GOOS and GOARCH cross-compile when non-empty. Passing these through is
	// most of what "cross-compilation for free" means in practice.
	GOOS   string
	GOARCH string
}

// ErrNoToolchain reports that the Go toolchain could not be found. It is a
// distinct error because it is a setup problem with a specific fix, not a
// compilation failure, and the CLI phrases it differently.
var ErrNoToolchain = errors.New("the Go toolchain is required to build cakebear programs but was not found on PATH")

// Build lowers a module to Go, compiles it, and writes the executable.
//
// It returns the generated Go source alongside any error so callers can show it
// — when the emitter produces something the Go compiler rejects, the source is
// the only useful thing to look at.
func Build(m *ir.Module, opts Options) (goSource string, err error) {
	goSource = Emit(m)

	if _, err := exec.LookPath("go"); err != nil {
		return goSource, ErrNoToolchain
	}

	dir, err := os.MkdirTemp("", "cakebear-build-*")
	if err != nil {
		return goSource, fmt.Errorf("creating build directory: %w", err)
	}
	if opts.KeepGoSource {
		defer func() { fmt.Fprintf(os.Stderr, "cakec: kept generated module in %s\n", dir) }()
	} else {
		defer func() { _ = os.RemoveAll(dir) }()
	}

	if err := writeModule(dir, goSource); err != nil {
		return goSource, err
	}

	out, err := filepath.Abs(opts.Output)
	if err != nil {
		return goSource, fmt.Errorf("resolving output path: %w", err)
	}

	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = dir
	// GOFLAGS from the surrounding environment can break a build that has no
	// network access; -mod=mod on a module with no requirements is safe.
	cmd.Env = append(os.Environ(),
		"GOFLAGS=-mod=mod",
		"GO111MODULE=on",
	)
	if opts.GOOS != "" {
		cmd.Env = append(cmd.Env, "GOOS="+opts.GOOS)
	}
	if opts.GOARCH != "" {
		cmd.Env = append(cmd.Env, "GOARCH="+opts.GOARCH)
	}

	if combined, err := cmd.CombinedOutput(); err != nil {
		return goSource, fmt.Errorf("go build failed: %w\n%s", err, strings.TrimSpace(string(combined)))
	}
	return goSource, nil
}

// writeModule lays out the throwaway module: a go.mod, the generated main, and
// the embedded runtime.
func writeModule(dir, goSource string) error {
	goMod := fmt.Sprintf("module %s\n\ngo %s\n", genModule, toolchainVersion())
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o600); err != nil {
		return fmt.Errorf("writing go.mod: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goSource), 0o600); err != nil {
		return fmt.Errorf("writing main.go: %w", err)
	}

	rtPath := filepath.Join(dir, rtDir)
	if err := os.MkdirAll(rtPath, 0o755); err != nil {
		return fmt.Errorf("creating runtime directory: %w", err)
	}

	entries, err := fs.ReadDir(runtimeSrc, "runtime")
	if err != nil {
		return fmt.Errorf("reading embedded runtime: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := fs.ReadFile(runtimeSrc, "runtime/"+entry.Name())
		if err != nil {
			return fmt.Errorf("reading embedded runtime file %s: %w", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(rtPath, entry.Name()), data, 0o600); err != nil {
			return fmt.Errorf("writing runtime file %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// toolchainVersion reports the language version for the generated go.mod.
//
// It follows the toolchain that is actually running rather than a pinned
// constant, so a user on a newer Go does not get a module that asks for an
// older language version than their tools default to.
func toolchainVersion() string {
	v := strings.TrimPrefix(runtime.Version(), "go")
	// runtime.Version can carry a patch level ("1.26.1") or a devel string;
	// go.mod wants major.minor.
	parts := strings.Split(v, ".")
	if len(parts) >= 2 && isNumeric(parts[0]) && isNumeric(parts[1]) {
		return parts[0] + "." + parts[1]
	}
	return "1.26"
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
