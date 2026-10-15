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
	if names := loopNames(s.Init); len(names) > 1 {
		// Go evaluates every value of a short declaration before it declares
		// any of the names, so `i, j := 0, i + 1` would read an outer i where
		// JavaScript reads the loop's own. The declarations run first, in
		// order, in a block of their own, and the clause redeclares the names
		// from them, which still gives each iteration its own copies.
		fmt.Fprintf(&e.buf, "%s{\n", pad)
		e.stmts(s.Init, depth+1)
		list := strings.Join(names, ", ")
		e.loop(s, list+" := "+list, pad+"\t", depth+1)
		fmt.Fprintf(&e.buf, "%s}\n", pad)
		return
	}
	e.loop(s, e.forInit(s.Init), pad, depth)
}

// loopNames returns the Go names of the variables a for loop's first clause
// declares, or nil when the clause is not declarations.
func loopNames(init []ir.Stmt) []string {
	var names []string
	for _, s := range init {
		d, ok := s.(*ir.VarDecl)
		if !ok {
			return nil
		}
		names = append(names, mangle(d.Name))
	}
	return names
}

// loop writes the for statement itself, with init as its first clause.
func (e *emitter) loop(s *ir.For, init, pad string, depth int) {
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
// one variable as a short declaration, or an assignment. forStmt declares two
// or more before the loop.
func (e *emitter) forInit(init []ir.Stmt) string {
	if len(init) == 0 {
		return ""
	}
	d, ok := init[0].(*ir.VarDecl)
	if !ok {
		return e.simple(init[0]) // an assignment or a call, which lowering allows only one of
	}
	// A call below the initialiser's root is one its reads must be ordered
	// against (order.go).
	saved := e.order
	e.order = innerCall(d.Init)
	defer func() { e.order = saved }()
	return mangle(d.Name) + " := " + conversion(e.typ(d.Type, d.Span), e.expr(d.Init))
}

// simple renders an assignment or an expression statement as a Go simple
// statement, for a loop clause.
func (e *emitter) simple(s ir.Stmt) string {
	switch s := s.(type) {
	case *ir.Assign:
		return e.top(s.Module, s.Name) + " = " + e.full(s.Value)
	case *ir.FieldSet:
		return e.fieldSet(s)
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
