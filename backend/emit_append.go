package backend

import (
	"fmt"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// Appends (M5 plan D13, ir/append.go). A string binding the program appends
// to gets a cakebearrt.StrAcc beside it, declared in the same Go scope, so a
// closure capturing the binding captures its accumulator too. `s += x` is
// `s = ts_acc_s.Append(s, x)`, and `s.length` is `ts_acc_s.Length(s)`.
// Everything else about s is as before: it is a Go string.

// accName is the accumulator beside the binding goName names. mangle and the
// module prefixes never make a name starting ts_acc_ (mangle.go), so it meets
// no other.
func accName(goName string) string { return "ts_acc_" + goName }

// accDecl declares goName's accumulator, at pad.
func (e *emitter) accDecl(pad, goName string) string {
	e.usesRuntime = true
	return fmt.Sprintf("%svar %s %s.StrAcc\n", pad, accName(goName), rtPkg)
}

// paramAccs declares the accumulators of a function's Appended parameters, at
// the start of its body.
func (e *emitter) paramAccs(params []ir.Param, pad string) string {
	var b strings.Builder
	for _, p := range params {
		if p.Appended {
			b.WriteString(e.accDecl(pad, mangle(p.Name)))
		}
	}
	return b.String()
}

// appendStmt emits an append. Its target is read before its parts are
// evaluated, as JavaScript reads the left of `+=` first, so it is ordered
// with them as one expression's operands are (order.go): in `s += f()`, s is
// read before f can change it.
func (e *emitter) appendStmt(s *ir.Append) string {
	all := append([]ir.Expr{s.Target}, s.Parts...)
	saved := e.order
	e.order = innerCall(&ir.Concat{Parts: all})
	defer func() { e.order = saved }()
	args := make([]string, len(all))
	for i, p := range all {
		args[i] = e.expr(p)
	}
	name := e.top(s.Target.Module, s.Target.Name)
	e.usesRuntime = true
	return fmt.Sprintf("%s = %s.Append(%s)", name, accName(name), strings.Join(args, ", "))
}

// accLength emits `s.length` on an Appended binding through its accumulator,
// which knows it without scanning s. ok is false for any other length.
func (e *emitter) accLength(x *ir.BuiltinCall) (string, bool) {
	if x.Fn != ir.StrLength || len(x.Args) != 1 {
		return "", false
	}
	id, isIdent := x.Args[0].(*ir.Ident)
	if !isIdent || !id.Appended {
		return "", false
	}
	return fmt.Sprintf("%s.Length(%s)", accName(e.top(id.Module, id.Name)), e.expr(id)), true
}

// stringSum emits `+` on two strings as one StrConcat of every piece the
// chain joins: `a + b + c` was StrConcat(StrConcat(a, b), c), which made a
// string for a + b only to copy it again. Joining the pieces at once joins
// exactly the surrogate halves the nested calls would (joinsHalves looks
// past an empty piece), and Go evaluates the call's arguments in order.
func (e *emitter) stringSum(x *ir.Binary) string {
	parts := ir.StringParts(x)
	args := make([]string, len(parts))
	for i, p := range parts {
		args[i] = e.expr(p)
	}
	e.usesRuntime = true
	return fmt.Sprintf("%s.StrConcat(%s)", rtPkg, strings.Join(args, ", "))
}
