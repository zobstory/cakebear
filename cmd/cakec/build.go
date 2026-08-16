package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/bundled"
	"github.com/zobstory/cakebear/internal/compiler"
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/tsoptions"
	"github.com/zobstory/cakebear/internal/tspath"
	"github.com/zobstory/cakebear/internal/vfs/osvfs"
)

// runBuild type-checks a single file and reports what it finds.
//
// The pipeline is assembled here rather than borrowed from internal/execute:
// filesystem, host, config, Program, diagnostics. Phase 4 lowers the checked
// program to Go and hands it to `go build`; that step slots in after
// collectDiagnostics reports clean.
func runBuild(opts buildOptions, stderr io.Writer) int {
	file, err := filepath.Abs(opts.file)
	if err != nil {
		fmt.Fprintf(stderr, "cakec: resolving %s: %v\n", opts.file, err)
		return exitBadArgs
	}

	// Read the file before handing it to the compiler. The Program would
	// report a missing input as a diagnostic, but "no such file" deserves a
	// plain error rather than TS6053 formatted like a type error.
	if _, err := os.Stat(file); err != nil {
		fmt.Fprintf(stderr, "cakec: %v\n", err)
		return exitBadArgs
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "cakec: %v\n", err)
		return exitBadArgs
	}
	cwd = tspath.NormalizePath(cwd)

	// bundled.WrapFS overlays the embedded lib.*.d.ts files onto the real
	// filesystem, so a checkout without a node_modules still resolves the
	// standard library.
	fs := bundled.WrapFS(osvfs.FS())

	host := compiler.NewCompilerHost(cwd, fs, bundled.LibPath(), nil /*extendedConfigCache*/, nil /*trace*/)

	config := tsoptions.NewParsedCommandLine(
		defaultCompilerOptions(),
		[]string{tspath.NormalizePath(file)},
		tspath.ComparePathsOptions{
			UseCaseSensitiveFileNames: fs.UseCaseSensitiveFileNames(),
			CurrentDirectory:          cwd,
		},
	)

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config: config,
		Host:   host,
	})

	diags := collectDiagnostics(context.Background(), program, config)
	return report(diags, cwd, opts, stderr)
}

// defaultCompilerOptions is cakebear's stance when there is no tsconfig.json.
//
// Strict is on because cakebear compiles ahead of time: every escape hatch the
// non-strict defaults allow (implicit any, unchecked null) is a value whose
// representation the backend cannot pin down. Better to refuse it at the type
// level than to discover it during lowering.
//
// NoEmit is on because upstream's emit stage produces JavaScript, which is
// precisely what cakebear does not want. Phase 4 emits Go from the IR instead.
//
// Phase 3 will let tsconfig.json override these; for now a single file gets a
// single sensible configuration.
func defaultCompilerOptions() *core.CompilerOptions {
	return &core.CompilerOptions{
		Target:       core.ScriptTargetESNext,
		Module:       core.ModuleKindESNext,
		Strict:       core.TSTrue,
		NoEmit:       core.TSTrue,
		SkipLibCheck: core.TSTrue,
	}
}

// collectDiagnostics runs every check stage and returns one sorted, deduplicated
// list.
//
// Order matters for what the user sees first, but not for correctness: the
// results are sorted by file and position at the end regardless. Syntactic
// diagnostics come first because a parse error usually cascades into semantic
// noise that is not worth reading.
func collectDiagnostics(ctx context.Context, program *compiler.Program, config *tsoptions.ParsedCommandLine) []*ast.Diagnostic {
	var diags []*ast.Diagnostic

	diags = append(diags, config.GetConfigFileParsingDiagnostics()...)
	diags = append(diags, program.GetProgramDiagnostics()...)

	// nil means "every file in the program".
	syntactic := program.GetSyntacticDiagnostics(ctx, nil)
	diags = append(diags, syntactic...)

	// A file that failed to parse produces semantic diagnostics that are
	// artefacts of the broken parse tree, not real type errors. tsc suppresses
	// them for the same reason.
	if len(syntactic) == 0 {
		diags = append(diags, program.GetBindDiagnostics(ctx, nil)...)
		diags = append(diags, program.GetSemanticDiagnostics(ctx, nil)...)
		diags = append(diags, program.GetGlobalDiagnostics(ctx)...)
	}

	return compiler.SortAndDeduplicateDiagnostics(diags)
}
