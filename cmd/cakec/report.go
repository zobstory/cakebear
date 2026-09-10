package main

import (
	"fmt"
	"io"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/diagnostics"
	"github.com/zobstory/cakebear/internal/diagnosticwriter"
	"github.com/zobstory/cakebear/internal/tspath"
)

// report writes diagnostics to stderr and returns the process exit code.
//
// Errors and warnings are counted separately: warnings alone should not fail a
// build, and conflating them is how a warning quietly becomes unfixable.
func report(diags []*ast.Diagnostic, cwd string, opts buildOptions, w io.Writer) int {
	if len(diags) == 0 {
		fmt.Fprintf(w, "cakec: %s type-checks clean\n", inputSummary(opts.files))
		fmt.Fprintln(w, "cakec: lowering to Go lands in Phase 4 — nothing built yet")
		return exitOK
	}

	formatOpts := &diagnosticwriter.FormattingOptions{
		NewLine: "\n",
		ComparePathsOptions: tspath.ComparePathsOptions{
			UseCaseSensitiveFileNames: true,
			CurrentDirectory:          cwd,
		},
	}

	written := diagnosticwriter.FromASTDiagnostics(diags)
	if opts.color {
		diagnosticwriter.FormatDiagnosticsWithColorAndContext(w, written, formatOpts)
	} else {
		diagnosticwriter.WriteFormatDiagnostics(w, written, formatOpts)
	}

	errors, warnings := tally(diags)
	fmt.Fprintln(w, summary(errors, warnings))

	if errors > 0 {
		return exitErrors
	}
	return exitOK
}

func tally(diags []*ast.Diagnostic) (errors, warnings int) {
	for _, d := range diags {
		switch d.Category() {
		case diagnostics.CategoryError:
			errors++
		case diagnostics.CategoryWarning:
			warnings++
		}
	}
	return errors, warnings
}

// summary reads as a sentence rather than a status line, because it is the one
// piece of output a person actually reads after a failed build.
func summary(errors, warnings int) string {
	switch {
	case errors > 0 && warnings > 0:
		return fmt.Sprintf("cakec: %s, %s", plural(errors, "error"), plural(warnings, "warning"))
	case errors > 0:
		return fmt.Sprintf("cakec: %s", plural(errors, "error"))
	case warnings > 0:
		return fmt.Sprintf("cakec: %s, no errors", plural(warnings, "warning"))
	default:
		return "cakec: no errors"
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// inputSummary names the inputs without pasting a thousand paths into the
// success line when someone points cakec at a whole tree.
func inputSummary(files []string) string {
	if len(files) == 1 {
		return files[0]
	}
	return plural(len(files), "file")
}
