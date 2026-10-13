package lower

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/bundled"
	"github.com/zobstory/cakebear/internal/compiler"
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/tsoptions"
	"github.com/zobstory/cakebear/internal/tspath"
	"github.com/zobstory/cakebear/internal/vfs/osvfs"
	"github.com/zobstory/cakebear/ir"
	"github.com/zobstory/cakebear/types"
)

// checkSource type-checks a snippet the way cakec does, returning the program
// and the snippet's file.
func checkSource(t *testing.T, src string) (*compiler.Program, *ast.SourceFile) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "t.ts")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	// Mirrors what cakec does: cakebear's declarations overlaid on the
	// bundled lib files, so i32, base64 and node:http resolve here too.
	fs := types.WrapFS(bundled.WrapFS(osvfs.FS()))
	host := compiler.NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil)

	config := tsoptions.NewParsedCommandLine(
		&core.CompilerOptions{
			Target: core.ScriptTargetESNext,
			Module: core.ModuleKindESNext,
			Strict: core.TSTrue,
			NoEmit: core.TSTrue,
		},
		append([]tspath.RootedFilePath{tspath.RootedFilePathFromAbsolute(path)}, types.RootFiles()...),
		nil, // projectReferences
		tspath.RootedDirectoryPathFromAbsolute(dir),
		fs.CaseSensitivity(),
	)

	program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})
	for _, f := range program.GetSourceFiles() {
		if strings.HasSuffix(string(f.FileName()), "t.ts") {
			return program, f
		}
	}
	t.Fatal("source file not found in program")
	return nil, nil
}

// lowerFiles type-checks several files as one program, the way cakec does,
// and lowers the program whose entry module is entry. It fails the test if
// TypeScript itself reports an error, so a program cakebear refuses is still
// one TypeScript accepts.
func lowerFiles(t *testing.T, files map[string]string, entry string) (*ir.Program, []*Error) {
	t.Helper()
	program, parsed := checkFiles(t, files)
	file, ok := parsed[entry]
	if !ok {
		t.Fatalf("no %s in the program", entry)
	}
	c, done := program.GetTypeChecker(t.Context())
	defer done()
	return Program(file, c)
}

// checkFiles type-checks files as one program, failing the test on any error
// TypeScript reports, and returns the program and its files by name.
func checkFiles(t *testing.T, files map[string]string) (*compiler.Program, map[string]*ast.SourceFile) {
	t.Helper()

	dir := t.TempDir()
	var roots []tspath.RootedFilePath
	for _, name := range slices.Sorted(maps.Keys(files)) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(files[name]), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		roots = append(roots, tspath.RootedFilePathFromAbsolute(path))
	}
	fs := types.WrapFS(bundled.WrapFS(osvfs.FS()))
	host := compiler.NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil)
	config := tsoptions.NewParsedCommandLine(
		&core.CompilerOptions{Target: core.ScriptTargetESNext, Module: core.ModuleKindESNext, Strict: core.TSTrue, NoEmit: core.TSTrue},
		append(roots, types.RootFiles()...), nil, tspath.RootedDirectoryPathFromAbsolute(dir), fs.CaseSensitivity(),
	)
	program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})

	diags := append(program.GetSyntacticDiagnostics(t.Context(), nil), program.GetSemanticDiagnostics(t.Context(), nil)...)
	for _, d := range diags {
		where := "the program"
		if f := d.File(); f != nil {
			where = filepath.Base(string(f.FileName()))
		}
		t.Errorf("TypeScript reports, in %s: %s", where, d.String())
	}

	parsed := map[string]*ast.SourceFile{}
	for name := range files {
		if f := program.GetSourceFile(tspath.RootedFilePathFromAbsolute(filepath.Join(dir, name))); f != nil {
			parsed[name] = f
		}
	}
	return program, parsed
}

// lowerSource type-checks a snippet and lowers it, the way cakec does.
func lowerSource(t *testing.T, src string) (*ir.Module, []*Error) {
	t.Helper()
	program, file := checkSource(t, src)
	c, done := program.GetTypeChecker(t.Context())
	defer done()
	p, errs := Program(file, c)
	if p == nil {
		return nil, errs
	}
	return p.EntryModule(), errs
}
