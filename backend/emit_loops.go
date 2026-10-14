package backend

import (
	"fmt"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// forStmt emits `for (init; cond; post)`. The loop's own variables are a
// short declaration in Go's init clause, which is what makes them Go's
// per-iteration loop variables: since Go 1.22 each iteration has its own,
// copied before the post statement runs, which is JavaScript's `let` in a
// for loop exactly. The generated go.mod follows the running toolchain
// (backend.go), and a test holds the semantics.
func (e *emitter) forStmt(s *ir.For, pad string, depth int) {
	init := e.forInit(s.Init)
	cond := ""
	if s.Cond != nil {
		cond = e.full(s.Cond)
	}
	post := ""
	if len(s.Post) == 1 {
		post = e.simple(s.Post[0])
	}
	fmt.Fprintf(&e.buf, "%sfor %s; %s; %s {\n", pad, init, cond, post)
	// Go rejects an unused loop variable; TypeScript does not.
	for _, st := range s.Init {
		if d, ok := st.(*ir.VarDecl); ok {
			fmt.Fprintf(&e.buf, "%s\t_ = %s\n", pad, mangle(d.Name))
		}
	}
	e.stmts(s.Body, depth+1)
	fmt.Fprintf(&e.buf, "%s}\n", pad)
}

// forInit renders a for loop's first clause as one Go simple statement: its
// variables as one short declaration, or an assignment.
func (e *emitter) forInit(init []ir.Stmt) string {
	var decls []*ir.VarDecl
	for _, s := range init {
		d, ok := s.(*ir.VarDecl)
		if !ok {
			return e.simple(s) // an assignment or a call, which lowering allows only one of
		}
		decls = append(decls, d)
	}
	if len(decls) == 0 {
		return ""
	}
	// The initialisers are evaluated as one list, left to right. A call in
	// any of them is one the others' reads must be ordered against
	// (order.go), and with one, a call below its root is.
	ordered := innerCall(decls[0].Init)
	if len(decls) > 1 {
		for _, d := range decls {
			ordered = ordered || isCall(d.Init) || innerCall(d.Init)
		}
	}
	saved := e.order
	e.order = ordered
	defer func() { e.order = saved }()

	names := make([]string, len(decls))
	values := make([]string, len(decls))
	for i, d := range decls {
		names[i] = mangle(d.Name)
		values[i] = conversion(e.typ(d.Type), e.expr(d.Init))
	}
	return strings.Join(names, ", ") + " := " + strings.Join(values, ", ")
}

// simple renders an assignment or an expression statement as a Go simple
// statement, for a loop clause.
func (e *emitter) simple(s ir.Stmt) string {
	switch s := s.(type) {
	case *ir.Assign:
		return e.top(s.Module, s.Name) + " = " + e.full(s.Value)
	case *ir.ExprStmt:
		if isCall(s.Expr) {
			return e.full(s.Expr)
		}
		return "_ = " + e.full(s.Expr)
	}
	e.unsupported = append(e.unsupported, Unsupported{Span: s.StmtSpan(), What: describeUnsupported(s)})
	return "#error unhandled loop clause"
}

// conversion converts a value to the Go type typ, so a short declaration
// gets the type TypeScript's annotation says rather than the one Go would
// infer from the value. A type that isn't one word is parenthesised.
func conversion(typ, value string) string {
	if strings.ContainsAny(typ, " ()[]*") {
		return "(" + typ + ")(" + value + ")"
	}
	return typ + "(" + value + ")"
}

// body writes a function's statements. One with a result that doesn't end in
// a return gets a panic to end it, because Go requires a function's last
// statement to be one Go can see terminates. TypeScript's own analysis is
// wider: `while (true)`, or an `if (true)` that returns, ends a function for
// TypeScript, but `for true` and `if true` don't for Go. cakebear builds with
// strictNullChecks, and no type the subset lowers admits undefined, so
// TypeScript reports TS2366 for any function whose end it can reach: the
// panic only runs if that analysis was wrong.
func (e *emitter) body(stmts []ir.Stmt, depth int, result bool) {
	e.stmts(stmts, depth)
	if !result {
		return
	}
	if len(stmts) > 0 {
		if _, ok := stmts[len(stmts)-1].(*ir.Return); ok {
			return
		}
	}
	fmt.Fprintf(&e.buf, "%spanic(\"cakebear: reached the end of a function TypeScript proved cannot end\")\n", strings.Repeat("\t", depth))
}
