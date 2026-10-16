package lower

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/internal/tspath"
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

// collect adds f and, in turn, every module of the program's own it imports.
// A visited module is not followed again, which is what lets two modules
// import each other. An erased import brings in nothing: the program is what
// runs.
func (p *program) collect(f *ast.SourceFile) {
	if p.modules[f] {
		return
	}
	p.modules[f] = true
	for _, stmt := range f.AsNode().Statements() {
		if stmt.Kind == ast.KindImportDeclaration {
			decl := stmt.AsImportDeclaration()
			if p.erased(decl) {
				continue
			}
			if target := p.imported(decl); target != nil {
				p.collect(target)
			}
		}
	}
}

// erased reports whether an import evaluates nothing: an `import type`, or
// an import naming only types, `import { Count } from "./types"`.
// TypeScript's emit drops both, so the module runs only if a value is
// imported from it somewhere. Node refuses the second outright, since a type
// has no runtime export to bind, which leaves TypeScript's as the behaviour
// to match. An import naming a value evaluates its module, used or not.
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
	elements := bindings.AsNamedImports().Elements.Nodes
	for _, el := range elements {
		if el.IsTypeOnly() {
			continue
		}
		if sym := p.resolve(p.checker.GetSymbolAtLocation(el.Name())); sym == nil || sym.Flags()&ast.SymbolFlagsValue != 0 {
			return false
		}
	}
	return len(elements) > 0
}

// imported returns the module an import declaration names, when it is one of
// the program's own: a TypeScript module, not a declaration file or a
// JavaScript one, named by a relative path. Anything else is nil, and
// importDecl says why.
func (p *program) imported(decl *ast.ImportDeclaration) *ast.SourceFile {
	if f := p.importedFile(decl); f != nil && !ast.IsInJSFile(f.AsNode()) {
		return f
	}
	return nil
}

// importedScript returns the JavaScript module an import declaration names
// by a relative path, which the program can't compile, or nil.
func (p *program) importedScript(decl *ast.ImportDeclaration) *ast.SourceFile {
	if f := p.importedFile(decl); f != nil && ast.IsInJSFile(f.AsNode()) {
		return f
	}
	return nil
}

// importedFile is the source file, TypeScript or JavaScript, that an import
// names by a relative path, or nil.
func (p *program) importedFile(decl *ast.ImportDeclaration) *ast.SourceFile {
	spec := decl.ModuleSpecifier
	if spec == nil || spec.Kind != ast.KindStringLiteral || !tspath.IsExternalModuleNameRelative(spec.Text()) {
		return nil
	}
	sym := p.checker.GetSymbolAtLocation(spec)
	if sym == nil {
		return nil
	}
	decl0 := sym.ValueDeclaration()
	if decl0 == nil || decl0.Kind != ast.KindSourceFile {
		return nil
	}
	if f := decl0.AsSourceFile(); !f.IsDeclarationFile {
		return f
	}
	return nil
}

// inProgram reports whether a declaration is in one of the program's modules.
func (p *program) inProgram(decl *ast.Node) bool {
	return decl != nil && p.modules[ast.GetSourceFileOfNode(decl)]
}

// resolve follows an import to what it names: the declaration in the module
// that exports it.
func (p *program) resolve(sym *ast.Symbol) *ast.Symbol {
	if sym != nil && sym.Flags()&ast.SymbolFlagsAlias != 0 {
		if target := p.checker.GetAliasedSymbol(sym); target != nil {
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
