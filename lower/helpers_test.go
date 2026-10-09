package lower

import (
	"os"
	"path/filepath"
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

	cwd := tspath.NormalizePath(dir)
	// Mirrors what cakec does: cakebear's declarations overlaid on the
	// bundled lib files, so i32, base64 and node:http resolve here too.
	fs := types.WrapFS(bundled.WrapFS(osvfs.FS()))
	host := compiler.NewCompilerHost(cwd, fs, bundled.LibPath(), nil, nil)

	config := tsoptions.NewParsedCommandLine(
		&core.CompilerOptions{
			Target: core.ScriptTargetESNext,
			Module: core.ModuleKindESNext,
			Strict: core.TSTrue,
			NoEmit: core.TSTrue,
		},
		append([]string{tspath.NormalizePath(path)}, types.RootFiles()...),
		tspath.ComparePathsOptions{UseCaseSensitiveFileNames: fs.UseCaseSensitiveFileNames(), CurrentDirectory: cwd},
	)

	program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})
	for _, f := range program.GetSourceFiles() {
		if strings.HasSuffix(f.FileName(), "t.ts") {
			return program, f
		}
	}
	t.Fatal("source file not found in program")
	return nil, nil
}

// lowerSource type-checks a snippet and lowers it, the way cakec does.
func lowerSource(t *testing.T, src string) (*ir.Module, []*Error) {
	t.Helper()
	program, file := checkSource(t, src)
	c, done := program.GetTypeCheckerForFile(t.Context(), file)
	defer done()
	return File(file, c)
}
