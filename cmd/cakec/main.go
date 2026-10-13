// Command cakec is the cakebear compiler: TypeScript in, native binary out.
//
// It is also tsc. `cakec build` is cakebear's command, and anything else goes
// to upstream's execute.CommandLine, which is the whole of tsc, exactly as
// cmd/tsc hands it over: every flag, tsconfig.json, --watch, --build, and the
// --lsp and --api servers. Package main can't be imported, so the tsc_*.go
// files are copies of cmd/tsc's, which TestTscCopies holds to upstream's byte
// for byte.
//
// Native builds don't route through internal/execute. That package is shaped
// around emitting JavaScript, the one stage cakebear replaces, and offers no
// hook to replace it; adding one would mean editing upstream files, which the
// golden rule (see CLAUDE.md) forbids. So build.go drives upstream's frontend
// itself -- scanner, parser, binder and checker -- and lowers the result to Go.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/execute"
	"github.com/zobstory/cakebear/internal/osutil"
)

const usage = `cakec — the cakebear compiler: tsc, plus native builds

USAGE:
    cakec build [flags]                   the project in the nearest tsconfig.json
    cakec build -p <tsconfig.json|dir> [flags]
    cakec build <file.ts>... [flags]      files alone, with no tsconfig.json
    cakec build --files-from <list.txt> [flags]
    cakec build --help
    cakec build --version
    cakec <tsc arguments>

Anything that isn't cakec build is tsc, unchanged: cakec --help prints tsc's
help, cakec -p . compiles a project to JavaScript, and cakec --lsp --stdio is
TypeScript's language server.

A project names the module to build in a "cakebear" block of its
tsconfig.json, which tsc ignores; paths are relative to the config:

    "cakebear": { "main": "src/server.ts", "output": "bin/server" }

FLAGS (cakec build):
    -p, --project PATH  build the project in PATH, a tsconfig.json or its directory
    -o PATH             write the executable to PATH (default: the block's output,
                        or else the source's basename)
    --check             type-check only; do not build
    --emit-go           also write the generated Go beside the output
    --target GOOS/ARCH  cross-compile, e.g. linux/amd64
    --files-from PATH   read input paths from PATH, one per line
    --checkers N        type-check with N parallel checkers (default 4)
    --single-threaded   parse, bind and check on one goroutine
    --no-color          plain diagnostics, no ANSI styling

cakec type-checks the inputs, lowers them to Go, and invokes the Go toolchain
to produce a native binary. The language subset the backend can lower is
narrower than what it type-checks; unsupported constructs are named.

EXIT CODES (cakec build):
    0    no diagnostics
    1    the program has errors
    2    bad usage, or an input could not be read
`

// Version is the cakebear release. Distinct from the upstream TypeScript
// version the frontend reports, which moves on Microsoft's schedule.
const version = "0.0.0-dev"

// Exit codes. Kept as named constants because build.go returns them from
// several places and a bare 2 is easy to misread as an error count.
const (
	exitOK      = 0
	exitErrors  = 1
	exitBadArgs = 2
)

// maxCheckers matches upstream's own clamp in newCheckerPool. Rejecting
// oversized values here means the user gets a diagnostic instead of silently
// getting a different number than they asked for.
const maxCheckers = 256

func main() {
	os.Exit(runMain())
}

// runMain is cmd/tsc's runMain plus one command, build. Only the bare word is
// cakebear's: tsc would read it as a file named "build", and --build and -b
// are still tsc's project-reference builds. The other difference is that a
// project with a "cakebear" block sees cakebear's declarations.
// TestTscMainUnchanged fails when upstream's dispatch changes, so that it gets
// mirrored here.
func runMain() int {
	core.ApplyDebugStackLimit()
	args := osutil.Args()[1:]
	if len(args) > 0 {
		switch args[0] {
		case "build":
			return runBuildCommand(args[1:], os.Stdout, os.Stderr)
		case "--lsp":
			return runLSP(args[1:])
		case "--api":
			return runAPI(args[1:])
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	sys := newSystem()
	if declaresCakebear(args, sys.fs, sys.cwd) {
		sys.fs = withDeclarations(sys.fs)
	}
	result := execute.CommandLine(ctx, sys, args, nil)
	return int(result.Status)
}

// runBuildCommand runs `cakec build`, given the arguments after "build". Its
// help and version live here because `cakec --help` and `cakec --version` are
// tsc's.
func runBuildCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h":
			fmt.Fprint(stdout, usage)
			return exitOK
		case "--version", "-v":
			fmt.Fprintf(stdout, "cakec %s (TypeScript %s)\n", version, core.Version())
			return exitOK
		}
	}

	opts, err := parseBuildArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "cakec: %v\n\nrun `cakec build --help` for usage\n", err)
		return exitBadArgs
	}
	return runBuild(opts, stderr)
}

// buildOptions is what `cakec build` was asked to do, after parsing.
type buildOptions struct {
	files []string
	color bool

	// project is -p's argument: a tsconfig.json, or a directory holding one.
	// With neither files nor -p, the build looks for a tsconfig.json.
	project string
	// configFile is the tsconfig.json a project build loaded, for messages.
	configFile string

	// checkers is 0 when unset, meaning "let upstream pick" (4). Upstream
	// additionally clamps to the file count, so a single-file build always
	// runs one checker no matter what is asked for here.
	checkers       int
	singleThreaded bool

	output    string
	checkOnly bool
	emitGo    bool
	goos      string
	goarch    string

	// race builds with Go's race detector. Tests set it (M1 check 8); there
	// is no flag for it, since it needs cgo and slows the program severalfold.
	race bool
}

func parseBuildArgs(args []string) (buildOptions, error) {
	opts := buildOptions{color: shouldUseColor()}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--no-color":
			opts.color = false

		case arg == "--single-threaded":
			opts.singleThreaded = true

		case arg == "--check":
			opts.checkOnly = true

		case arg == "--emit-go":
			opts.emitGo = true

		case arg == "-o":
			i++
			if i >= len(args) {
				return opts, errors.New("-o needs a path")
			}
			opts.output = args[i]

		case arg == "-p" || arg == "--project":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s needs a tsconfig.json, or a directory holding one", arg)
			}
			opts.project = args[i]

		case arg == "--target":
			i++
			if i >= len(args) {
				return opts, errors.New("--target needs a GOOS/GOARCH pair, e.g. linux/amd64")
			}
			goos, goarch, err := parseTarget(args[i])
			if err != nil {
				return opts, err
			}
			opts.goos, opts.goarch = goos, goarch

		case arg == "--files-from":
			i++
			if i >= len(args) {
				return opts, errors.New("--files-from needs a path")
			}
			listed, err := readFileList(args[i])
			if err != nil {
				return opts, err
			}
			opts.files = append(opts.files, listed...)

		case arg == "--checkers":
			i++
			if i >= len(args) {
				return opts, errors.New("--checkers needs a number")
			}
			n, err := parseCheckers(args[i])
			if err != nil {
				return opts, err
			}
			opts.checkers = n

		case len(arg) > len("--checkers=") && arg[:len("--checkers=")] == "--checkers=":
			n, err := parseCheckers(arg[len("--checkers="):])
			if err != nil {
				return opts, err
			}
			opts.checkers = n

		case len(arg) > 1 && arg[0] == '-':
			return opts, fmt.Errorf("unknown flag %q", arg)

		default:
			opts.files = append(opts.files, arg)
		}
	}

	// tsc's TS5042 says the same: a project brings its own files.
	if opts.project != "" && len(opts.files) > 0 {
		return opts, errors.New("-p can't be combined with input files: a project's files come from its tsconfig.json")
	}
	if opts.checkers > 0 && opts.singleThreaded {
		return opts, errors.New("--checkers and --single-threaded contradict each other; pick one")
	}
	return opts, nil
}

// readFileList takes input paths from a file, one per line. A real project has
// more files than a shell command line can hold -- ARG_MAX is about 1MB, which
// a few thousand paths exhaust -- and splitting the run with xargs would
// compile several unrelated programs instead of one.
//
// Blank lines and # comments are skipped so a list can be kept by hand.
func readFileList(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading --files-from %s: %w", path, err)
	}

	var files []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		files = append(files, line)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("--files-from %s listed no files", path)
	}
	return files, nil
}

// parseTarget splits a GOOS/GOARCH pair. Cross-compilation is a passthrough to
// the Go toolchain, which is most of what choosing a Go-emitting backend bought.
func parseTarget(raw string) (goos, goarch string, err error) {
	goos, goarch, found := strings.Cut(raw, "/")
	if !found || goos == "" || goarch == "" {
		return "", "", fmt.Errorf("--target wants GOOS/GOARCH, e.g. linux/amd64, got %q", raw)
	}
	return goos, goarch, nil
}

func parseCheckers(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("--checkers wants a number, got %q", raw)
	}
	if n < 1 {
		return 0, fmt.Errorf("--checkers must be at least 1, got %d", n)
	}
	if n > maxCheckers {
		return 0, fmt.Errorf("--checkers must be at most %d, got %d", maxCheckers, n)
	}
	return n, nil
}

// shouldUseColor honours NO_COLOR (https://no-color.org) and skips styling
// when stderr is not a terminal, so piping diagnostics into a file or another
// tool doesn't fill it with escape sequences.
func shouldUseColor() bool {
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
