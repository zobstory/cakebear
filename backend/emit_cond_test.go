package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// cond is `test ? then : els`, of then's type.
func cond(test, then, els ir.Expr) *ir.Cond {
	return &ir.Cond{Test: test, Then: then, Else: els, Typ: then.ExprType(), Span: span}
}

// letIn holds value in name, a binding of lowering's own, for body.
func letIn(name string, value, body ir.Expr) *ir.Let {
	return &ir.Let{Name: name, Value: value, Body: body, Span: span}
}

// asserted is `v!`.
func asserted(v ir.Expr) *ir.Unwrap {
	u := must(v)
	u.Asserted = true
	return u
}

func callOf(name string, t ir.Type, args ...ir.Expr) *ir.Call {
	return &ir.Call{Callee: name, Args: args, Typ: t, Span: span}
}

// nullish is `x ?? d` for x a plain read: x unwrapped, or as it is when d
// may be empty too.
func nullish(x, d ir.Expr) *ir.Cond {
	var els ir.Expr = x
	if d.ExprType().Kind != ir.KindMaybe {
		els = must(x)
	}
	return cond(isEmpty(x, true, true), d, els)
}

// heldNullish is `x ?? d` for any other x, held in name and evaluated once.
func heldNullish(name string, x, d ir.Expr) *ir.Let {
	return letIn(name, x, nullish(stable(name, x.ExprType()), d))
}

// optional is `head?.rest`: head held in name, and rest read from it
// unwrapped when it isn't empty, and undefined when it is.
func optional(name string, head ir.Expr, rest func(ir.Expr) ir.Expr) *ir.Let {
	h := stable(name, head.ExprType())
	v := rest(must(h))
	return letIn(name, head, cond(isEmpty(h, true, true), undef(v.ExprType()), some(v)))
}

func elem(xs ir.Expr, i ir.Expr) *ir.IndexGet {
	return &ir.IndexGet{Array: xs, Index: i, Typ: *xs.ExprType().Elem, Span: span}
}

func strMethod(fn ir.Builtin, s ir.Expr) *ir.BuiltinCall {
	return &ir.BuiltinCall{Fn: fn, Args: []ir.Expr{s}, Typ: ir.String, Span: span}
}

// TestEmitCond: each form as the Go a reader would check it against. A
// conditional is a literal called in place, ordered among the calls beside
// it, its test and branches each ordered on their own; one in another's
// else, and the one a Let holds its value for, join that literal's chain;
// and a void one is a statement of its own.
func TestEmitCond(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	numM, ptM := ir.MaybeOf(ir.Number), ir.MaybeOf(ir.ObjectOf(pt))
	a, b, c, p := ident("a", ir.Number), ident("b", ir.Number), ident("c", ir.Boolean), stable("p", ptM)
	f := callOf("f", ir.Number)
	req := stable("req", ir.MaybeOf(ir.HostOf(ir.HostIncomingMessage)))
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{
			{Name: "f", Result: ir.Number, Span: span, Body: []ir.Stmt{assign("a", num(10)), ret(num(5))}},
			{Name: "g", Result: numM, Span: span, Body: []ir.Stmt{ret(undef(ir.Number))}},
			{Name: "say", Result: ir.Void, Span: span},
			{Name: "url", Params: []ir.Param{{Name: "req", Type: req.Typ, Span: span}}, Result: ir.String, Span: span,
				Body: []ir.Stmt{ret(&ir.HostProp{Prop: ir.HostURL, Recv: asserted(req), Typ: ir.String, Span: span})}},
		},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "a", Type: ir.Number, Init: num(1), Mutable: true, Span: span},
			&ir.VarDecl{Name: "b", Type: ir.Number, Init: num(2), Mutable: true, Span: span},
			&ir.VarDecl{Name: "c", Type: ir.Boolean, Init: boolLit(true), Mutable: true, Span: span},
			decl("p", ptM, undef(ir.ObjectOf(pt))),
			logNum(binary(ir.OpAdd, a, cond(c, b, f), ir.Number)),
			decl("grade", ir.String, cond(c, str("A"), cond(c, str("B"), str("C")))),
			decl("n", ir.Number, heldNullish("ts held1", callOf("g", numM), num(0))),
			logNum(optional("ts held1", p, func(x ir.Expr) ir.Expr { return get(x, "x") })),
			do(cond(c, callOf("say", ir.Void), callOf("say", ir.Void))),
			logNum(get(asserted(p), "x")),
			decl("w", ir.Number, letIn("ts held1", f, cond(c, num(1), letIn("ts held1", f, stable("ts held1", ir.Number))))),
		}}
	got := Emit(withShapes(m, &shapes))
	mustParse(t, got)
	for _, want := range []string{
		// The test and the branches have no call of their own, so they read
		// a, b and c as they are; the read of a beside the literal is ordered.
		"\tcakebearrt.LogNumber((ts_read(a) + func() float64 {\n\t\tif c {\n\t\t\treturn b\n\t\t}\n\t\treturn f(ts_domain)\n\t}()))\n",
		"\tgrade = func() string {\n\t\tif c {\n\t\t\treturn \"A\"\n\t\t}\n\t\tif c {\n\t\t\treturn \"B\"\n\t\t}\n\t\treturn \"C\"\n\t}()\n",
		// The held value is named as the source writes it, for Must.
		"\tn = func() float64 {\n\t\tvar ts_x_ts_u20_held1 cakebearrt.Maybe[float64] = g(ts_domain)\n\t\t_ = ts_x_ts_u20_held1\n" +
			"\t\tif ts_x_ts_u20_held1.IsEmpty() {\n\t\t\treturn 0.0\n\t\t}\n\t\treturn ts_x_ts_u20_held1.Must(\"g()\")\n\t}()\n",
		"\t\treturn cakebearrt.Some[float64](ts_x_ts_u20_held1.MustRead(\"p\", \"'x'\", &ts_sites[0]).x)\n",
		"\tfunc() {\n\t\tif c {\n\t\t\tsay(ts_domain)\n\t\t\treturn\n\t\t}\n\t\tsay(ts_domain)\n\t}()\n",
		"\tcakebearrt.LogNumber(p.AssertRead(\"p\", \"'x'\", &ts_sites[0]).x)\n",
		"\treturn req.AssertRead(\"req\", \"'url'\", &ts_sites[0]).URL()\n",
		// A second ts held1 in the chain would be declared twice.
		"\t\tif c {\n\t\t\treturn 1.0\n\t\t}\n\t\treturn func() float64 {\n\t\t\tvar ts_x_ts_u20_held1 float64 = f(ts_domain)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}

// TestHeldNames: an unwrap of a Let's name is named as the source names the
// value it holds, in its body only, and by an inner Let's while that one's
// body is emitted; a value the source doesn't name, by where the read is.
func TestHeldNames(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	o := stable("o", ir.ObjectOf(shapes.Intern("", []ir.Field{{Name: "len", Type: ir.MaybeOf(ir.Number)}})))
	held := stable("ts held1", ir.MaybeOf(ir.Number))
	awaited := &ir.Await{Value: stable("p", ir.PromiseOf(ir.MaybeOf(ir.Number))), Typ: ir.MaybeOf(ir.Number), Span: span}
	var e emitter
	check := func(when, want string) {
		t.Helper()
		if got := e.unwrapped(held); got != want {
			t.Errorf("%s: unwrapped = %q, want %q", when, got, want)
		}
	}
	outer := e.hold(letIn("ts held1", get(o, "len"), held))
	check("in the outer body", "o.len")
	inner := e.hold(letIn("ts held1", awaited, held))
	check("in the inner body", "the value at "+span.String())
	inner()
	check("back in the outer body", "o.len")
	outer()
	check("after both", "ts held1")
}

// TestLetReusingANameMatchesNode: a Let in a chain of another of the same
// name gets a literal of its own, where Go would reject the name declared
// twice. Lowering numbers its names, so no TypeScript reaches this, but the
// IR allows it: f() held and unread, then a conditional whose else holds
// f() again.
//
//	let count: number = 0;
//	function f(): number { count = count + 1; console.log("f", count); return count; }
//	const d: boolean = false;
//	console.log((f(), d ? 1 : f()));
func TestLetReusingANameMatchesNode(t *testing.T) {
	t.Parallel()

	count := ident("count", ir.Number)
	f := callOf("f", ir.Number)
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "f", Result: ir.Number, Span: span, Body: []ir.Stmt{
			assign("count", binary(ir.OpAdd, count, num(1), ir.Number)), logAll(str("f"), count), ret(count),
		}}},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "count", Type: ir.Number, Init: num(0), Mutable: true, Span: span},
			decl("d", ir.Boolean, boolLit(false)),
			logNum(letIn("ts held1", f, cond(stable("d", ir.Boolean), num(1), letIn("ts held1", f, stable("ts held1", ir.Number))))),
		}}
	if got, want := buildAndRun(t, ir.ProgramOf(m)), "f 1\nf 2\n2\n"; got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}
