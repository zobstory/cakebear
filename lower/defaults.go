package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// Default exports and imports (M7 plan D7, R12–R15).
//
// A default export is a top-level binding of its module, named by what it
// declares:
//   - `export default function f` is f, a function declaration, hoisted and
//     live, as Node has it (R14). An anonymous default function is a function
//     declaration named `default`, a name no program binding can have, which
//     the emitter's mangling makes a Go one.
//   - `export default <name>` of a const or a function is that binding: the
//     same value, since neither changes.
//   - Any other `export default`, of a let's name, a call or any other
//     expression, is a hidden const named `default`, evaluated at its place in
//     the module's body (R13). That is Node's snapshot (R14): after
//     `let v = 1; export default v; v = 2`, an importer reads 1.
//
// `export { x as default }` is x, live, and of a let is refused as exporting
// a let is (module.go). A default class waits for classes (M12).

// exportAssignment returns the `export default <expr>` declaring sym, or nil.
func exportAssignment(sym *ast.Symbol) *ast.Node {
	if sym == nil {
		return nil
	}
	for _, d := range sym.Declarations() {
		if d.Kind == ast.KindExportAssignment && !d.AsExportAssignment().IsExportEquals {
			return d
		}
	}
	return nil
}

// declaredName is the name the IR gives the top-level binding sym declares.
// The checker names a default export's symbol `default`, a named default
// function's too, so that one takes its function's name.
func declaredName(sym *ast.Symbol) string {
	if sym.Name() == ast.InternalSymbolNameDefault {
		for _, d := range sym.Declarations() {
			if d.Kind == ast.KindFunctionDeclaration && d.Name() != nil {
				return d.Name().Text()
			}
		}
	}
	return sym.Name()
}

// snapshotDefault returns the `export default <name>` symbol an alias chain
// from sym passes through, when what the name refers to, target, is no
// top-level const or function: a let or a property, which the hidden const
// holds a snapshot of. Otherwise nil, and the chain ends at target.
func (p *program) snapshotDefault(sym, target *ast.Symbol) *ast.Symbol {
	if sameValueAlways(target) {
		return nil
	}
	for s, steps := sym, 0; s != nil && s.Flags()&ast.SymbolFlagsAlias != 0 && steps < 64; steps++ {
		if exportAssignment(s) != nil {
			return s
		}
		s = p.checker.GetImmediateAliasedSymbol(s)
	}
	return nil
}

// sameValueAlways reports whether reading sym anywhere, at any time after its
// module ran, gives the same value: a top-level function or const, or a type,
// which has no value to read.
func sameValueAlways(sym *ast.Symbol) bool {
	if sym.Flags()&ast.SymbolFlagsValue == 0 {
		return true
	}
	d := sym.ValueDeclaration()
	if d == nil {
		return false
	}
	return (d.Kind == ast.KindFunctionDeclaration || isConstDecl(d)) && isTopLevel(d)
}

// defaultExport lowers `export default <expr>`: nothing for a name that is
// the binding itself, and otherwise the hidden const, at its place.
func (l *lowerer) defaultExport(n *ast.Node) []ir.Stmt {
	a := n.AsExportAssignment()
	if a.IsExportEquals {
		l.failf(n, "`export =` is not supported; it is CommonJS's, so write `export default`")
		return nil
	}
	sym := n.Symbol()
	if sym == nil {
		return nil
	}
	if sym.Flags()&ast.SymbolFlagsAlias != 0 {
		if target := l.checker.GetAliasedSymbol(sym); target != nil && sameValueAlways(target) {
			return nil // `export default f`: importers refer to f itself
		}
	}
	// The export's own type, widened: `export default 5` exports a number.
	t := l.typeFromType(l.checker.GetTypeOfSymbol(sym))
	if t == ir.Invalid {
		l.failf(n, "the default export has a type the backend cannot represent yet%s", l.reason())
		return nil
	}
	init := l.expr(a.Expression)
	if init == nil {
		return nil
	}
	return []ir.Stmt{&ir.VarDecl{Name: ast.InternalSymbolNameDefault, Type: t, Init: init, Span: l.span(n)}}
}

// The race rule for a default export's hidden const (race.go).

// collectDefault makes `export default () => …`, a hidden const holding a
// function literal, a callable, seen into as a const bound to one is.
func (rc *raceChecker) collectDefault(n *ast.Node) {
	e := ast.SkipParentheses(n.Expression())
	if sym := n.Symbol(); sym != nil && (e.Kind == ast.KindArrowFunction || e.Kind == ast.KindFunctionExpression) {
		rc.addSymbol(sym, ast.InternalSymbolNameDefault, e)
	}
}

func (rc *raceChecker) addSymbol(sym *ast.Symbol, name string, fn *ast.Node) {
	c := &callable{name: name, node: fn}
	rc.callables[sym] = c
	rc.order = append(rc.order, c)
}

// sharedDefault is mutableBinding for a hidden const: shared unless what it
// holds can't change, as for any const. One holding a function is checked as
// a callable, or refused as opaque (opaqueDefault).
func (rc *raceChecker) sharedDefault(d *ast.Node) (*ast.Node, bool) {
	if !rc.l.p.inProgram(d) || shareableConstType(rc.l.typeOf(d.Expression())) {
		return nil, false
	}
	return d, true
}

// opaqueDefault is opaqueFunction for a hidden const: one holding a function
// that isn't a literal the rule sees into, such as a call's result.
func (rc *raceChecker) opaqueDefault(sym *ast.Symbol, d *ast.Node) (*ast.Node, bool) {
	if _, known := rc.callables[sym]; known || !rc.l.p.inProgram(d) || rc.l.typeOf(d.Expression()).Kind != ir.KindFunc {
		return nil, false
	}
	return d, true
}
