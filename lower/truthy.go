package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// Truthiness (M5 plan D7). JavaScript tests any value in a condition, by
// ToBoolean (ir.Truthy), and `||` and `&&` on values that aren't booleans give
// one of their operands rather than a boolean. Truthiness is how real code
// narrows: `if (s)` makes a `string | undefined` a string, as `s !== undefined`
// would, and the checker's narrowing is all lowering needs from it.

// truthy lowers n as a condition: n when it is a boolean, its truthiness
// otherwise. `a && b`, `a || b` and `!a` are tested part by part, as ToBoolean
// distributes over them, so a condition never builds a value of either
// operand: `if (s && n)` is s's truthiness and then n's.
func (l *lowerer) truthy(n *ast.Node) ir.Expr {
	n = ast.SkipParentheses(n)
	switch {
	case n.Kind == ast.KindBinaryExpression:
		b := n.AsBinaryExpression()
		op, ok := map[ast.Kind]ir.BinaryOp{ast.KindAmpersandAmpersandToken: ir.OpAnd, ast.KindBarBarToken: ir.OpOr}[b.OperatorToken.Kind]
		if !ok {
			break
		}
		left, right := l.truthy(b.Left), l.truthy(b.Right)
		if left == nil || right == nil {
			return nil
		}
		return &ir.Binary{Op: op, Left: left, Right: right, Typ: ir.Boolean, Span: l.span(n)}
	case n.Kind == ast.KindPrefixUnaryExpression && n.AsPrefixUnaryExpression().Operator == ast.KindExclamationToken:
		v := l.truthy(n.AsPrefixUnaryExpression().Operand)
		if v == nil {
			return nil
		}
		return &ir.Unary{Op: ir.OpNot, Operand: v, Typ: ir.Boolean, Span: l.span(n)}
	}
	v := l.expr(n)
	if v == nil {
		return nil
	}
	return toBoolean(v)
}

// toBoolean is v tested as a condition: v when it is a boolean, and its
// truthiness otherwise.
func toBoolean(v ir.Expr) ir.Expr {
	if v.ExprType() == ir.Boolean {
		return v
	}
	return &ir.Truthy{Value: v, Span: v.ExprSpan()}
}

// booleanOperands reports whether the checker types both of b's operands as
// booleans, so that `&&` or `||` between them is a boolean of its own.
func (l *lowerer) booleanOperands(b *ast.BinaryExpression) bool {
	return l.typeOf(b.Left) == ir.Boolean && l.typeOf(b.Right) == ir.Boolean
}

// logical lowers `x || y` or `x && y` used as a value where an operand isn't
// a boolean: `x || y` is x when x is truthy and y otherwise, `x && y` is y
// when x is truthy and x otherwise. y is evaluated only when it is the
// answer, as ??'s default is, and x once, held where reading it again could
// run code.
func (l *lowerer) logical(n *ast.Node) ir.Expr {
	b := n.AsBinaryExpression()
	or := b.OperatorToken.Kind == ast.KindBarBarToken
	op := map[bool]string{true: "||", false: "&&"}[or]
	// exprOf, as for ??: the checker gives both operands the whole's slot.
	x := l.exprOf(b.Left)
	y := l.exprOf(b.Right)
	if x == nil || y == nil {
		return nil
	}
	if x.ExprType() == ir.Unknown {
		// TypeScript types `u || d` as `{} | D`: what the truthy unknown
		// holds, which no type of the subset's is.
		l.failf(n, "%s on an unknown as a value is not supported yet: narrow it first, or test it with ?? if null and undefined are what it guards", op)
		return nil
	}
	t := l.typeOf(n)
	if t == ir.Invalid {
		l.failf(n, "this %s has a type the backend cannot represent yet%s", op, l.reason())
		return nil
	}
	if y = into(y, t); !y.ExprType().Equal(t) {
		l.failf(n, "%s of %s and %s is not supported yet", op, aType(x.ExprType()), aType(y.ExprType()))
		return nil
	}
	return l.hold(x, func(held func() ir.Expr) ir.Expr {
		test := toBoolean(held())
		// x is the answer when it is truthy for ||, and falsy for &&.
		kept := keptAs(held, t, or)
		if kept == nil {
			l.failf(n, "%s of a %s and a %s is not supported yet", op, x.ExprType(), y.ExprType())
			return nil
		}
		if or {
			return &ir.Cond{Test: test, Then: kept, Else: y, Typ: t, Span: l.span(n)}
		}
		return &ir.Cond{Test: test, Then: y, Else: kept, Typ: t, Span: l.span(n)}
	})
}

// keptAs is x, read by held, as the result t of a logical operator that gives
// x where x is truthy, or falsy when truthy is false; nil where it can't be
// one. A truthy maybe holds a value, so it is unwrapped into a t that is its
// element. A falsy maybe of a value that is always truthy, an object, say, is
// empty, and stays null or undefined as the result: `user && user.name` is
// user's null or undefined where user is either.
func keptAs(held func() ir.Expr, t ir.Type, truthy bool) ir.Expr {
	x := held()
	xt := x.ExprType()
	switch {
	case xt.Equal(t):
		return x
	case xt.Kind == ir.KindMaybe && truthy && t.Equal(*xt.Elem):
		return &ir.Unwrap{Value: x, Typ: t, Span: x.ExprSpan()}
	case xt.Kind == ir.KindMaybe && !truthy && t.Kind == ir.KindMaybe && alwaysTruthy(*xt.Elem):
		isNull := &ir.IsEmpty{Value: x, Null: true, Span: x.ExprSpan()}
		return &ir.Cond{Test: isNull, Then: &ir.Empty{Null: true, Typ: t, Span: x.ExprSpan()}, Else: &ir.Empty{Typ: t, Span: x.ExprSpan()}, Typ: t, Span: x.ExprSpan()}
	}
	if v := into(x, t); v.ExprType().Equal(t) {
		return v
	}
	return nil
}

// alwaysTruthy reports whether every value of t is truthy: an object, an
// array, a function, a promise or a host value.
func alwaysTruthy(t ir.Type) bool {
	switch t.Kind {
	case ir.KindObject, ir.KindArray, ir.KindFunc, ir.KindPromise, ir.KindHost:
		return true
	}
	return false
}

// booleanCallback is the global Boolean passed as a callback, as in
// `xs.filter(Boolean)` (D7): a function of slot's signature giving its first
// argument's truthiness.
func (l *lowerer) booleanCallback(n *ast.Node, slot ir.Type) ir.Expr {
	sig := slot.Sig
	if sig == nil || len(sig.Params) == 0 || sig.Result != ir.Boolean {
		l.failf(n, "Boolean as a callback is supported where a boolean of a value is wanted, as filter's is")
		return nil
	}
	lit := &ir.FuncLit{Result: ir.Boolean, Typ: slot, Span: l.span(n)}
	value := l.tempName("value")
	for i, pt := range sig.Params {
		name := ""
		if i == 0 {
			name = value
		}
		lit.Params = append(lit.Params, ir.Param{Name: name, Type: pt})
	}
	arg := &ir.Ident{Name: value, Stable: true, Typ: sig.Params[0], Span: l.span(n)}
	lit.Body = []ir.Stmt{&ir.Return{Value: toBoolean(arg), Span: l.span(n)}}
	return lit
}
