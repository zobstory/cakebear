package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// strCall is the string builtin fn of args, a method of args[0], of type t.
func strCall(fn ir.Builtin, t ir.Type, args ...ir.Expr) *ir.BuiltinCall {
	return &ir.BuiltinCall{Fn: fn, Args: args, Typ: t, Span: span}
}

// unchecked is `xs[i]` where the read may be undefined (M5 plan D9), of t,
// OrUndefined of the element type.
func unchecked(xs, i ir.Expr, t ir.Type) *ir.IndexGet {
	return &ir.IndexGet{Array: xs, Index: i, Typ: t, OrUndefined: true, Span: span}
}

// TestEmitMembersThatMayBeUndefined: each read that may be undefined as the
// Go a reader would check it against. A runtime method gives the element
// and whether there was one, OrUndefined makes a Maybe of it, and for an
// array of maybes MaybeOrUndefined gives the element as it is. pop and
// shift as statements drop the element; s[i]'s receiver, unwrapped, fails
// as a read of the property i does.
func TestEmitMembersThatMayBeUndefined(t *testing.T) {
	t.Parallel()

	nums, numM, strM := ir.ArrayOf(ir.Number), ir.MaybeOf(ir.Number), ir.MaybeOf(ir.String)
	xs, ms := stable("xs", nums), stable("ms", ir.ArrayOf(numM))
	s, i := stable("s", ir.String), stable("i", ir.Number)
	gt := arrow(visit(ir.Number, "x", "", ""), ir.Boolean, ret(binary(ir.OpGreater, stable("x", ir.Number), num(1), ir.Boolean)))
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("xs", nums, arrayOf(ir.Number, num(1))),
		decl("ms", ms.Typ, arrayOf(numM, some(num(1)))),
		decl("s", ir.String, str("ab")),
		decl("ns", strM, some(str("ab"))),
		decl("i", ir.Number, num(0)),
		logAll(unchecked(xs, i, numM), unchecked(ms, num(0), numM)),
		logAll(method(ir.ArrayPop, xs, numM), method(ir.ArrayShift, ms, numM)),
		do(method(ir.ArrayPop, xs, ir.Void)),
		do(method(ir.ArrayShift, xs, ir.Void)),
		logAll(method(ir.ArrayAt, xs, numM, i), method(ir.ArrayAt, ms, numM, i)),
		logAll(method(ir.ArrayFind, xs, numM, gt), method(ir.ArrayFindLast, xs, numM, gt)),
		logAll(strCall(ir.StrAt, strM, s, i), strCall(ir.StrCodePointAt, numM, s, i)),
		logAll(strCall(ir.StrIndex, ir.String, s, i), strCall(ir.StrIndexOrUndefined, strM, s, i)),
		logAll(strCall(ir.StrIndex, ir.String, must(stable("ns", strM)), num(0))),
		logAll(strCall(ir.StrAt, strM, must(stable("ns", strM)), num(0))),
	}}
	got := Emit(ir.ProgramOf(m))
	mustParse(t, got)
	for _, want := range []string{
		"cakebearrt.OrUndefined(xs.Lookup(i))",
		"cakebearrt.MaybeOrUndefined(ms.Lookup(0.0))",
		"cakebearrt.OrUndefined(xs.Pop()), cakebearrt.MaybeOrUndefined(ms.Shift())",
		"\txs.Pop()\n\txs.Shift()\n",
		"cakebearrt.OrUndefined(xs.RelativeAt(i)), cakebearrt.MaybeOrUndefined(ms.RelativeAt(i))",
		"cakebearrt.OrUndefined(xs.Find(ts_domain, func(",
		"cakebearrt.OrUndefined(xs.FindLast(ts_domain, func(",
		"cakebearrt.StrAt(s, i), cakebearrt.StrCodePointAt(s, i)",
		"cakebearrt.StrIndex(s, i), cakebearrt.StrIndexOrUndefined(s, i)",
		`cakebearrt.StrIndex(ns.MustRead("ns", "'0'", &ts_sites[0]), 0.0)`,
		`cakebearrt.StrAt(ns.MustRead("ns", "'at'", &ts_sites[0]), 0.0)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}
