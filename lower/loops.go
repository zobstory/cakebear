package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// Loops and the statements that update a variable (M4 plan step 1, D13):
// `for (;;)`, unlabelled break and continue, ++ and --, and the compound
// assignments the subset's arithmetic covers. All of them are statements
// here: ++ and += as values are refused, since a statement is what Go has.

// exprStatement lowers expr, the expression of stmt, as a statement.
// Assignment and its relatives are expressions in TypeScript and statements
// in Go, so they are recognised here rather than in expr().
func (l *lowerer) exprStatement(stmt, expr *ast.Node) []ir.Stmt {
	if out, ok := l.update(stmt, expr); ok {
		return out
	}
	if out, ok := l.popStatement(stmt, expr); ok {
		return out
	}
	if expr.Kind == ast.KindBinaryExpression {
		if bin := expr.AsBinaryExpression(); bin.OperatorToken.Kind == ast.KindEqualsToken {
			return l.assignment(stmt, bin)
		}
	}
	lowered := l.expr(expr)
	if lowered == nil {
		return nil
	}
	return []ir.Stmt{&ir.ExprStmt{Expr: lowered, Span: l.span(stmt)}}
}

// updateOf splits an update, `x++`, `x--`, `++x`, `--x` or `x op= v`, into
// its target, its value (nil for ++ and --), the operator it applies and how
// it is written. ok is false for anything else.
func updateOf(expr *ast.Node) (target, value *ast.Node, op ir.BinaryOp, text string, ok bool) {
	step := func(operator ast.Kind) (ir.BinaryOp, string, bool) {
		switch operator {
		case ast.KindPlusPlusToken:
			return ir.OpAdd, "++", true
		case ast.KindMinusMinusToken:
			return ir.OpSub, "--", true
		}
		return 0, "", false
	}
	switch expr.Kind {
	case ast.KindPrefixUnaryExpression:
		u := expr.AsPrefixUnaryExpression()
		op, text, ok = step(u.Operator)
		return u.Operand, nil, op, text, ok
	case ast.KindPostfixUnaryExpression:
		u := expr.AsPostfixUnaryExpression()
		op, text, ok = step(u.Operator)
		return u.Operand, nil, op, text, ok
	case ast.KindBinaryExpression:
		b := expr.AsBinaryExpression()
		switch b.OperatorToken.Kind {
		case ast.KindPlusEqualsToken:
			return b.Left, b.Right, ir.OpAdd, "+=", true
		case ast.KindMinusEqualsToken:
			return b.Left, b.Right, ir.OpSub, "-=", true
		case ast.KindAsteriskEqualsToken:
			return b.Left, b.Right, ir.OpMul, "*=", true
		case ast.KindSlashEqualsToken:
			return b.Left, b.Right, ir.OpDiv, "/=", true
		case ast.KindPercentEqualsToken:
			return b.Left, b.Right, ir.OpMod, "%=", true
		}
	}
	return nil, nil, 0, "", false
}

// update lowers an update as a statement, to the assignment `x = x op v`,
// and reports whether expr was one. x is read once and written once, so the
// two spellings mean the same: the target is a variable, a field of one,
// `name.f`, or an element `name[i]` with a name or a number for i (D13),
// never an expression a second evaluation could change.
func (l *lowerer) update(stmt, expr *ast.Node) ([]ir.Stmt, bool) {
	target, value, op, text, ok := updateOf(expr)
	if !ok {
		return nil, false
	}
	if !updatable(target) {
		l.failf(stmt, "%s on anything but a variable's name, a field of one, or an element indexed by a name or a number, is not supported yet", text)
		return nil, true
	}
	left := l.expr(target)
	if left == nil {
		return nil, true
	}
	var right ir.Expr = &ir.NumberLit{Value: 1, Span: l.span(expr)}
	if value != nil {
		if right = l.expr(value); right == nil {
			return nil, true
		}
	}
	t, rt := left.ExprType(), right.ExprType()
	switch {
	case t.IsExtension():
		// TypeScript types i32 + i32 as number, and the subset makes the
		// narrowing back visible (CLAUDE.md, the type extensions).
		l.failf(stmt, "%s on a %s is not supported, so the conversion stays visible: write %s = %s(%s + …)", text, t, nodeText(target), t, nodeText(target))
		return nil, true
	case t == ir.Number && rt == ir.Number, t == ir.String && op == ir.OpAdd && rt == ir.String:
	default:
		l.failf(stmt, "%s with a %s and a %s is not supported yet", text, t, rt)
		return nil, true
	}
	if id, ok := left.(*ir.Ident); ok && id.Appended && t == ir.String {
		return []ir.Stmt{&ir.Append{Target: id, Parts: ir.StringParts(right), Span: l.span(stmt)}}, true
	}
	var sum ir.Expr = &ir.Binary{Op: op, Left: left, Right: right, Typ: t, Span: l.span(expr)}
	// A maybe the checker has narrowed is read unwrapped and written back
	// wrapped: `n += 1` on a number | undefined known to be a number.
	stored := left
	if u, ok := left.(*ir.Unwrap); ok {
		stored = u.Value
		sum = &ir.Wrap{Value: sum, Typ: stored.ExprType(), Span: l.span(expr)}
	}
	switch get := stored.(type) {
	case *ir.FieldGet:
		return []ir.Stmt{&ir.FieldSet{Object: get.Object, Field: get.Field, Value: sum, Span: l.span(stmt)}}, true
	case *ir.IndexGet:
		return []ir.Stmt{&ir.IndexSet{Array: get.Array, Index: get.Index, Value: sum, Span: l.span(stmt)}}, true
	case *ir.ArrayOp: // xs.length
		op := &ir.ArrayOp{Method: ir.ArraySetLength, Array: get.Array, Args: []ir.Expr{sum}, Typ: ir.Void, Span: l.span(stmt)}
		return []ir.Stmt{&ir.ExprStmt{Expr: op, Span: l.span(stmt)}}, true
	}
	name, module := l.ref(target)
	return []ir.Stmt{&ir.Assign{Name: name, Module: module, Value: sum, Span: l.span(stmt)}}, true
}

// updatable reports whether an update may target n: a name, a field of a
// name, or an element of a name indexed by a name or a number, whose
// receiver and index a second read finds unchanged.
func updatable(n *ast.Node) bool {
	if n.Kind == ast.KindIdentifier {
		return true
	}
	if n.Kind == ast.KindElementAccessExpression {
		access := n.AsElementAccessExpression()
		index := access.ArgumentExpression.Kind
		return access.QuestionDotToken == nil && access.Expression.Kind == ast.KindIdentifier &&
			(index == ast.KindIdentifier || index == ast.KindNumericLiteral)
	}
	if n.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	access := n.AsPropertyAccessExpression()
	return access.QuestionDotToken == nil && access.Expression.Kind == ast.KindIdentifier
}

// nodeText is n as written, for a message.
func nodeText(n *ast.Node) string {
	switch n.Kind {
	case ast.KindPropertyAccessExpression:
		access := n.AsPropertyAccessExpression()
		return nodeText(access.Expression) + "." + access.Name().Text()
	case ast.KindElementAccessExpression:
		access := n.AsElementAccessExpression()
		return nodeText(access.Expression) + "[" + nodeText(access.ArgumentExpression) + "]"
	}
	return n.Text()
}

// failUpdateValue refuses ++ or -- where its value is used, `f(i++)` say.
func (l *lowerer) failUpdateValue(n *ast.Node, text string) {
	l.failf(n, "%s as a value is not supported yet; use it as a statement of its own", text)
}

// condition lowers the condition of an if, a while or a for. It must be a
// boolean: TypeScript also accepts any value and tests its truthiness, which
// the backend does not do yet, and which Go's own if would reject.
func (l *lowerer) condition(n *ast.Node) ir.Expr {
	cond := l.expr(n)
	if cond == nil {
		return nil
	}
	if t := cond.ExprType(); t != ir.Boolean {
		l.failf(n, "a condition must be a boolean; testing a %s for truthiness is not supported yet", t)
		return nil
	}
	return cond
}

// forStmt lowers `for (init; cond; post) body`. A let in the first clause
// is one binding per iteration, in JavaScript and in Go 1.22's loop
// variables alike, so a closure made in the body sees its own iteration's
// value. A clause holding a comma expression is refused.
func (l *lowerer) forStmt(n *ast.Node) []ir.Stmt {
	s := n.AsForStatement()
	out := &ir.For{Span: l.span(n)}
	if init := s.Initializer; init != nil {
		if init.Kind == ast.KindVariableDeclarationList {
			out.Init = l.declarations(init, init)
		} else {
			out.Init = l.clause(init)
		}
	}
	if s.Condition != nil {
		out.Cond = l.condition(s.Condition)
	}
	if s.Incrementor != nil {
		out.Post = l.clause(s.Incrementor)
	}
	out.Body = l.statementBody(s.Statement)
	return []ir.Stmt{out}
}

// clause lowers the expression of a for loop's first or third clause, which
// Go takes as one simple statement.
func (l *lowerer) clause(e *ast.Node) []ir.Stmt {
	if e.Kind == ast.KindBinaryExpression && e.AsBinaryExpression().OperatorToken.Kind == ast.KindCommaToken {
		l.failf(e, "a comma expression in a for loop's clause is not supported yet")
		return nil
	}
	return l.exprStatement(e, e)
}

// jump lowers an unlabelled break or continue. A labelled one, which can
// leave an outer loop, is refused. TypeScript only accepts a label that
// names an enclosing statement, and lowering already refuses a labelled
// statement, so this is the backstop for when it no longer does.
func (l *lowerer) jump(n *ast.Node) []ir.Stmt {
	if n.Kind == ast.KindBreakStatement {
		if n.AsBreakStatement().Label != nil {
			l.failf(n, "a labelled break is not supported yet")
			return nil
		}
		return []ir.Stmt{&ir.Break{Span: l.span(n)}}
	}
	if n.AsContinueStatement().Label != nil {
		l.failf(n, "a labelled continue is not supported yet")
		return nil
	}
	return []ir.Stmt{&ir.Continue{Span: l.span(n)}}
}
