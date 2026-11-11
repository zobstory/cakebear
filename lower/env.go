package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
	"github.com/zobstory/cakebear/types"
)

// process.env, read-only (S1 plan D8, s1-node R44, R45): a variable read by
// name or by a computed key is a string or undefined, and `in` asks whether
// one is set. process is recognised by its declaration, cakebear's
// (types/node-globals.d.ts) or @types/node's, so a program's own is an
// ordinary binding. Writes are refused by name: the environment would be
// state every request shares. With none, the table never changes, so a
// handler or a spawn may read it.

// envFile reports whether decl is one of the declarations of process or
// its env: cakebear's, or @types/node's.
func envFile(decl *ast.Node) bool {
	if f := ast.GetSourceFileOfNode(decl); f != nil && string(f.FileName()) == types.NodeGlobalsPath() {
		return true
	}
	switch typesNodeFile(decl) {
	case "globals.d.ts", "process.d.ts":
		return true
	}
	return false
}

func declaredForEnv(sym *ast.Symbol) bool {
	if sym == nil || len(sym.Declarations()) == 0 {
		return false
	}
	for _, d := range sym.Declarations() {
		if !envFile(d) {
			return false
		}
	}
	return true
}

// isProcess reports whether n is the global process.
func (l *lowerer) isProcess(n *ast.Node) bool {
	n = ast.SkipParentheses(n)
	return n.Kind == ast.KindIdentifier && n.Text() == "process" && declaredForEnv(l.checker.GetSymbolAtLocation(n))
}

// isProcessEnv reports whether n is `process.env`.
func (l *lowerer) isProcessEnv(n *ast.Node) bool {
	n = ast.SkipParentheses(n)
	return n.Kind == ast.KindPropertyAccessExpression && n.Name().Text() == "env" && l.isProcess(n.Expression()) &&
		declaredForEnv(l.checker.GetSymbolAtLocation(n.Name()))
}

// envRead is process.env's variable key, a string, as a read at n: narrowed
// to the string where the checker has narrowed it, which nothing can make
// unsound, since the table never changes.
func (l *lowerer) envRead(n *ast.Node, key ir.Expr) ir.Expr {
	get := &ir.HostCall{Op: ir.HostEnvGet, Args: []ir.Expr{key}, Typ: ir.MaybeOf(ir.String), Span: l.span(n)}
	return narrowed(get, l.typeOf(n), get.Span)
}

// envProperty lowers `process.env.NAME`, and refuses process.env used whole
// and process's other members.
func (l *lowerer) envProperty(n *ast.Node) (ir.Expr, bool) {
	switch {
	case l.isProcessEnv(n.Expression()):
		name := n.Name()
		return l.envRead(n, &ir.StringLit{Value: name.Text(), Span: l.span(name)}), true
	case l.isProcessEnv(n):
		l.failf(n, "process.env as a whole is not supported yet: read a variable by name, as process.env.PORT")
		return nil, true
	case l.isProcess(n.Expression()):
		l.failf(n, "process.%s is not supported yet; only process.env's variables are read", n.Name().Text())
		return nil, true
	}
	return nil, false
}

// envElement lowers `process.env[key]`, key a string.
func (l *lowerer) envElement(n *ast.Node) (ir.Expr, bool) {
	e := n.AsElementAccessExpression()
	if !l.isProcessEnv(e.Expression) {
		return nil, false
	}
	key := l.expr(e.ArgumentExpression)
	if key == nil {
		return nil, true
	}
	if key.ExprType() != ir.String {
		l.failf(e.ArgumentExpression, "process.env is read by a string here, not %s", aType(key.ExprType()))
		return nil, true
	}
	return l.envRead(n, key), true
}

// envIn lowers `key in process.env`.
func (l *lowerer) envIn(n *ast.Node, b *ast.BinaryExpression) (ir.Expr, bool) {
	if !l.isProcessEnv(b.Right) {
		return nil, false
	}
	key := l.expr(b.Left)
	if key == nil {
		return nil, true
	}
	if key.ExprType() != ir.String {
		l.failf(b.Left, "in process.env takes a string here, not %s", aType(key.ExprType()))
		return nil, true
	}
	return &ir.HostCall{Op: ir.HostEnvHas, Args: []ir.Expr{key}, Typ: ir.Boolean, Span: l.span(n)}, true
}

// envWrite refuses, at stmt, a write to process.env or a variable of it:
// an assignment, an update, `??=` or a delete. It reports whether target
// was one.
func (l *lowerer) envWrite(stmt, target *ast.Node) bool {
	target = ast.SkipParentheses(target)
	switch {
	case l.isProcessEnv(target):
	case target.Kind == ast.KindPropertyAccessExpression && l.isProcessEnv(target.Expression()):
	case target.Kind == ast.KindElementAccessExpression && l.isProcessEnv(target.AsElementAccessExpression().Expression):
	default:
		return false
	}
	l.failf(stmt, "writing process.env is not supported: the environment would be state every request shares; keep what changes in a binding of your own")
	return true
}

// processCall refuses a call of one of process's methods, process.exit
// say, reporting whether c was one.
func (l *lowerer) processCall(c *ast.CallExpression) bool {
	if c.Expression.Kind != ast.KindPropertyAccessExpression || !l.isProcess(c.Expression.Expression()) {
		return false
	}
	l.failf(c.Expression, "process.%s is not supported yet; only process.env's variables are read", c.Expression.Name().Text())
	return true
}
