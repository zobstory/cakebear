package lower

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// program is what lowering shares across the modules of one program.
type program struct {
	checker *checker.Checker
	entry   *ast.SourceFile
	// modules is the module graph: the entry and every module of the
	// program's own it imports, directly or not.
	modules map[*ast.SourceFile]bool
	// files is the same modules, sorted by file, the order the race rule
	// summarises them in.
	files []*ast.SourceFile
	// race is the handler race rule's summary of the whole program, built the
	// first time a concurrent function is checked (race.go).
	race *raceChecker
	// assigned is every binding some code in the program writes to after
	// its declaration. A read of any other is stable (ir.Ident.Stable).
	assigned map[*ast.Symbol]bool
	// appended is every string binding an append targets (append.go).
	appended map[*ast.Symbol]bool
	// shapes interns the program's object shapes (object.go); objects keeps
	// what each object type was classified as, objectLog the order they were
	// classified in, and pending the types being classified, with the shape
	// reserved for one found to refer to itself.
	shapes    ir.Shapes
	objects   map[*checker.Type]objectResult
	objectLog []*checker.Type
	pending   map[*checker.Type]*ir.Shape
	// frozen keeps, per const declaration, why it isn't frozen, or "" when
	// it is (frozen.go).
	frozen map[*ast.Node]string
	// sameFile maps a module that is one file with another by its real
	// path to that other (boundary.go).
	sameFile map[*ast.SourceFile]*ast.SourceFile
}

// Program lowers the program whose entry module is entry: entry and every
// module it imports from the program's own files, directly or not (M3 plan
// D5, D7). The modules come out sorted by file. All of them go through the one
// checker, because types from different checkers can't be mixed.
//
// It gathers every unsupported construct rather than stopping at the first, so
// a user porting a program learns the whole gap in one run.
func Program(entry *ast.SourceFile, c *checker.Checker) (*ir.Program, []*Error) {
	p := &program{
		checker: c, entry: entry, modules: map[*ast.SourceFile]bool{},
		objects: map[*checker.Type]objectResult{}, pending: map[*checker.Type]*ir.Shape{},
	}
	p.collect(entry)

	files := make([]*ast.SourceFile, 0, len(p.modules))
	for f := range p.modules {
		files = append(files, f)
	}
	slices.SortFunc(files, func(a, b *ast.SourceFile) int { return strings.Compare(string(a.FileName()), string(b.FileName())) })
	p.files = files
	p.findSameFiles()
	p.findAssigned()
	p.findAppended()

	out := &ir.Program{}
	var errs []*Error
	for i, f := range files {
		if f == entry {
			out.Entry = i
		}
		l := &lowerer{p: p, file: f, checker: c, name: moduleName(f)}
		out.Modules = append(out.Modules, l.module())
		errs = append(errs, l.errs...)
	}
	if len(errs) > 0 {
		return nil, errs
	}
	out.Shapes = usedShapes(out, p.shapes.All())
	return out, nil
}

// findAssigned records every binding the program writes to: the target of an
// assignment, a compound assignment, or ++ or --, anywhere, nested functions
// included.
func (p *program) findAssigned() {
	p.assigned = map[*ast.Symbol]bool{}
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n.Kind == ast.KindIdentifier && isWrite(n) {
			if sym := p.resolve(p.checker.GetSymbolAtLocation(n)); sym != nil {
				p.assigned[sym] = true
			}
		}
		n.ForEachChild(visit)
		return false
	}
	for _, f := range p.files {
		f.AsNode().ForEachChild(visit)
	}
}

// stable reports whether the binding n names is never written after it is
// declared (ir.Ident.Stable).
func (l *lowerer) stable(n *ast.Node) bool {
	sym := l.p.resolve(l.checker.GetSymbolAtLocation(n))
	return sym != nil && !l.p.assigned[sym]
}

// collect adds f and, in turn, every module of the program's own it imports
// or re-exports from: both are module requests (M7 R8). A visited module is
// not followed again, which is what lets two modules import each other. An
// erased import or re-export brings in nothing: the program is what runs.
func (p *program) collect(f *ast.SourceFile) {
	if p.modules[f] {
		return
	}
	p.modules[f] = true
	for _, stmt := range f.AsNode().Statements() {
		var target *ast.SourceFile
		switch stmt.Kind {
		case ast.KindImportDeclaration:
			if decl := stmt.AsImportDeclaration(); !p.erased(decl) {
				target = p.ownModule(decl.ModuleSpecifier)
			}
		case ast.KindExportDeclaration:
			target = p.reexported(stmt.AsExportDeclaration())
		}
		if target != nil {
			p.collect(target)
		}
	}
}

// reexported returns the module of the program's own an `export … from`
// evaluates, or nil: for an `export` with no specifier, one erased
// (exportErased), or one from anything else, which exportDecl refuses.
func (p *program) reexported(decl *ast.ExportDeclaration) *ast.SourceFile {
	if decl.ModuleSpecifier == nil || p.exportErased(decl) {
		return nil
	}
	return p.ownModule(decl.ModuleSpecifier)
}

// exportErased reports whether an export evaluates nothing and binds
// nothing at run time (M7 plan D3, R7): `export type { … }`, `export type *
// from`, or a list naming types without saying so. `export { type T } from`
// is a module request to Node, which evaluates the module.
func (p *program) exportErased(decl *ast.ExportDeclaration) bool {
	if decl.IsTypeOnly {
		return true
	}
	return decl.ModuleSpecifier != nil && decl.ExportClause != nil && decl.ExportClause.Kind == ast.KindNamedExports &&
		p.namesTypesOnly(decl.ExportClause.AsNamedExports().Elements.Nodes)
}

// erased reports whether an import evaluates nothing (M7 plan D3, R7): an
// `import type`, or an import naming types without saying so,
// `import { Count } from "./types"`. An import naming a value evaluates its
// module, used or not, and so does one whose every name says `type`,
// `import { type Count } from "./types"`: Node strips it to
// `import {} from "./types"`, which runs the module.
func (p *program) erased(decl *ast.ImportDeclaration) bool {
	clause := decl.ImportClause
	if clause == nil {
		return false // `import "./x"`, for its effects alone
	}
	if clause.IsTypeOnly() {
		return true
	}
	bindings := clause.AsImportClause().NamedBindings
	if clause.Name() != nil || bindings == nil || bindings.Kind != ast.KindNamedImports {
		return false
	}
	return p.namesTypesOnly(bindings.AsNamedImports().Elements.Nodes)
}

// namesTypesOnly reports whether a list of import or export specifiers names
// types without saying so: one at least has no `type`, and every one without
// it names a type. TypeScript's emit drops such a declaration, and Node fails
// at link, since a type has no runtime export to bind: an accepted
// difference (M7 plan D3). A list whose every specifier says `type`, or an
// empty one, is a module request to Node, which evaluates the module.
func (p *program) namesTypesOnly(specifiers []*ast.Node) bool {
	plain := false
	for _, el := range specifiers {
		if el.IsTypeOnly() {
			continue
		}
		plain = true
		// A default's hidden const is a value, though its symbol is the
		// `export default <name>` alias the snapshot stops at.
		if sym := p.resolve(p.checker.GetSymbolAtLocation(el.Name())); sym == nil || sym.Flags()&ast.SymbolFlagsValue != 0 || exportAssignment(sym) != nil {
			return false
		}
	}
	return plain
}

// inProgram reports whether a declaration is in one of the program's
// modules, and not in an ambient context there: `declare global { var x }`
// declares a value the program has no code for, as a library's does.
func (p *program) inProgram(decl *ast.Node) bool {
	return decl != nil && decl.Flags&ast.NodeFlagsAmbient == 0 && p.modules[ast.GetSourceFileOfNode(decl)]
}

// resolve follows an import to what it names: the declaration in the module
// that exports it, or the hidden const of an `export default` the chain
// passes through, when that holds a snapshot (defaults.go).
func (p *program) resolve(sym *ast.Symbol) *ast.Symbol {
	if sym != nil && sym.Flags()&ast.SymbolFlagsAlias != 0 {
		if target := p.checker.GetAliasedSymbol(sym); target != nil {
			if d := p.snapshotDefault(sym, target); d != nil {
				return d
			}
			return target
		}
	}
	return sym
}

// moduleName is a module's source basename, which names the output binary.
func moduleName(f *ast.SourceFile) string {
	name := string(f.FileName())
	return strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
}
