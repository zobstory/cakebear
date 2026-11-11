package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// JSON.parse, res.json() and the reads of what they make (S1 plan D3,
// D4 (b)). TypeScript types JSON.parse's result `any`, which cakebear
// doesn't lower, so a result is lowered where it fills an unknown's slot:
// an unknown is what a program narrows to read it.

// intoUnknown reports whether n, whose own type is any, fills a slot of
// type unknown: an annotated binding, an argument, a return. That slot is
// what the result is, since nothing else can be done with an unknown
// without the narrowing that checks it.
func (l *lowerer) intoUnknown(n *ast.Node) bool {
	for p := n; ; p = p.Parent {
		ct := l.contextualType(p)
		if ct != nil {
			return ct.Flags()&checker.TypeFlagsUnknown != 0
		}
		// An await's operand takes its slot from the await.
		if p.Parent == nil || p.Parent.Kind != ast.KindAwaitExpression && p.Parent.Kind != ast.KindParenthesizedExpression {
			return false
		}
	}
}

// jsonParse lowers `JSON.parse(text)`.
func (l *lowerer) jsonParse(n *ast.Node, c *ast.CallExpression) ir.Expr {
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}
	switch {
	case len(args) == 2:
		l.failf(args[1], "JSON.parse with a reviver is not supported yet: a reviver can make holes, which arrays don't hold")
		return nil
	case len(args) != 1:
		l.invalidf(n, "JSON.parse takes one or two arguments")
		return nil
	case !l.intoUnknown(n):
		l.failf(n, "JSON.parse returns any, which cakebear doesn't lower: give its result the type unknown, and narrow it to read it")
		return nil
	}
	text := l.exprOf(args[0])
	if text == nil {
		return nil
	}
	if text.ExprType() != ir.String {
		l.failf(args[0], "JSON.parse takes a string here, not a %s", text.ExprType())
		return nil
	}
	return &ir.JSONParse{Text: text, Span: l.calleeCaret(l.span(n), n)}
}

// unknownRead reports whether n reads an unknown that its narrowing has
// made a type the IR doesn't hold, an object or an array: a binding stored
// as an unknown, or a property or an element of such a read. It lowers to
// a value of type unknown.
func (l *lowerer) unknownRead(n *ast.Node) bool {
	switch n.Kind {
	case ast.KindParenthesizedExpression:
		return l.unknownRead(n.AsParenthesizedExpression().Expression)
	case ast.KindIdentifier:
		if _, ok := l.storedUnknown(n); !ok {
			return false
		}
	case ast.KindPropertyAccessExpression:
		if !l.unknownRead(n.AsPropertyAccessExpression().Expression) {
			return false
		}
	case ast.KindElementAccessExpression:
		if !l.unknownRead(n.AsElementAccessExpression().Expression) {
			return false
		}
	default:
		return false
	}
	switch l.typeFromTypeQuiet(l.checker.GetTypeAtLocation(n)) {
	case ir.Invalid, ir.Unknown:
		return true
	}
	return false
}

// unknownProperty lowers `u.k` of an unknown narrowed to an object, and
// unboxes it where the checker narrowed the property too.
func (l *lowerer) unknownProperty(n *ast.Node) (ir.Expr, bool) {
	access := n.AsPropertyAccessExpression()
	if !l.unknownRead(access.Expression) {
		return nil, false
	}
	recv := l.expr(access.Expression)
	if recv == nil {
		return nil, true
	}
	get := &ir.UnknownGet{Value: recv, Key: access.Name().Text(), Span: l.span(n)}
	get.Span.Caret = l.span(access.Name()).Caret
	return narrowed(get, l.typeFromTypeQuiet(l.checker.GetTypeAtLocation(n)), get.Span), true
}

// unknownElement lowers `u[i]` of an unknown narrowed to an array, or
// `u["k"]` of one narrowed to an object.
func (l *lowerer) unknownElement(n *ast.Node) (ir.Expr, bool) {
	access := n.AsElementAccessExpression()
	if !l.unknownRead(access.Expression) {
		return nil, false
	}
	recv := l.expr(access.Expression)
	if recv == nil {
		return nil, true
	}
	use := l.typeFromTypeQuiet(l.checker.GetTypeAtLocation(n))
	if arg := access.ArgumentExpression; arg.Kind == ast.KindStringLiteral {
		get := &ir.UnknownGet{Value: recv, Key: arg.Text(), Span: l.span(n)}
		return narrowed(get, use, get.Span), true
	}
	index := l.exprOf(access.ArgumentExpression)
	if index == nil {
		return nil, true
	}
	if index.ExprType() != ir.Number {
		l.failf(access.ArgumentExpression, "an unknown's element takes a number here, not a %s", index.ExprType())
		return nil, true
	}
	get := &ir.UnknownIndex{Value: recv, Index: index, Span: l.span(n)}
	return narrowed(get, use, get.Span), true
}

// unknownIn lowers `key in u` of an unknown.
func (l *lowerer) unknownIn(n *ast.Node) (ir.Expr, bool) {
	if n.Kind != ast.KindBinaryExpression {
		return nil, false
	}
	b := n.AsBinaryExpression()
	if b.OperatorToken.Kind != ast.KindInKeyword || !l.unknownRead(b.Right) {
		return nil, false
	}
	key := l.exprOf(b.Left)
	value := l.expr(b.Right)
	if key == nil || value == nil {
		return nil, true
	}
	if key.ExprType() != ir.String {
		l.failf(b.Left, "in takes a string key here, not a %s", key.ExprType())
		return nil, true
	}
	span := l.span(n)
	span.Caret = l.span(b.OperatorToken).Caret
	return &ir.UnknownIn{Key: key, Value: value, Span: span}, true
}

// arrayIsArray lowers `Array.isArray(u)` of an unknown, TypeScript's own
// Array, recognised by its libs' declaration.
func (l *lowerer) arrayIsArray(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	access := c.Expression.AsPropertyAccessExpression()
	if access.Expression.Kind != ast.KindIdentifier || access.Expression.Text() != "Array" || access.Name().Text() != "isArray" ||
		!onlyInBundledLib(l.checker.GetSymbolAtLocation(access.Name()), "") {
		return nil, false
	}
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}
	if len(args) != 1 {
		l.invalidf(n, "Array.isArray takes one argument")
		return nil, true
	}
	if _, ok := l.storedUnknown(args[0]); !ok && !l.unknownRead(args[0]) {
		l.failf(n, "Array.isArray of a value whose type is known is not supported yet; it tests an unknown")
		return nil, true
	}
	v := l.expr(args[0])
	if v == nil {
		return nil, true
	}
	if v.ExprType() != ir.Unknown {
		l.failf(args[0], "Array.isArray tests an unknown here, not a %s", v.ExprType())
		return nil, true
	}
	return &ir.UnknownIsArray{Value: v, Span: l.span(n)}, true
}

// jsonResult is init's JSON.parse or res.json() call, through an await or
// parentheses, when it is one: a binding typed by its any is better refused
// in its words than as any (explainInitializer).
func (l *lowerer) jsonResult(init *ast.Node) (*ast.Node, bool) {
	for init.Kind == ast.KindAwaitExpression || init.Kind == ast.KindParenthesizedExpression {
		if init.Kind == ast.KindAwaitExpression {
			init = init.AsAwaitExpression().Expression
		} else {
			init = init.AsParenthesizedExpression().Expression
		}
	}
	if init.Kind != ast.KindCallExpression {
		return nil, false
	}
	callee := init.AsCallExpression().Expression
	if callee.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	access := callee.AsPropertyAccessExpression()
	switch access.Name().Text() {
	case "parse":
		return init, access.Expression.Kind == ast.KindIdentifier && access.Expression.Text() == "JSON"
	case "json":
		_, _, _, _, ok := l.fetchMemberOf(callee)
		return init, ok
	}
	return nil, false
}
