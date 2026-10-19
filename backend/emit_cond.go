package backend

import (
	"fmt"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// Conditionals (M5 plan D5): `c ? a : b`, and the `??` and `?.` lowering
// builds from one, with a Let holding the value they test. Go has no
// conditional expression, so each is a function literal, called where it
// stands, that evaluates the test and then one branch, and only that one:
//
//	var n float64 = func() float64 {
//		var ts_x_ts_u20_held1 cakebearrt.Maybe[float64] = f(ts_domain)
//		_ = ts_x_ts_u20_held1
//		if ts_x_ts_u20_held1.IsEmpty() {
//			return 0.0
//		}
//		return ts_x_ts_u20_held1.Must("f()")
//	}()
//
// The literal runs on the goroutine of the code around it, so an await in a
// branch awaits in that code's coroutine, and ts_domain and ts_co are that
// code's. Its statements run in order, each a whole expression ordered on
// its own (order.go), and the literal is a call, which Go orders among the
// calls around it: a read beside it that a branch's call could change is
// ordered by ts_read, since innerCall finds the call inside.
//
// A Cond in the else of another, and the Cond a Let holds its value for,
// are written into the same literal, so `a ? 1 : b ? 2 : 3` is one chain of
// ifs rather than a literal in a literal. A void one, `c ? f() : g()` as a
// statement, is a literal with no result.

// heldName is what the source calls the value a Let holds, for an unwrap's
// message: a Let's name is lowering's own, and means nothing to a reader.
type heldName struct {
	name  string
	named bool
}

// cond emits a Cond or a Let as a function literal called at once.
func (e *emitter) cond(x ir.Expr) string {
	depth := e.depth
	result := ""
	void := x.ExprType() == ir.Void
	if !void {
		result = " " + e.typ(x.ExprType(), x.ExprSpan())
	}
	body := e.capture(func() { e.chain(x, depth+1, void, map[string]bool{}) })
	e.depth = depth
	return fmt.Sprintf("func()%s {\n%s%s}()", result, body, strings.Repeat("\t", depth))
}

// chain writes x as the statements of a literal's body, at depth: a Cond as
// an if returning its then, followed by its else, and a Let as a local
// followed by its body, until something else is the result. declared holds
// the locals the literal has: a Let reusing a name has a literal of its own,
// since Go rejects a second declaration of a name in one scope.
func (e *emitter) chain(x ir.Expr, depth int, void bool, declared map[string]bool) {
	pad := strings.Repeat("\t", depth)
	e.depth = depth
	switch c := x.(type) {
	case *ir.Cond:
		fmt.Fprintf(&e.buf, "%sif %s {\n", pad, e.full(c.Test))
		e.result(c.Then, depth+1, void, true)
		fmt.Fprintf(&e.buf, "%s}\n", pad)
		e.chain(c.Else, depth, void, declared)
		return
	case *ir.Let:
		if !declared[c.Name] {
			declared[c.Name] = true
			name := mangle(c.Name)
			// Go rejects a local nothing reads, as for a VarDecl (stmt).
			fmt.Fprintf(&e.buf, "%svar %s %s = %s\n%s_ = %[2]s\n", pad, name, e.typ(c.Value.ExprType(), c.Span), e.full(c.Value), pad)
			defer e.hold(c)()
			e.chain(c.Body, depth, void, declared)
			return
		}
	}
	e.result(x, depth, void, false)
}

// result writes x as what the literal returns, at depth. A void literal
// evaluates it instead, and returns after it when more of the body follows.
func (e *emitter) result(x ir.Expr, depth int, void, more bool) {
	pad := strings.Repeat("\t", depth)
	e.depth = depth
	switch {
	case !void:
		fmt.Fprintf(&e.buf, "%sreturn %s\n", pad, e.full(x))
	case more:
		fmt.Fprintf(&e.buf, "%s%s\n%sreturn\n", pad, e.full(x), pad)
	default:
		fmt.Fprintf(&e.buf, "%s%s\n", pad, e.full(x))
	}
}

// hold records what the source calls the value let holds, if anything,
// while its body is emitted, and returns what puts back the name's previous
// meaning.
func (e *emitter) hold(let *ir.Let) func() {
	name, named := e.sourceName(let.Value)
	saved, had := e.held[let.Name]
	if e.held == nil {
		e.held = map[string]heldName{}
	}
	e.held[let.Name] = heldName{name, named}
	return func() {
		if had {
			e.held[let.Name] = saved
		} else {
			delete(e.held, let.Name)
		}
	}
}
