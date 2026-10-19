package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// Appends to a string binding (M5 plan D13, ir/append.go). A string let or
// parameter the program appends to is marked Appended, at its declaration
// and every reference, and the append itself is an ir.Append, so the backend
// can keep an accumulator beside the binding and make a loop of appends
// linear. Measured on wx-next, 146 of its 148 append sites have such a
// binding as their target (spikes/m5-measure/reports/append_sites.md).

// findAppended records the bindings an append targets: `s += x`,
// `s = s + …`, and a template beginning with `${s}` assigned to s, on a
// string, where s is a let declared on its own or a parameter with a plain
// name. A let in a for loop's first clause is left out, since each iteration
// has a binding of its own, and so is a destructured one; an append to
// either is lowered as before.
func (p *program) findAppended() {
	p.appended = map[*ast.Symbol]bool{}
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n.Kind == ast.KindBinaryExpression {
			if sym := p.appendTarget(n.AsBinaryExpression()); sym != nil {
				p.appended[sym] = true
			}
		}
		n.ForEachChild(visit)
		return false
	}
	for _, f := range p.files {
		f.AsNode().ForEachChild(visit)
	}
}

// appendTarget is the binding b appends to, or nil when b is no append to
// one findAppended keeps.
func (p *program) appendTarget(b *ast.BinaryExpression) *ast.Symbol {
	left := b.Left
	if left.Kind != ast.KindIdentifier {
		return nil
	}
	sym := p.resolve(p.checker.GetSymbolAtLocation(left))
	// The binding's declared type, not the one at this use: a string |
	// undefined narrowed to a string here is still stored as a maybe.
	if sym == nil || !appendable(sym.ValueDeclaration()) || p.checker.GetTypeOfSymbol(sym).Flags()&checker.TypeFlagsString == 0 {
		return nil
	}
	switch b.OperatorToken.Kind {
	case ast.KindPlusEqualsToken:
		return sym
	case ast.KindEqualsToken:
		if first := firstPiece(b.Right); first != nil && first.Kind == ast.KindIdentifier &&
			p.resolve(p.checker.GetSymbolAtLocation(first)) == sym {
			return sym
		}
	}
	return nil
}

// appendable reports whether decl declares a binding an accumulator can sit
// beside: a let of its own statement, or a parameter, named plainly.
func appendable(decl *ast.Node) bool {
	if decl == nil || decl.Name() == nil || decl.Name().Kind != ast.KindIdentifier {
		return false
	}
	switch decl.Kind {
	case ast.KindVariableDeclaration:
		list := decl.Parent
		return list != nil && list.Kind == ast.KindVariableDeclarationList && list.Flags&ast.NodeFlagsLet != 0 &&
			list.Parent != nil && list.Parent.Kind == ast.KindVariableStatement
	case ast.KindParameter:
		return decl.AsParameterDeclaration().DotDotDotToken == nil
	}
	return false
}

// firstPiece is the first piece a concatenation joins: the leftmost operand
// of a chain of +, or a template's first substitution when no text comes
// before it.
func firstPiece(n *ast.Node) *ast.Node {
	for {
		n = ast.SkipParentheses(n)
		switch n.Kind {
		case ast.KindBinaryExpression:
			b := n.AsBinaryExpression()
			if b.OperatorToken.Kind != ast.KindPlusToken {
				return n
			}
			n = b.Left
		case ast.KindTemplateExpression:
			t := n.AsTemplateExpression()
			if t.Head.Text() != "" || len(t.TemplateSpans.Nodes) == 0 {
				return n
			}
			n = t.TemplateSpans.Nodes[0].AsTemplateSpan().Expression
		default:
			return n
		}
	}
}

// appended reports whether n, a declaration's name or a reference, names a
// binding findAppended recorded.
func (l *lowerer) appended(n *ast.Node) bool {
	sym := l.p.resolve(l.valueSymbol(n))
	return sym != nil && l.p.appended[sym]
}

// appendStmt is the append `target = value` makes, when target is an
// Appended binding and value begins with it: `s = s + x`, or a template
// whose first part is `${s}`. It is nil for any other assignment.
func appendStmt(stmt ir.Span, name, module string, value ir.Expr) *ir.Append {
	parts := ir.StringParts(value)
	if len(parts) < 2 {
		return nil
	}
	first, ok := parts[0].(*ir.Ident)
	if !ok || !first.Appended || first.Name != name || first.Module != module {
		return nil
	}
	return &ir.Append{Target: first, Parts: parts[1:], Span: stmt}
}
