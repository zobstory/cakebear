package lower

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/tspath"
)

// The program's boundary (M7 plan D4–D6): which modules are the program's
// own, whatever specifier reaches them.
//
// The checker resolves a specifier however it is written: a relative path,
// with an extension or without, a tsconfig `paths` alias, a package.json
// `imports` entry, a workspace package linked through node_modules, which
// TypeScript takes at its real path (R22), or a package's own name. A module
// is the program's own when that resolves to a TypeScript source file, not a
// declaration file, whose real path isn't under node_modules. Specifiers only
// TypeScript resolves, such as an extensionless one or an alias, are taken as
// a bundler takes them (D6): Node itself fails to load those, an accepted
// difference.

// moduleKind is what a specifier reaches, as the boundary sees it.
type moduleKind int

const (
	// moduleOwn is a TypeScript source file of the program's own.
	moduleOwn moduleKind = iota
	// modulePackage is a package's declarations, an ambient module, or
	// nothing the checker resolved: packages come with M13.
	modulePackage
	// moduleScript is a JavaScript module.
	moduleScript
	// moduleDeclared is a declaration file outside node_modules: a
	// workspace package typed by its built .d.ts, or a relative .d.ts.
	moduleDeclared
	// moduleInNodeModules is a TypeScript source file whose real path is
	// under node_modules, which Node refuses to strip (R22).
	moduleInNodeModules
	// moduleMissing is a specifier the checker resolved to nothing, which
	// it reports as an error of its own.
	moduleMissing
)

// specifierModule returns the source file a module specifier resolves to,
// and what kind of module it is.
func (p *program) specifierModule(spec *ast.Node) (*ast.SourceFile, moduleKind) {
	if spec == nil || spec.Kind != ast.KindStringLiteral {
		return nil, modulePackage
	}
	sym := p.checker.GetSymbolAtLocation(spec)
	if sym == nil {
		return nil, moduleMissing
	}
	decl := sym.ValueDeclaration()
	if decl == nil || decl.Kind != ast.KindSourceFile {
		return nil, modulePackage // an ambient module, `declare module "x"`
	}
	f := decl.AsSourceFile()
	under := underNodeModules(realPath(f))
	switch {
	case ast.IsInJSFile(f.AsNode()):
		return f, moduleScript
	case f.IsDeclarationFile && under:
		return f, modulePackage
	case f.IsDeclarationFile:
		return f, moduleDeclared
	case under:
		return f, moduleInNodeModules
	}
	return f, moduleOwn
}

// ownModule returns the module of the program's own a specifier names, or
// nil.
func (p *program) ownModule(spec *ast.Node) *ast.SourceFile {
	if f, kind := p.specifierModule(spec); kind == moduleOwn {
		return f
	}
	return nil
}

// outside says why what a specifier names is no module cakebear compiles;
// what is "imports from" or "re-exports from".
func (p *program) outside(what string, spec *ast.Node) string {
	f, kind := p.specifierModule(spec)
	name := spec.Text()
	switch kind {
	case moduleScript:
		if filepath.Ext(string(f.FileName())) == ".json" {
			return filepath.Base(string(f.FileName())) + " is a JSON module, which cakebear doesn't import yet; declare its data as a const in a .ts module"
		}
		return filepath.Base(string(f.FileName())) + " is a JavaScript module; cakebear compiles TypeScript, so give it types or rewrite it as a .ts module"
	case moduleDeclared:
		if tspath.IsExternalModuleNameRelative(name) {
			return what + " " + quote(name) + " are not supported: it is a declaration file, with no code to compile; cakebear compiles the program's own TypeScript source"
		}
		return quote(name) + " resolves to a declaration file, " + filepath.Base(string(f.FileName())) +
			", and cakebear compiles TypeScript source: point the package's `types` at its source, or list a source condition before `types` in its exports (and in `customConditions`)"
	case moduleMissing:
		return what + " " + quote(name) + " are not supported: TypeScript resolves it to no module, which a missing file, a script (a file with no import or export, which `export {}` makes a module) or a package with no types gives"
	case moduleInNodeModules:
		return quote(name) + " is TypeScript source under node_modules, which Node refuses to run (ERR_UNSUPPORTED_NODE_MODULES_TYPE_STRIPPING); link the package from outside node_modules, as a workspace does"
	}
	return what + " " + quote(name) + " are not supported yet: it is a package, and cakebear compiles node:http and the program's own TypeScript source"
}

func quote(s string) string { return `"` + s + `"` }

// realPath is the file's path with every symlink resolved, or its name as
// the checker has it when that fails, as for a virtual file.
func realPath(f *ast.SourceFile) string {
	name := string(f.FileName())
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		return filepath.ToSlash(resolved)
	}
	return name
}

// underNodeModules reports whether a path has a node_modules directory in it.
func underNodeModules(path string) bool {
	return slices.Contains(strings.Split(path, "/"), "node_modules")
}

// findSameFiles maps each module that is one file with another by its real
// path to that other (M7 plan D5, R11). TypeScript makes two source files,
// with symbols of their own, of a file reached through a symlinked directory
// and directly, where Node runs one module. Lowering can't merge them
// soundly, so every import or re-export reaching either is refused.
func (p *program) findSameFiles() {
	p.sameFile = map[*ast.SourceFile]*ast.SourceFile{}
	byReal := map[string][]*ast.SourceFile{}
	for _, f := range p.files {
		path := realPath(f)
		byReal[path] = append(byReal[path], f)
	}
	for _, group := range byReal {
		for i, f := range group {
			if len(group) > 1 {
				p.sameFile[f] = group[(i+1)%len(group)]
			}
		}
	}
}
