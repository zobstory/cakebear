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
}

// Program lowers the program whose entry module is entry: entry and every
// module it imports from the program's own files, directly or not (M3 plan
// D5, D7). The modules come out sorted by file. All of them go through the one
// checker, because types from different checkers can't be mixed.
//
// It gathers every unsupported construct rather than stopping at the first, so
// a user porting a program learns the whole gap in one run.
func Program(entry *ast.SourceFile, c *checker.Checker) (*ir.Program, []*Error) {
	p := &program{checker: c, entry: entry, modules: map[*ast.SourceFile]bool{}}
	p.collect(entry)

	files := make([]*ast.SourceFile, 0, len(p.modules))
	for f := range p.modules {
		files = append(files, f)
	}
	slices.SortFunc(files, func(a, b *ast.SourceFile) int { return strings.Compare(string(a.FileName()), string(b.FileName())) })
	p.files = files

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
	return out, nil
}

// collect adds f and, in turn, every module of the program's own it imports.
// A visited module is not followed again, which is what lets two modules
// import each other. An `import type` is erased, so it brings in nothing: the
// program is what runs.
func (p *program) collect(f *ast.SourceFile) {
	if p.modules[f] {
		return
	}
	p.modules[f] = true
	for _, stmt := range f.AsNode().Statements() {
		if stmt.Kind == ast.KindImportDeclaration {
			decl := stmt.AsImportDeclaration()
			if clause := decl.ImportClause; clause != nil && clause.IsTypeOnly() {
				continue
			}
			if target := p.imported(decl); target != nil {
				p.collect(target)
			}
		}
	}
}

// imported returns the module an import declaration names, when it is one of
// the program's own: a TypeScript module, not a declaration file, named by a
// relative path. Anything else is nil, and importDecl says why.
func (p *program) imported(decl *ast.ImportDeclaration) *ast.SourceFile {
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
