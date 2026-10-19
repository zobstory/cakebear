package lower

import (
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// consoleLog lowers console.log(a, b, …) (M4 plan D12): each argument a
// value the runtime can print, separated by spaces as Node separates them.
// Node takes a first string followed by more arguments as a format,
// replacing `%s` and the like, which cakebear doesn't do yet, so one is
// accepted there only as a literal with no `%` in it.
func (l *lowerer) consoleLog(n *ast.Node, c *ast.CallExpression) ir.Expr {
	var nodes []*ast.Node
	if c.Arguments != nil {
		nodes = c.Arguments.Nodes
	}
	args := make([]ir.Expr, 0, len(nodes))
	for i, a := range nodes {
		if a.Kind == ast.KindSpreadElement {
			l.failf(a, "spreading arguments into console.log is not supported yet")
			return nil
		}
		arg := l.expr(a)
		if arg == nil || !l.printable(a, arg.ExprType()) {
			return nil
		}
		if t := arg.ExprType(); i == 0 && len(nodes) > 1 && (stringish(t) || t.Kind == ir.KindMaybe && stringish(*t.Elem)) {
			if lit, ok := arg.(*ir.StringLit); !ok || strings.Contains(lit.Value, "%") {
				l.failf(a, "a string before console.log's other arguments is a format to Node, which cakebear doesn't implement yet: make it a string literal with no %%, or log it on its own")
				return nil
			}
		}
		args = append(args, arg)
	}
	return &ir.ConsoleLog{Args: args, Span: l.span(n)}
}

// printable refuses, at n, a value console.log can't print yet: a function,
// which a Go func value carries no name for (Node prints `[Function: f]`),
// a host value, a promise, or an object or array holding one.
func (l *lowerer) printable(n *ast.Node, t ir.Type) bool {
	if what := unprintable(t, nil); what != "" {
		switch t.Kind {
		case ir.KindPromise:
			l.failf(n, "console.log cannot print a promise yet; log what it resolves to, with await")
		case ir.KindInvalid, ir.KindFunc, ir.KindHost:
			l.failf(n, "console.log cannot print %s yet", what)
		default:
			l.failf(n, "console.log cannot print a %s yet: it holds %s", t, what)
		}
		return false
	}
	return true
}

// unprintable names what in a value of type t console.log can't print, or
// returns "".
func unprintable(t ir.Type, seen map[*ir.Shape]bool) string {
	switch t.Kind {
	case ir.KindInvalid:
		return "this type"
	case ir.KindFunc:
		return "a function"
	case ir.KindHost:
		return "a " + t.String()
	case ir.KindPromise:
		return "a promise"
	case ir.KindArray, ir.KindMaybe:
		if t.Elem != nil {
			return unprintable(*t.Elem, seen)
		}
	case ir.KindObject:
		if t.Shape == nil || seen[t.Shape] {
			return ""
		}
		if seen == nil {
			seen = map[*ir.Shape]bool{}
		}
		seen[t.Shape] = true
		for _, f := range t.Shape.Fields {
			if what := unprintable(f.Type, seen); what != "" {
				return what
			}
		}
	}
	return ""
}
