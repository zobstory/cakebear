package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zobstory/cakebear/backend"
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/compiler"
	"github.com/zobstory/cakebear/lower"
)

// compileToBinary lowers a checked program to Go and builds it.
//
// This runs only after type checking reported clean, so anything it refuses is
// a limit of the current phase rather than a bug in the user's types. The
// diagnostics say so.
func compileToBinary(ctx context.Context, program *compiler.Program, opts buildOptions, stderr io.Writer) int {
	files := userFiles(program, opts)
	if len(files) == 0 {
		fmt.Fprintln(stderr, "cakec: no input file to compile")
		return exitBadArgs
	}
	if len(files) > 1 {
		// Linking several modules needs an import graph in the IR, which does
		// not exist yet. Saying that plainly beats compiling only the first.
		fmt.Fprintf(stderr, "cakec: building more than one file is not supported yet (got %d); multi-file programs land with imports\n", len(files))
		return exitBadArgs
	}
	file := files[0]

	checker, done := program.GetTypeCheckerForFile(ctx, file)
	module, lowerErrs := lower.File(file, checker)
	done()

	if len(lowerErrs) > 0 {
		var unsupported, invalid int
		for _, e := range lowerErrs {
			switch e.Kind {
			case lower.Invalid:
				invalid++
				fmt.Fprintf(stderr, "%s: error: %s\n", e.Span, e.Msg)
			default:
				unsupported++
				fmt.Fprintf(stderr, "%s: cannot compile yet: %s\n", e.Span, e.Msg)
			}
		}
		// Two counts, because they mean opposite things to whoever is reading:
		// one asks them to change their code, the other to wait for a release.
		if invalid > 0 {
			fmt.Fprintf(stderr, "cakec: %s\n", plural(invalid, "error"))
		}
		if unsupported > 0 {
			fmt.Fprintf(stderr, "cakec: %s the backend does not support yet\n", plural(unsupported, "construct"))
		}
		return exitErrors
	}

	output := opts.output
	if output == "" {
		output = module.Name
	}

	goSource, err := backend.Build(module, backend.Options{
		Output:       output,
		KeepGoSource: opts.emitGo,
		GOOS:         opts.goos,
		GOARCH:       opts.goarch,
	})

	if opts.emitGo {
		path := output + ".go"
		if writeErr := os.WriteFile(path, []byte(goSource), 0o600); writeErr != nil {
			fmt.Fprintf(stderr, "cakec: writing %s: %v\n", path, writeErr)
		} else {
			fmt.Fprintf(stderr, "cakec: wrote generated Go to %s\n", path)
		}
	}

	if err != nil {
		if err == backend.ErrNoToolchain {
			fmt.Fprintln(stderr, "cakec: the Go toolchain is required to build cakebear programs but was not found on PATH")
			fmt.Fprintln(stderr, "cakec: install Go from https://go.dev/dl and make sure `go` is on PATH")
			return exitBadArgs
		}
		fmt.Fprintf(stderr, "cakec: %v\n", err)
		if !opts.emitGo {
			fmt.Fprintln(stderr, "cakec: run again with --emit-go to inspect the generated source")
		}
		return exitErrors
	}

	fmt.Fprintf(stderr, "cakec: built %s\n", output)
	return exitOK
}

// userFiles returns the program's source files that came from the command line,
// filtering out lib.d.ts and anything pulled in by resolution.
func userFiles(program *compiler.Program, opts buildOptions) []*ast.SourceFile {
	// opts.files holds only what the user named; cakebear's injected
	// declarations are deliberately absent, so they are never compiled.
	wanted := make(map[string]bool, len(opts.files))
	for _, f := range opts.files {
		abs, err := filepath.Abs(f)
		if err != nil {
			continue
		}
		wanted[strings.ToLower(filepath.ToSlash(abs))] = true
	}

	var out []*ast.SourceFile
	for _, f := range program.GetSourceFiles() {
		name := strings.ToLower(filepath.ToSlash(f.FileName()))
		if wanted[name] {
			out = append(out, f)
		}
	}
	return out
}
