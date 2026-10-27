package lower

import (
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// consoleMembers are the members of console lowered, each with whether it
// writes to stderr, as Node 24.18's do (measured): error and warn there,
// info and debug where log writes. All five format as log does.
var consoleMembers = map[string]bool{"log": false, "info": false, "debug": false, "error": true, "warn": true}

// consoleMember reports whether n, a call's callee, is `console.m` for the
// console no module of the program's declares, and gives m: what runs is
// then Node's, however a lib or @types/node declares it. A program's own
// console is an ordinary value.
func (l *lowerer) consoleMember(n *ast.Node) (string, bool) {
	if n.Kind != ast.KindPropertyAccessExpression {
		return "", false
	}
	access := n.AsPropertyAccessExpression()
	recv := access.Expression
	if recv.Kind != ast.KindIdentifier || recv.Text() != "console" || access.Name() == nil || !l.libraryName(recv) { // library.go
		return "", false
	}
	return access.Name().Text(), true
}

// consoleLog lowers console.log(a, b, …) (M4 plan D12), and info, debug,
// error and warn (M6 step 6): each argument a value the runtime can print,
// separated by spaces as Node separates them. Node takes a first string
// followed by more arguments as a format, replacing `%s` and the like,
// which cakebear doesn't do yet, so one is accepted there only as a literal
// with no `%` in it.
func (l *lowerer) consoleLog(n *ast.Node, c *ast.CallExpression, member string) ir.Expr {
	stderr, ok := consoleMembers[member]
	if !ok {
		l.failf(n, "console.%s is not supported yet; console.log, info, debug, error and warn are", member)
		return nil
	}
	name := "console." + member
	var nodes []*ast.Node
	if c.Arguments != nil {
		nodes = c.Arguments.Nodes
	}
	args := make([]ir.Expr, 0, len(nodes))
	for i, a := range nodes {
		if a.Kind == ast.KindSpreadElement {
			l.failf(a, "spreading arguments into %s is not supported yet", name)
			return nil
		}
		arg := l.expr(a)
		if arg == nil || !l.printable(a, arg.ExprType(), name) {
			return nil
		}
		// An unknown may hold a string, and so a format, at run time (M6 D5).
		if t := arg.ExprType(); i == 0 && len(nodes) > 1 && (stringish(t) || t == ir.Unknown || t.Kind == ir.KindMaybe && stringish(*t.Elem)) {
			if lit, ok := arg.(*ir.StringLit); !ok || strings.Contains(lit.Value, "%") {
				l.failf(a, "a string before %s's other arguments is a format to Node, which cakebear doesn't implement yet: make it a string literal with no %%, or log it on its own", name)
				return nil
			}
		}
		args = append(args, arg)
	}
	return &ir.ConsoleLog{Args: args, Stderr: stderr, Span: l.span(n)}
}

// printable refuses, at n, a value name (console.log, say) can't print yet:
// a function, which a Go func value carries no name for (Node prints
// `[Function: f]`), a host value, a promise, or an object or array holding
// one.
func (l *lowerer) printable(n *ast.Node, t ir.Type, name string) bool {
	if what := unprintable(t, nil); what != "" {
		switch t.Kind {
		case ir.KindPromise:
			l.failf(n, "%s cannot print a promise yet; log what it resolves to, with await", name)
		case ir.KindInvalid, ir.KindFunc, ir.KindHost:
			l.failf(n, "%s cannot print %s yet", name, what)
		default:
			l.failf(n, "%s cannot print a %s yet: it holds %s", name, t, what)
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
