// Command cakec is the cakebear compiler: TypeScript in, native binary out.
//
// Phase 2 gets as far as type checking. The frontend is upstream's -- scanner,
// parser, binder and checker all come from the fork -- but the pipeline that
// drives them is ours, in build.go, and deliberately does not route through
// internal/execute. That package is shaped around emitting JavaScript, which
// is the one stage cakebear replaces; hooking into it would mean editing
// upstream files, and the golden rule (see CLAUDE.md) exists to prevent that.
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `cakec — the cakebear compiler

USAGE:
    cakec build <file.ts> [flags]
    cakec --help
    cakec --version

FLAGS:
    --no-color    plain diagnostics, no ANSI styling

Phase 2 type-checks the input and reports diagnostics. Lowering to Go and
producing a binary lands in Phase 4.

EXIT CODES:
    0    no diagnostics
    1    the program has errors
    2    bad usage, or the input could not be read
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
	file  string
	color bool
}

func parseBuildArgs(args []string) (buildOptions, error) {
	opts := buildOptions{color: shouldUseColor()}

	for _, arg := range args {
		switch {
		case arg == "--no-color":
			opts.color = false
		case len(arg) > 1 && arg[0] == '-':
			return opts, fmt.Errorf("unknown flag %q", arg)
		case opts.file != "":
			return opts, fmt.Errorf("multiple input files given (%q and %q); expected one", opts.file, arg)
		default:
			opts.file = arg
		}
	}

	if opts.file == "" {
		return opts, fmt.Errorf("missing input file")
	}
	return opts, nil
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
