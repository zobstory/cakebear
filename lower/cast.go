package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// Casts (S1 plan D4): `e satisfies T` changes nothing; `e as unknown`
// boxes e; `u as T` of an unknown is the checked cast, a decoder made for
// T (backend/emit_decode.go), which throws a TypeError naming the path where
// u doesn't fit. Node erases every cast and carries on: an accepted
// difference. So does the `any` of JSON.parse or res.json() filling a slot
// of a data type, `const r: T = JSON.parse(s)` (decodeAny).

// cast lowers `e as T`, `<T>e` and `e satisfies T`.
func (l *lowerer) cast(n *ast.Node) ir.Expr {
	inner := n.Expression()
	v := l.exprOf(inner)
	if v == nil {
		return nil
	}
	if n.Kind == ast.KindSatisfiesExpression {
		return v // a check of the checker's, and nothing at run time
	}
	target := l.typeOf(n)
	switch {
	case target == ir.Invalid:
		l.failf(n, "a cast to a type the backend cannot represent yet%s", l.reason())
		return nil
	case target == ir.Unknown:
		return l.box(n, v)
	case v.ExprType() == ir.Unknown:
		if why := ir.Decodable(target); why != "" {
			l.failf(n, "a cast to %s, which holds %s, is not supported: cakebear checks a cast, and can't check that", l.checker.TypeToString(l.checker.GetTypeAtLocation(n)), why)
			return nil
		}
		return &ir.Decode{Value: v, Typ: target, Cast: l.checker.TypeToString(l.checker.GetTypeAtLocation(n)), Span: l.span(n)}
	case v.ExprType().Equal(target):
		return v // `"a" as Mode`: the value is the type already
	}
	l.failf(n, "a cast from %s to %s is not supported yet: cakebear casts only from an unknown, which it checks", aType(v.ExprType()), aType(target))
	return nil
}

// decodeAny is v, the unknown of an expression TypeScript types `any`
// (JSON.parse, res.json(), an element of an unknown narrowed to an array),
// decoded into the data type of the slot n fills, as a cast to it is; v as
// it is for any other value or slot.
func (l *lowerer) decodeAny(n *ast.Node, v ir.Expr) ir.Expr {
	if v.ExprType() != ir.Unknown || l.checker.GetTypeAtLocation(n).Flags()&checker.TypeFlagsAny == 0 {
		return v
	}
	slot := l.slotType(n)
	if slot == ir.Invalid || slot == ir.Unknown || ir.Decodable(slot) != "" {
		return v
	}
	return &ir.Decode{Value: v, Typ: slot, Cast: l.checker.TypeToString(l.contextualType(n)), Span: l.span(n)}
}

// jsonSlot reports whether n, whose own type is any, fills a slot it can
// be lowered into: an unknown's, or a data type's, which a checked cast
// decodes it into; or none, as a statement of its own, whose value is
// dropped. An await's operand takes its slot from the await.
func (l *lowerer) jsonSlot(n *ast.Node) bool {
	for p := n; ; p = p.Parent {
		if p.Parent != nil && p.Parent.Kind == ast.KindExpressionStatement {
			return true // JSON.parse(s) for its throw alone
		}
		if l.allElement(p) {
			return true // decoded as it is bound (promise.go)
		}
		if ct := l.contextualType(p); ct != nil {
			if ct.Flags()&checker.TypeFlagsUnknown != 0 {
				return true
			}
			t := l.slotOf(ct)
			return t != ir.Invalid && ir.Decodable(t) == ""
		}
		if p.Parent == nil || p.Parent.Kind != ast.KindAwaitExpression && p.Parent.Kind != ast.KindParenthesizedExpression {
			return false
		}
	}
}
