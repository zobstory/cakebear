package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
)

// Module-qualified references (M3 plan D5). A name declared at a module's top
// level is qualified with that module's file, so two modules' top-level
// `helper`s stay apart; a name local to a function is not, since Go's own
// scoping resolves it. An imported name resolves to the module that exports
// it.

// moduleOf returns the file of the module whose top level declares the
// binding n names, or "" when it is local to a function.
func (l *lowerer) moduleOf(n *ast.Node) string {
	sym := l.checker.GetSymbolAtLocation(n)
	if sym == nil {
		return ""
	}
	if sym.Flags()&ast.SymbolFlagsAlias != 0 {
		if target := l.checker.GetAliasedSymbol(sym); target != nil {
			sym = target
		}
	}
	return topLevelModule(sym)
}

// topLevelModule returns the file whose top level declares sym, or "".
func topLevelModule(sym *ast.Symbol) string {
	for _, d := range sym.Declarations() {
		if isTopLevel(d) {
			return string(ast.GetSourceFileOfNode(d).FileName())
		}
	}
	return ""
}

// isTopLevel reports whether decl is a function or variable declared directly
// in its file. One inside a block of top-level code is local to the block, as
// the emitter treats it.
func isTopLevel(decl *ast.Node) bool {
	switch decl.Kind {
	case ast.KindFunctionDeclaration:
		return decl.Parent != nil && decl.Parent.Kind == ast.KindSourceFile
	case ast.KindVariableDeclaration:
		list := decl.Parent
		if list == nil || list.Parent == nil || list.Parent.Kind != ast.KindVariableStatement {
			return false
		}
		statement := list.Parent
		return statement.Parent != nil && statement.Parent.Kind == ast.KindSourceFile
	}
	return false
}
