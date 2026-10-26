package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// Optional properties (M5 plan D3). A property `p?: T` is a field of a maybe
// that an object may not have: a literal can leave it out, and whether the
// object has it is its layout's business. Reading and writing it lower as
// any field's do (object.go); this file is the two ways a program asks about
// presence itself, `delete o.p` and `"p" in o`.

// presence lowers `"p" in o`, or refuses a delete used as a value, and
// reports whether n was one of them.
func (l *lowerer) presence(n *ast.Node) (ir.Expr, bool) {
	switch n.Kind {
	case ast.KindDeleteExpression:
		l.failf(n, "delete used as a value is not supported yet; write it as a statement of its own")
		return nil, true
	case ast.KindBinaryExpression:
		if b := n.AsBinaryExpression(); b.OperatorToken.Kind == ast.KindInKeyword {
			return l.inExpr(n, b), true
		}
	}
	return nil, false
}

// inExpr lowers `"p" in o`: whether o has p. The key must be written as a
// string, and name one of o's properties: a shape's fields are all the
// properties an object of it can have.
func (l *lowerer) inExpr(n *ast.Node, b *ast.BinaryExpression) ir.Expr {
	key := ast.SkipParentheses(b.Left)
	if key.Kind != ast.KindStringLiteral && key.Kind != ast.KindNoSubstitutionTemplateLiteral {
		l.failf(n, "in with a key that isn't written as a string is not supported yet")
		return nil
	}
	obj := l.expr(b.Right)
	if obj == nil {
		return nil
	}
	t := obj.ExprType()
	if t.Kind != ir.KindObject {
		l.failf(n, "in on a %s is not supported yet; it takes an object", t)
		return nil
	}
	i := t.Shape.Index(key.Text())
	if i < 0 {
		l.failf(n, "in with %q, which isn't a property of %s, is not supported yet", key.Text(), t.Shape)
		return nil
	}
	return &ir.HasField{Object: obj, Field: i, Span: l.caretAfter(l.span(n), b.Left)}
}

// deleteStatement lowers the statement `delete o.p`, p an optional property:
// TypeScript allows deleting no other. It reports whether expr was a delete.
func (l *lowerer) deleteStatement(stmt, expr *ast.Node) ([]ir.Stmt, bool) {
	if expr.Kind != ast.KindDeleteExpression {
		return nil, false
	}
	target := ast.SkipParentheses(expr.AsDeleteExpression().Expression)
	if target.Kind != ast.KindPropertyAccessExpression || target.AsPropertyAccessExpression().QuestionDotToken != nil {
		l.failf(stmt, "delete of anything but an object's property, o.p, is not supported yet")
		return nil, true
	}
	obj, i, ok := l.field(target.AsPropertyAccessExpression())
	if !ok {
		l.failf(stmt, "delete of %s is not supported yet; it takes an object's property", nodeText(target))
		return nil, true
	}
	if obj == nil {
		return nil, true
	}
	if f := obj.ExprType().Shape.Fields[i]; !f.Optional {
		l.invalidf(stmt, "delete of %s, which isn't optional", f.Name)
		return nil, true
	}
	// V8 records the object, or for a name, the delete itself.
	at, object := l.span(stmt), target.AsPropertyAccessExpression().Expression
	if records(object) {
		at = l.recordedCaret(at, object)
	}
	return []ir.Stmt{&ir.FieldDelete{Object: obj, Field: i, Span: at}}, true
}
