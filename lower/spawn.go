package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
	"github.com/zobstory/cakebear/types"
)

// spawn(fn), cakebear's Go-style `go` (M2 plan, D7 and D10): fn runs in a
// domain of its own, in parallel with its caller, and the call is a promise
// of its result. Declared in cakebear.d.ts, so it needs no import; recognised
// by that declaration, so a program's own `spawn` is an ordinary call.

// spawnCall lowers a call to cakebear's spawn. handled is false when the
// callee is anything else.
func (l *lowerer) spawnCall(n *ast.Node, c *ast.CallExpression) (out ir.Expr, handled bool) {
	if c.Expression.Kind != ast.KindIdentifier || c.Expression.Text() != "spawn" || !l.declaredByCakebear(c.Expression) {
		return nil, false
	}
	if c.Arguments == nil || len(c.Arguments.Nodes) != 1 {
		l.failf(n, "spawn takes 1 argument, the function to run")
		return nil, true
	}
	typ := l.typeOf(n)
	if typ.Kind != ir.KindPromise || typ.Elem == nil {
		l.failf(n, "spawn's result is a type the backend cannot represent yet")
		return nil, true
	}
	arg := c.Arguments.Nodes[0]
	fn := l.expr(arg)
	if fn == nil {
		return nil, true
	}
	if !spawnable(fn.ExprType(), *typ.Elem) {
		l.failf(arg, "spawn needs a function that takes no arguments, and this one is %s", fn.ExprType())
		return nil, true
	}
	// fn runs in parallel with its caller: the race rule a request handler
	// is held to applies, so the two share nothing that can change.
	l.checkConcurrent(arg, fn, spawnRuns)
	return &ir.HostCall{Op: ir.HostSpawn, Args: []ir.Expr{fn}, Typ: typ, Span: l.span(n)}, true
}

// spawnable reports whether a function of type t can be spawned for a promise
// of elem: it takes nothing, and returns elem, or a promise of elem.
func spawnable(t, elem ir.Type) bool {
	if t.Kind != ir.KindFunc || t.Sig == nil || len(t.Sig.Params) != 0 {
		return false
	}
	result := t.Sig.Result
	if result.Kind == ir.KindPromise && result.Elem != nil {
		result = *result.Elem
	}
	return result.Equal(elem)
}

// declaredByCakebear reports whether every declaration of the name is in
// cakebear's own declarations file.
func (l *lowerer) declaredByCakebear(name *ast.Node) bool {
	sym := l.checker.GetSymbolAtLocation(name)
	if sym == nil || len(sym.Declarations()) == 0 {
		return false
	}
	for _, d := range sym.Declarations() {
		if f := ast.GetSourceFileOfNode(d); f == nil || string(f.FileName()) != types.DeclarationPath() {
			return false
		}
	}
	return true
}
