package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// The conditional expressions (M5 plan D5): `c ? a : b`, `x ?? d`, `x!`, the
// optional chain `a?.b`, and the statement `x ??= d`. Each is an ir.Cond, with
// the value it tests held in an ir.Let where reading it twice could run code
// twice.

// condExpr lowers n when it is one of them, or a receiver an optional chain
// holds, and reports whether it was.
func (l *lowerer) condExpr(n *ast.Node) (ir.Expr, bool) {
	if v, ok := l.held[n]; ok {
		return v, true
	}
	if ast.IsOptionalChain(n) && ast.IsOutermostOptionalChain(n) && !l.chaining[n] {
		return l.chain(n), true
	}
	switch n.Kind {
	case ast.KindConditionalExpression:
		return l.conditional(n), true
	case ast.KindNonNullExpression:
		return l.nonNull(n), true
	case ast.KindBinaryExpression:
		switch n.AsBinaryExpression().OperatorToken.Kind {
		case ast.KindQuestionQuestionToken:
			return l.coalesce(n), true
		case ast.KindQuestionQuestionEqualsToken:
			l.failf(n, "??= used as a value is not supported yet; write it as a statement of its own")
			return nil, true
		case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken:
			if !l.booleanOperands(n.AsBinaryExpression()) {
				return l.logical(n), true // truthy.go
			}
		}
	}
	return nil, false
}

// chainLinkType is t, the checker's type for n, as a link of an optional
// chain is lowered. The checker types each link inside a chain, the `a?.b`
// in `a?.b.c`, as one that may be undefined, which is the chain's, not the
// link's: by the time it is read, a has been tested.
func chainLinkType(n *ast.Node, t ir.Type) ir.Type {
	if ast.IsOptionalChain(n) && !ast.IsOutermostOptionalChain(n) {
		return elemOfMaybe(t)
	}
	return t
}

// conditional lowers `c ? a : b`. The condition must be a boolean, as an
// if's must, and the branches are of one type: the slot's, when they fill
// one, or else the type the checker gives the whole.
func (l *lowerer) conditional(n *ast.Node) ir.Expr {
	c := n.AsConditionalExpression()
	test := l.condition(c.Condition)
	then := l.expr(c.WhenTrue)
	els := l.expr(c.WhenFalse)
	if test == nil || then == nil || els == nil {
		return nil
	}
	t := then.ExprType()
	if !els.ExprType().Equal(t) {
		if t = l.typeOf(n); t == ir.Invalid {
			l.failf(n, "this conditional has a type the backend cannot represent yet%s", l.reason())
			return nil
		}
		then, els = into(then, t), into(els, t)
	}
	if !then.ExprType().Equal(t) || !els.ExprType().Equal(t) {
		l.failf(n, "a conditional choosing between a %s and a %s is not supported yet", then.ExprType(), els.ExprType())
		return nil
	}
	return &ir.Cond{Test: test, Then: then, Else: els, Typ: t, Span: l.span(n)}
}

// into is v as a value of t, where the conversion is one a slot makes: a T,
// null or undefined into a maybe, an extension type into a number.
func into(v ir.Expr, t ir.Type) ir.Expr {
	if t == ir.Number && v.ExprType().IsNumeric() && v.ExprType() != ir.Number {
		return asNumber(v)
	}
	return intoMaybe(v, t)
}

// nonNull lowers `x!`, the program's assertion that x is neither null nor
// undefined. It is checked, as a narrowing is (D4): an Unwrap, Asserted. On
// a value that can't be empty it is x.
func (l *lowerer) nonNull(n *ast.Node) ir.Expr {
	// exprOf, not expr: the slot is the asserted value's, not x's.
	x := l.exprOf(n.AsNonNullExpression().Expression)
	if x == nil {
		return nil
	}
	if t := x.ExprType(); t.Kind == ir.KindMaybe {
		return &ir.Unwrap{Value: x, Typ: *t.Elem, Asserted: true, Span: l.span(n)}
	}
	return x
}

// coalesce lowers `x ?? d`: x, or d when x is null or undefined, d evaluated
// only then. On an x that can't be empty it is x, as d is never reached.
func (l *lowerer) coalesce(n *ast.Node) ir.Expr {
	b := n.AsBinaryExpression()
	// exprOf: the checker gives both operands the whole's slot, which x,
	// being a maybe, doesn't fill, and d fills only through the result.
	x := l.exprOf(b.Left)
	d := l.exprOf(b.Right)
	if x == nil || d == nil {
		return nil
	}
	xt := x.ExprType()
	if xt.Kind != ir.KindMaybe {
		return x
	}
	t := l.typeOf(n)
	if t == ir.Invalid {
		l.failf(n, "this ?? has a type the backend cannot represent yet%s", l.reason())
		return nil
	}
	if d = into(d, t); !d.ExprType().Equal(t) || !t.Equal(xt) && !t.Equal(*xt.Elem) {
		l.failf(n, "?? of a %s and a %s is not supported yet", xt, d.ExprType())
		return nil
	}
	return l.hold(x, func(held func() ir.Expr) ir.Expr {
		present := held()
		if !t.Equal(xt) {
			present = &ir.Unwrap{Value: present, Typ: t, Span: present.ExprSpan()}
		}
		test := &ir.IsEmpty{Value: held(), Undefined: true, Null: true, Span: l.span(b.Left)}
		return &ir.Cond{Test: test, Then: d, Else: present, Typ: t, Span: l.span(n)}
	})
}

// chain lowers n, the last link of an optional chain, `a?.b.c`. When a is
// null or undefined the chain is undefined, and nothing right of the `?.` is
// evaluated. Otherwise it is the chain's links read from a's value, which
// lowering them finds held for a: each link lowers as it would after a `.`.
func (l *lowerer) chain(n *ast.Node) ir.Expr {
	root := n
	for !ast.IsOptionalChainRoot(root) {
		root = root.Expression()
	}
	head := root.Expression()
	h := l.expr(head)
	if h == nil {
		return nil
	}
	if l.held == nil {
		l.held, l.chaining = map[*ast.Node]ir.Expr{}, map[*ast.Node]bool{}
	}
	l.chaining[n] = true
	defer delete(l.chaining, n)
	defer delete(l.held, head)
	ht := h.ExprType()
	if ht.Kind != ir.KindMaybe {
		// `?.` on a value that can't be empty is `.`.
		l.held[head] = h
		return l.exprOf(n)
	}
	t := l.typeOf(n)
	if t.Kind != ir.KindMaybe {
		l.failf(n, "this optional chain has a type the backend cannot represent yet%s", l.reason())
		return nil
	}
	return l.hold(h, func(held func() ir.Expr) ir.Expr {
		l.held[head] = &ir.Unwrap{Value: held(), Typ: *ht.Elem, Span: h.ExprSpan()}
		inner := l.exprOf(n)
		if inner == nil {
			return nil
		}
		test := &ir.IsEmpty{Value: held(), Undefined: true, Null: true, Span: l.span(head)}
		return &ir.Cond{Test: test, Then: &ir.Empty{Typ: t, Span: l.span(n)}, Else: intoMaybe(inner, t), Typ: t, Span: l.span(n)}
	})
}

// hold gives body x to read, more than once, each read a node of its own:
// x again when reading it again can't run code or see another value, a
// binding or a field of one, and otherwise a local holding x, in an ir.Let
// around body.
func (l *lowerer) hold(x ir.Expr, body func(held func() ir.Expr) ir.Expr) ir.Expr {
	if again := reread(x); again != nil {
		first := true
		return body(func() ir.Expr {
			if first {
				first = false
				return x
			}
			return reread(x)
		})
	}
	name := l.tempName("held")
	in := body(func() ir.Expr { return &ir.Ident{Name: name, Stable: true, Typ: x.ExprType(), Span: x.ExprSpan()} })
	if in == nil {
		return nil
	}
	return &ir.Let{Name: name, Value: x, Body: in, Span: x.ExprSpan()}
}

// reread is a second read of x, a copy, when reading it again right after
// the first is the same as using the first: a literal, a binding, or a field
// of one, as nothing runs between a Cond's test and the branch it chooses.
// It is nil for anything else.
func reread(x ir.Expr) ir.Expr {
	switch x := x.(type) {
	case *ir.Ident:
		again := *x
		return &again
	case *ir.NumberLit:
		again := *x
		return &again
	case *ir.StringLit:
		again := *x
		return &again
	case *ir.BoolLit:
		again := *x
		return &again
	case *ir.Empty:
		again := *x
		return &again
	case *ir.Wrap:
		if v := reread(x.Value); v != nil {
			again := *x
			again.Value = v
			return &again
		}
	case *ir.FieldGet:
		if obj := reread(x.Object); obj != nil {
			again := *x
			again.Object = obj
			return &again
		}
	case *ir.Unwrap:
		if v := reread(x.Value); v != nil {
			again := *x
			again.Value = v
			return &again
		}
	}
	return nil
}

// writtenAs is the type a write back to what read x stores: x's own, but for
// an element read as one that may be undefined (D9), which is stored as the
// array's element.
func writtenAs(x ir.Expr) ir.Type {
	if get, ok := x.(*ir.IndexGet); ok && get.OrUndefined {
		return *get.Array.ExprType().Elem
	}
	return x.ExprType()
}

// coalesceAssign lowers the statement `x ??= d`: d assigned to x only when x
// is null or undefined, as an if. x is a name, a field of a name or an
// element of one, as for the other compound assignments, so it is read
// twice. It reports whether expr was one.
func (l *lowerer) coalesceAssign(stmt, expr *ast.Node) ([]ir.Stmt, bool) {
	if expr.Kind != ast.KindBinaryExpression || expr.AsBinaryExpression().OperatorToken.Kind != ast.KindQuestionQuestionEqualsToken {
		return nil, false
	}
	b := expr.AsBinaryExpression()
	if !updatable(b.Left) {
		l.failf(stmt, "??= on anything but a variable's name, a field of one, or an element indexed by a name or a number, is not supported yet")
		return nil, true
	}
	cur := l.exprOf(b.Left)
	d := l.exprOf(b.Right)
	if cur == nil || d == nil {
		return nil, true
	}
	if u, ok := cur.(*ir.Unwrap); ok {
		cur = u.Value // narrowed here, but assigned as the maybe it is
	}
	if get, ok := cur.(*ir.IndexGet); ok && !get.OrUndefined {
		// An element is undefined past the end however it is typed, and
		// Node assigns there: `xs[0] ??= v` on an empty xs appends v.
		cur = &ir.IndexGet{Array: get.Array, Index: get.Index, Typ: ir.OrUndefined(get.Typ), OrUndefined: true, Span: get.Span}
	}
	t := cur.ExprType()
	if t.Kind != ir.KindMaybe {
		return nil, true // never null or undefined, so never assigned
	}
	if d = into(d, writtenAs(cur)); !d.ExprType().Equal(writtenAs(cur)) {
		l.failf(stmt, "??= of a %s to a %s is not supported yet", d.ExprType(), t)
		return nil, true
	}
	var set ir.Stmt
	switch get := cur.(type) {
	case *ir.FieldGet:
		set = &ir.FieldSet{Object: get.Object, Field: get.Field, Value: d, Span: l.span(stmt)}
	case *ir.IndexGet:
		set = &ir.IndexSet{Array: get.Array, Index: get.Index, Value: d, Span: l.span(stmt)}
	default:
		name, module := l.ref(b.Left)
		set = &ir.Assign{Name: name, Module: module, Value: d, Span: l.span(stmt)}
	}
	test := &ir.IsEmpty{Value: cur, Undefined: true, Null: true, Span: l.span(b.Left)}
	return []ir.Stmt{&ir.If{Cond: test, Then: []ir.Stmt{set}, Span: l.span(stmt)}}, true
}
