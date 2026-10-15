package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
)

// Modules and what they share (M3 plan D5, D7).
//
// A name declared at a module's top level is qualified with that module's
// file, so two modules' top-level `helper`s stay apart; a name local to a
// function is not, since Go's own scoping resolves it. An imported name refers
// into the module that exports it, by the name it is declared with there.
//
// What a module may export is a function or a const declared at its top
// level. A let is refused: every module importing it would share a binding
// that can change, which the race rule would have to follow from module to
// module. So are default exports, `export … from` and re-exporting an import.

// ref returns how the IR names the binding n refers to: its declared name and
// the file of the module whose top level declares it, or n's own text and ""
// for a binding local to a function. So `import { double as twice }` makes
// `twice` a reference to lib.ts's `double`.
func (l *lowerer) ref(n *ast.Node) (name, module string) {
	if sym := l.p.resolve(l.checker.GetSymbolAtLocation(n)); sym != nil {
		if module := topLevelModule(sym); module != "" {
			return sym.Name(), module
		}
	}
	return n.Text(), ""
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
	case ast.KindBindingElement:
		return isTopLevel(bindingRoot(decl))
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

// importDecl checks an import, and returns the file of the program's module
// it evaluates, or "". node:http's are host imports (host.go). One of the
// program's own modules emits no code: Program has already brought the module
// in, and each reference to what the import names refers into it. What the
// import does decide is evaluation order (ir.Module.Imports).
func (l *lowerer) importDecl(n *ast.Node) string {
	decl := n.AsImportDeclaration()
	spec := decl.ModuleSpecifier
	if spec.Kind == ast.KindStringLiteral && (spec.Text() == "node:http" || spec.Text() == "http") {
		l.hostImport(n)
		return ""
	}
	target := l.p.imported(decl)
	if target == nil {
		l.failf(n, "imports from %q are not supported; cakebear compiles node:http and the program's own TypeScript modules, imported by a relative path", spec.Text())
		return ""
	}
	clause := decl.ImportClause
	if clause == nil {
		return string(target.FileName()) // `import "./x"`, for its effects alone
	}
	if l.p.erased(decl) {
		return "" // `import type`, or only types: it evaluates nothing
	}
	if clause.Name() != nil {
		l.failf(n, "a default import is not supported yet; import what %s exports by name", spec.Text())
	}
	if b := clause.AsImportClause().NamedBindings; b != nil && b.Kind == ast.KindNamespaceImport {
		l.failf(n, "`import * as` is supported only for node:http; import what %s exports by name", spec.Text())
	}
	return string(target.FileName())
}

// exportDecl checks `export { … }`: each name must be a function or a const
// of this module's own. `export {}` exports nothing and only makes a file an
// ES module, which TypeScript requires for a top-level await.
func (l *lowerer) exportDecl(n *ast.Node) {
	decl := n.AsExportDeclaration()
	if decl.ModuleSpecifier != nil {
		l.failf(n, "re-exporting with `export … from` is not supported yet; import from the module that declares the names")
		return
	}
	if decl.IsTypeOnly || decl.ExportClause == nil || decl.ExportClause.Kind != ast.KindNamedExports {
		return
	}
	for _, el := range decl.ExportClause.AsNamedExports().Elements.Nodes {
		if el.IsTypeOnly() {
			continue
		}
		sym := l.checker.GetExportSpecifierLocalTargetSymbol(el)
		name := el.AsExportSpecifier().PropertyName
		if name == nil {
			name = el.Name()
		}
		switch {
		case sym == nil:
		case sym.Flags()&ast.SymbolFlagsAlias != 0:
			l.failf(el, "exporting %s, which this module imports, is not supported yet; import it from the module that declares it", name.Text())
		case sym.Flags()&ast.SymbolFlagsValue == 0:
			// A type: erased, with nothing to compile.
		case isLet(sym):
			l.failf(el, "exporting %s, a let, is not supported yet: every module importing it would share a binding that can change; export a function that reads it", name.Text())
		}
	}
}

// exportedVariables checks `export const …` and `export let …`, as
// exportDecl does a name.
func (l *lowerer) exportedVariables(n *ast.Node) {
	if list := n.AsVariableStatement().DeclarationList; list != nil && list.Flags&ast.NodeFlagsLet != 0 {
		l.failf(n, "exporting a let is not supported yet: every module importing it would share a binding that can change; export a function that reads it")
	}
}

// isLet reports whether sym is a variable declared with let.
func isLet(sym *ast.Symbol) bool {
	decl := bindingRoot(sym.ValueDeclaration())
	return decl != nil && decl.Kind == ast.KindVariableDeclaration && decl.Parent != nil &&
		decl.Parent.Flags&ast.NodeFlagsLet != 0
}
