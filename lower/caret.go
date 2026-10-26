package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/scanner"
	"github.com/zobstory/cakebear/ir"
)

// The carets of uncaught errors' reports (M6 plan D6). Node's report quotes
// the line V8 recorded for the operation that threw, with a caret under
// the column, and V8 records an operation where it happens rather than
// where its expression starts. Measured on Node 24.18:
//
//	console.log(b.p!.x)      under x: a read, at the property's name
//	"ab".repeat(-1)          under repeat: a method call, at the name
//	console.log(u![0])       under [: an element's read
//	b.p!.x = 1               under =: a write, at the assignment
//	xs.length -= 5           under -=
//	"x" in b.p!              under in
//	for (const v of b.xs!)   under xs: the iterable, as V8 records it
//	delete b.p!.x            under p: the object, as V8 records it, or
//	delete q!.x              under delete, where the object is a name
//
// So a node that may throw carries, in its span's Caret, where V8 would
// record it: these helpers find that place in the source.

// caretAt is span with its caret at the first token of n.
func (l *lowerer) caretAt(span ir.Span, n *ast.Node) ir.Span {
	return l.caretAtPos(span, scanner.GetTokenPosOfNode(n, l.file, false /*includeJSDoc*/))
}

// caretAfter is span with its caret at the first token after n: the
// operator after an assignment's target or an `in`'s key, or the `[` after
// an element access's array.
func (l *lowerer) caretAfter(span ir.Span, n *ast.Node) ir.Span {
	return l.caretAtPos(span, scanner.SkipTrivia(l.file.Text(), n.End()))
}

func (l *lowerer) caretAtPos(span ir.Span, pos int) ir.Span {
	line, col := lineAndColumn(l.file, pos)
	span.Caret = ir.Pos{Line: line + 1, Col: col + 1}
	return span
}

// recordedCaret is span with its caret where V8 records n's own evaluation:
// a property access at its name, an element access at its `[`, anything
// else at its start. Parentheses, `!` and type assertions emit nothing, so
// V8 sees through them.
func (l *lowerer) recordedCaret(span ir.Span, n *ast.Node) ir.Span {
	n = ast.SkipOuterExpressions(n, ast.OEKAll)
	switch n.Kind {
	case ast.KindPropertyAccessExpression:
		return l.caretAt(span, n.AsPropertyAccessExpression().Name())
	case ast.KindElementAccessExpression:
		return l.elementCaret(span, n)
	}
	return l.caretAt(span, n)
}

// records reports whether V8 records n's evaluation where recordedCaret
// says, rather than leaving the operation around it at its own place: a
// property or element access, seen through what emits nothing.
func records(n *ast.Node) bool {
	n = ast.SkipOuterExpressions(n, ast.OEKAll)
	return n.Kind == ast.KindPropertyAccessExpression || n.Kind == ast.KindElementAccessExpression
}

// elementCaret is span with its caret at the `[` of n, an element access.
// One in an optional chain, `xs?.[i]`, reads only a receiver the chain has
// tested, so it never throws Node's errors, and its caret is never used.
func (l *lowerer) elementCaret(span ir.Span, n *ast.Node) ir.Span {
	return l.caretAfter(span, n.AsElementAccessExpression().Expression)
}

// calleeCaret is span with its caret at the method's name when n is a call
// of one, `recv.m(…)`. A call of a free function is recorded at its start,
// where span is already.
func (l *lowerer) calleeCaret(span ir.Span, n *ast.Node) ir.Span {
	if n.Kind != ast.KindCallExpression {
		return span
	}
	callee := n.AsCallExpression().Expression
	if callee.Kind != ast.KindPropertyAccessExpression {
		return span
	}
	return l.caretAt(span, callee.AsPropertyAccessExpression().Name())
}
