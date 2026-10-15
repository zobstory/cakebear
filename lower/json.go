package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// jsonCall lowers a call of one of JSON's members, recognised as the global
// value TypeScript's libs declare: a program's own JSON is an ordinary
// binding. The global's symbol is the interface JSON as well, which a program
// may augment, so the test is where the value is declared. Only
// JSON.stringify is in the subset (M4 plan D15). JSON.parse returns any, and
// a typed decode needs validation, so it waits.
func (l *lowerer) jsonCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	access := c.Expression.AsPropertyAccessExpression()
	recv := access.Expression
	if recv.Kind != ast.KindIdentifier || recv.Text() != "JSON" {
		return nil, false
	}
	if sym := l.checker.GetSymbolAtLocation(recv); sym == nil || sym.ValueDeclaration() == nil || !inBundledLib(sym.ValueDeclaration()) {
		return nil, false
	}
	name := access.Name().Text()
	switch {
	case !onlyInBundledLib(l.checker.GetSymbolAtLocation(access.Name()), ""):
		l.failf(access.Name(), "JSON.%s is added to JSON outside TypeScript's libs, which is not supported", name)
	case name == "stringify":
		return l.jsonStringify(n, c), true
	case name == "parse":
		l.failf(n, "JSON.parse is not supported yet: it returns any, and decoding into a type needs checks that wait for null and errors (M5, M6)")
	default:
		l.failf(n, "JSON.%s is not supported yet; only JSON.stringify is", name)
	}
	return nil, true
}

// jsonStringify lowers JSON.stringify(value), and (value, null, space).
func (l *lowerer) jsonStringify(n *ast.Node, c *ast.CallExpression) ir.Expr {
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}
	for _, a := range args {
		if a.Kind == ast.KindSpreadElement {
			l.failf(a, "spreading arguments into JSON.stringify is not supported yet")
			return nil
		}
	}
	if len(args) == 0 || len(args) > 3 {
		l.invalidf(n, "JSON.stringify takes one to three arguments")
		return nil
	}
	value := l.expr(args[0])
	if value == nil || !l.serializable(args[0], value.ExprType()) {
		return nil
	}
	out := &ir.JSONStringify{Value: value, Span: l.span(n)}
	if len(args) > 1 && !isNullish(args[1]) {
		l.failf(args[1], "a replacer is not supported yet; pass null to give a space")
		return nil
	}
	if len(args) == 3 {
		// TypeScript takes a string or a number here, and a union-typed space
		// has already been refused, so this is one or the other (Validate
		// holds it to that).
		if out.Space = l.expr(args[2]); out.Space == nil {
			return nil
		}
	}
	return out
}

// serializable refuses, at n, a value JSON.stringify can't serialise yet.
// Node leaves out a function (or gives undefined for one alone, which the
// subset has no value for), serialises a promise as `{}`, and a host value
// as the runtime object behind it.
func (l *lowerer) serializable(n *ast.Node, t ir.Type) bool {
	what := ir.Unserializable(t, nil)
	switch {
	case what == "":
		return true
	case t.Kind == ir.KindFunc:
		l.failf(n, "JSON.stringify of a function is not supported yet: Node gives undefined, which has no value in cakebear yet (M5)")
	case t.Kind == ir.KindInvalid || t.Kind == ir.KindHost || t.Kind == ir.KindPromise:
		l.failf(n, "JSON.stringify cannot serialise %s yet", what)
	default:
		l.failf(n, "JSON.stringify cannot serialise a %s yet: it holds %s, which JSON would leave out or give as {}", t, what)
	}
	return false
}

// isNullish reports whether n is `null` or `undefined` written as such: the
// replacer argument a call passes to reach the space.
func isNullish(n *ast.Node) bool {
	n = ast.SkipParentheses(n)
	return n.Kind == ast.KindNullKeyword || (n.Kind == ast.KindIdentifier && n.Text() == "undefined")
}
