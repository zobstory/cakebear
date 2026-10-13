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
	"github.com/zobstory/cakebear/internal/vfs"
	"github.com/zobstory/cakebear/internal/vfs/osvfs"
	cakebeartypes "github.com/zobstory/cakebear/types"
)

// runBuild type-checks the program and, if it is clean, builds it.
//
// The pipeline is assembled here rather than borrowed from internal/execute:
// filesystem, host, config, Program, diagnostics. The program is either the
// files named on the command line, under cakebear's own options, or a
// tsconfig.json project (project.go).
func runBuild(opts buildOptions, stderr io.Writer) int {
	osCwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "cakec: %v\n", err)
		return exitBadArgs
	}
	cwd := tspath.NormalizePath(osCwd)

	// bundled.WrapFS overlays the embedded lib.*.d.ts files onto the real
	// filesystem, so a checkout without a node_modules still resolves the
	// standard library.
	// Two overlays: upstream's bundled lib.*.d.ts, and cakebear's own type
	// extensions. Both serve virtual paths, so neither can be shadowed by a
	// real file or left stale by a partial install.
	fs := cakebeartypes.WrapFS(bundled.WrapFS(osvfs.FS()))

	var config *tsoptions.ParsedCommandLine
	if len(opts.files) > 0 {
		var code int
		if config, code = filesConfig(opts, fs, cwd, stderr); config == nil {
			return code
		}
	} else {
		path, err := configFile(opts, osCwd)
		if err != nil {
			fmt.Fprintf(stderr, "cakec: %v\n", err)
			return exitBadArgs
		}
		p, diags, err := loadProject(path, opts, fs, cwd)
		if len(diags) > 0 {
			report(diags, cwd, opts, stderr)
			return exitErrors
		}
		if err != nil {
			fmt.Fprintf(stderr, "cakec: %v\n", err)
			return exitBadArgs
		}
		config = p.config
		opts.files, opts.configFile = []string{p.main}, path
		if opts.output == "" {
			opts.output = relativeTo(cwd, p.output)
		}
	}

	host := compiler.NewCompilerHost(fs, bundled.LibPath(), nil /*extendedConfigCache*/, nil /*trace*/, nil /*contentMapperProject*/)

	program := compiler.NewProgram(compiler.ProgramOptions{
		Config: config,
		Host:   host,
		// SingleThreaded reaches further than --checkers: it also serialises
		// parsing and binding, not just the checker pool.
		SingleThreaded: singleThreadedTristate(opts),
	})

	ctx := context.Background()
	diags := collectDiagnostics(ctx, program, config)

	if code := report(diags, cwd, opts, stderr); code != exitOK {
		return code
	}
	if opts.checkOnly {
		return exitOK
	}
	return compileToBinary(ctx, program, opts, stderr)
}

// filesConfig is the program for input files named on the command line. A nil
// config means it couldn't be made, and code is the exit code.
func filesConfig(opts buildOptions, fs vfs.FS, cwd string, stderr io.Writer) (config *tsoptions.ParsedCommandLine, code int) {
	roots := make([]tspath.RootedFilePath, 0, len(opts.files))
	for _, name := range opts.files {
		abs, err := filepath.Abs(name)
		if err != nil {
			fmt.Fprintf(stderr, "cakec: resolving %s: %v\n", name, err)
			return nil, exitBadArgs
		}
		// Check the file exists before handing it to the compiler. The Program
		// would report a missing input as a diagnostic, but "no such file"
		// deserves a plain error rather than TS6053 formatted like a type error.
		if _, err := os.Stat(abs); err != nil {
			fmt.Fprintf(stderr, "cakec: %v\n", err)
			return nil, exitBadArgs
		}
		roots = append(roots, tspath.RootedFilePathFromAbsolute(abs))
	}

	// cakebear's declarations are root files of every program, so `i32`,
	// `base64` and `node:http` resolve without an import, a reference
	// directive or an install.
	roots = append(roots, cakebeartypes.RootFiles()...)

	return tsoptions.NewParsedCommandLine(
		compilerOptionsFor(opts),
		roots,
		nil, // projectReferences
		tspath.RootedDirectoryPathFromAbsolute(cwd),
		fs.CaseSensitivity(),
	), exitOK
}

// compilerOptionsFor is cakebear's stance when there is no tsconfig.json.
//
// Strict is on because cakebear compiles ahead of time: every escape hatch the
// non-strict defaults allow (implicit any, unchecked null) is a value whose
// representation the backend cannot pin down. Better to refuse it at the type
// level than to discover it during lowering.
//
// NoEmit is on because upstream's emit stage produces JavaScript, which is
// precisely what cakebear does not want. Phase 4 emits Go from the IR instead.
func compilerOptionsFor(opts buildOptions) *core.CompilerOptions {
	options := &core.CompilerOptions{
		Target:       core.ScriptTargetESNext,
		Module:       core.ModuleKindESNext,
		Strict:       core.TSTrue,
		NoEmit:       core.TSTrue,
		SkipLibCheck: core.TSTrue,
	}
	if opts.checkers > 0 {
		options.Checkers = &opts.checkers
	}
	return options
}

// defaultCompilerOptions is the no-flags configuration, kept as a named
// function because the defaults are a deliberate stance worth testing directly.
func defaultCompilerOptions() *core.CompilerOptions {
	return compilerOptionsFor(buildOptions{})
}

func singleThreadedTristate(opts buildOptions) core.Tristate {
	if opts.singleThreaded {
		return core.TSTrue
	}
	return core.TSUnknown
}

// collectDiagnostics runs every check stage and returns one sorted, deduplicated
// list.
//
// The sort at the end is what makes output independent of how many checkers ran:
// the pool hands files to workers by weight, so the order diagnostics are
// produced in varies, but the order they are reported in must not. The
// determinism test in parallel_test.go is the guard on that.
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
