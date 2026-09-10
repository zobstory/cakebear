// Command cakec is the cakebear compiler: TypeScript in, native binary out.
//
// Phase 3 gets as far as type checking, in parallel. The frontend is
// upstream's -- scanner, parser, binder and checker all come from the fork --
// but the pipeline that drives them is ours, in build.go, and deliberately does
// not route through internal/execute. That package is shaped around emitting
// JavaScript, which is the one stage cakebear replaces; hooking into it would
// mean editing upstream files, and the golden rule (see CLAUDE.md) exists to
// prevent that.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const usage = `cakec — the cakebear compiler

USAGE:
    cakec build <file.ts>... [flags]
    cakec build --files-from <list.txt> [flags]
    cakec --help
    cakec --version

FLAGS:
    -o PATH             write the executable to PATH (default: source basename)
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

EXIT CODES:
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
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return exitOK
	}

	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage)
		return exitOK

	case "--version", "-v":
		fmt.Fprintf(stdout, "cakec %s\n", version)
		return exitOK

	case "build":
		opts, err := parseBuildArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "cakec: %v\n\nrun `cakec --help` for usage\n", err)
			return exitBadArgs
		}
		return runBuild(opts, stderr)

	default:
		fmt.Fprintf(stderr, "cakec: unknown subcommand %q\n\n%s", args[0], usage)
		return exitBadArgs
	}
}

// buildOptions is what `cakec build` was asked to do, after parsing.
type buildOptions struct {
	files []string
	color bool

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
				return opts, fmt.Errorf("-o needs a path")
			}
			opts.output = args[i]

		case arg == "--target":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("--target needs a GOOS/GOARCH pair, e.g. linux/amd64")
			}
			goos, goarch, err := parseTarget(args[i])
			if err != nil {
				return opts, err
			}
			opts.goos, opts.goarch = goos, goarch

		case arg == "--files-from":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("--files-from needs a path")
			}
			listed, err := readFileList(args[i])
			if err != nil {
				return opts, err
			}
			opts.files = append(opts.files, listed...)

		case arg == "--checkers":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("--checkers needs a number")
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

	if len(opts.files) == 0 {
		return opts, fmt.Errorf("missing input file")
	}
	if opts.checkers > 0 && opts.singleThreaded {
		return opts, fmt.Errorf("--checkers and --single-threaded contradict each other; pick one")
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
