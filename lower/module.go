package lower

import (
	"path/filepath"

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
// level, or one it imports or re-exports from another of the program's own
// modules (M7 step 1), and default exports (step 3, defaults.go): either way
// a reference to the name refers into the module declaring it. A let is
// refused: every module importing it would share a binding that can change,
// which the race rule would have to follow from module to module.

// ref returns how the IR names the binding n refers to: its declared name and
// the file of the module whose top level declares it, or n's own text and ""
// for a binding local to a function. So `import { double as twice }` makes
// `twice` a reference to lib.ts's `double`.
func (l *lowerer) ref(n *ast.Node) (name, module string) {
	if sym := l.p.resolve(l.checker.GetSymbolAtLocation(n)); sym != nil {
		if module := topLevelModule(sym); module != "" {
			return declaredName(sym), module
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
	case ast.KindFunctionDeclaration, ast.KindExportAssignment:
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
	// An import of types alone evaluates nothing, so where it is from
	// doesn't matter: `import type { NextRequest } from "next/server"` is
	// fine, though a package is no module cakebear compiles.
	if decl.ImportClause != nil && l.p.erased(decl) {
		return ""
	}
	target := l.p.ownModule(spec)
	if target == nil {
		if saysTypeEverywhere(decl) {
			l.failf(n, "`import { type … }` evaluates %q, as Node runs it, though it names only types, and cakebear compiles only node:http and the program's own TypeScript modules; write `import type { … }`, which evaluates nothing", spec.Text())
			return ""
		}
		l.failf(n, "%s", l.p.outside("imports from", spec))
		return ""
	}
	if l.refusedSameFile(n, spec, target) {
		return ""
	}
	clause := decl.ImportClause
	if clause == nil {
		return string(target.FileName()) // `import "./x"`, for its effects alone
	}
	if b := clause.AsImportClause().NamedBindings; b != nil && b.Kind == ast.KindNamespaceImport {
		l.failf(n, "`import * as` is supported only for node:http; import what %s exports by name", spec.Text())
	}
	return string(target.FileName())
}

// saysTypeEverywhere reports whether every name an import lists says `type`,
// `import { type A, type B } from "…"`: a module request to Node, naming
// nothing it binds.
func saysTypeEverywhere(decl *ast.ImportDeclaration) bool {
	clause := decl.ImportClause
	if clause == nil || clause.Name() != nil {
		return false
	}
	b := clause.AsImportClause().NamedBindings
	if b == nil || b.Kind != ast.KindNamedImports || len(b.AsNamedImports().Elements.Nodes) == 0 {
		return false
	}
	for _, el := range b.AsNamedImports().Elements.Nodes {
		if !el.IsTypeOnly() {
			return false
		}
	}
	return true
}

// exportDecl checks `export { … }` and `export … from`, and returns the file
// of the program's module an `export … from` evaluates, or "". What evaluates
// nothing is erased first, wherever it is from (M7 plan D3, R7). Each other
// name `export { … }` lists must be a function or a const of this module's
// own, or one it imports from another of the program's own modules.
// `export {}` exports nothing and only makes a file an ES module, which
// TypeScript requires for a top-level await.
func (l *lowerer) exportDecl(n *ast.Node) string {
	decl := n.AsExportDeclaration()
	if l.p.exportErased(decl) {
		return ""
	}
	if decl.ModuleSpecifier != nil {
		return l.reexport(n, decl)
	}
	if decl.ExportClause == nil || decl.ExportClause.Kind != ast.KindNamedExports {
		return ""
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
		if sym == nil {
			continue
		}
		switch target := l.p.resolve(sym); {
		case target.Flags()&ast.SymbolFlagsValue == 0:
			// A type, this module's own or imported: erased, with nothing to
			// compile. Node refuses one exported without `type` (D3).
		case sym.Flags()&ast.SymbolFlagsAlias != 0 && !l.p.inProgram(target.ValueDeclaration()):
			l.failf(el, "exporting %s, which this module imports from outside the program, is not supported; import it in the module that uses it", name.Text())
		case isLet(target):
			l.failf(el, "exporting %s, a let, is not supported yet: every module importing it would share a binding that can change; export a function that reads it", name.Text())
		}
	}
	return ""
}

// reexport checks `export … from` and returns the file of the module it
// evaluates (M7 plan D2). It is a module request, evaluated in source order
// with the module's imports (R8), and makes no binding of its own: a
// reference to a name it re-exports refers into the declaring module, through
// the checker's aliases, however long the chain (R1, R9). `export *` is the
// checker's too, so a local export shadowing a star's (R3), and a star never
// forwarding default (R4), are TypeScript's.
func (l *lowerer) reexport(n *ast.Node, decl *ast.ExportDeclaration) string {
	spec := decl.ModuleSpecifier
	target := l.p.reexported(decl)
	if target == nil {
		l.failf(n, "%s", l.p.outside("re-exports from", spec))
		return ""
	}
	if l.refusedSameFile(n, spec, target) {
		return ""
	}
	switch clause := decl.ExportClause; {
	case clause != nil && clause.Kind == ast.KindNamespaceExport:
		l.failf(n, "`export * as %s from` is not supported yet; re-export the names, or import them from %s where they are used", clause.Name().Text(), spec.Text())
	}
	return string(target.FileName())
}

// refusedSameFile refuses an import or re-export reaching a module that is
// one file with another by its real path (M7 plan D5), and reports whether
// it did.
func (l *lowerer) refusedSameFile(n, spec *ast.Node, target *ast.SourceFile) bool {
	other := l.p.sameFile[target]
	if other == nil {
		return false
	}
	l.failf(n, "%q reaches %s, and another import reaches the same file as %s: TypeScript checks one file reached by two paths as two, where Node runs one module; import it by one path", spec.Text(), l.relName(target), l.relName(other))
	return true
}

// relName is a module's file relative to the directory of the one being
// lowered.
func (l *lowerer) relName(f *ast.SourceFile) string {
	name := string(f.FileName())
	if rel, err := filepath.Rel(filepath.Dir(string(l.file.FileName())), name); err == nil {
		return filepath.ToSlash(rel)
	}
	return name
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
