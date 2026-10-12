package printer

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/tsoptions"
	"github.com/zobstory/cakebear/internal/tspath"
)

// NOTE: EmitHost operations must be thread-safe
type EmitHost interface {
	Options() *core.CompilerOptions
	SourceFiles() []*ast.SourceFile
	CaseSensitivity() tspath.CaseSensitivity
	CommonSourceDirectory() tspath.RootedDirectoryPath
	IsEmitBlocked(file tspath.RootedFilePath) bool
	WriteFile(fileName tspath.RootedFilePath, text string) error
	GetEmitModuleFormatOfFile(file ast.HasFileName) core.ModuleKind
	NewEmitResolver(emitContext *EmitContext) EmitResolver
	GetProjectReferenceFromSource(path tspath.PathKey) *tsoptions.SourceOutputAndProjectReference
	IsSourceFileFromExternalLibrary(file *ast.SourceFile) bool
}
