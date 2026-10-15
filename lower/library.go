package lower

import (
	"path/filepath"

	"github.com/zobstory/cakebear/internal/ast"
)

// libraryName reports whether n names something declared only outside the
// program's modules: in TypeScript's libs (parseInt, NaN, setTimeout), one of
// cakebear's bundled files, or a file the entry module doesn't import. Those
// have Go behind them only where lowering maps them explicitly (console.log,
// node:http, the extension conversions, all checked before this), so a plain
// reference would emit an undefined Go name and fail inside go build. An
// import resolves to the module that exports it.
func (l *lowerer) libraryName(n *ast.Node) bool {
	sym := l.p.resolve(l.checker.GetSymbolAtLocation(n))
	if sym == nil || len(sym.Declarations()) == 0 {
		return false
	}
	for _, d := range sym.Declarations() {
		if l.p.inProgram(d) {
			return false // the program's own, such as a function named parseInt
		}
	}
	return true
}

// failOutside refuses at, which uses name, declared outside the program's
// modules: what is a library global or function, unless name is declared in
// a TypeScript file of the user's own that the entry module doesn't import,
// such as a script whose top-level names are global.
func (l *lowerer) failOutside(at, name *ast.Node, what string) {
	if sym := l.p.resolve(l.checker.GetSymbolAtLocation(name)); sym != nil {
		for _, d := range sym.Declarations() {
			f := ast.GetSourceFileOfNode(d)
			switch {
			case f == nil || f.IsDeclarationFile:
				continue
			case ast.IsInJSFile(f.AsNode()):
				l.failf(at, "%s is declared in %s, a JavaScript module, which cakebear doesn't compile", name.Text(), filepath.Base(string(f.FileName())))
			default:
				l.failf(at, "%s is declared in %s, which the entry module doesn't import; cakebear compiles only the modules it imports",
					name.Text(), filepath.Base(string(f.FileName())))
			}
			return
		}
	}
	l.failf(at, "%s is a library %s cakebear does not implement yet", name.Text(), what)
}
