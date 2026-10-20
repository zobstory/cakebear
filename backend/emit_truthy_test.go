package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestEmitTruthy: each kind's rule as the Go a reader would check it
// against, a maybe's with its element's rule inside. An undefined Empty is
// parenthesised as Truthy's receiver, since Go would read a composite
// literal starting an if's condition as its block. A Truthy is no call to
// the order pass, so `!t` reads t bare though t is written to.
func TestEmitTruthy(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	obj := ir.ObjectOf(pt)
	values := []namedValue{
		{"s", str("x")}, {"e", conv(ir.Base64, str("aGk="))}, {"i", conv(ir.Int32, num(1))}, {"u", conv(ir.Uint64, num(1))},
		{"n", num(1)}, {"h", conv(ir.Float32, num(0.5))}, {"b", boolLit(true)}, {"o", lit(pt, "x", num(0), "y", num(0))},
		{"m", some(num(1))}, {"ms", some(str("x"))}, {"mi", some(conv(ir.Int32, num(1)))}, {"mb", some(boolLit(true))},
		{"mf", some(conv(ir.Float32, num(0.5)))}, {"mp", undef(obj)},
	}
	main := []ir.Stmt{&ir.VarDecl{Name: "t", Type: ir.String, Init: str("y"), Mutable: true, Span: span}}
	for _, v := range values {
		main = append(main, decl(v.name, v.init.ExprType(), v.init))
	}
	for _, v := range values {
		main = append(main, &ir.If{Cond: truthyOf(stable(v.name, v.init.ExprType())), Span: span})
	}
	main = append(main,
		&ir.If{Cond: truthyOf(undef(ir.Number)), Span: span},
		&ir.If{Cond: truthyOf(&ir.NullLit{Span: span}), Span: span},
		do(not(truthyOf(ident("t", ir.String)))),
	)
	got := Emit(withShapes(&ir.Module{Name: "t", Main: main}, &shapes))
	mustParse(t, got)
	for _, want := range []string{
		"\tif (s != \"\") {\n",
		"\tif (e != \"\") {\n",
		"\tif (i != 0) {\n",
		"\tif (u != 0) {\n",
		"\tif cakebearrt.TruthyNumber(n) {\n",
		"\tif cakebearrt.TruthyNumber(float64(h)) {\n",
		"\tif b {\n",
		"\tif cakebearrt.AlwaysTruthy(o) {\n",
		"\tif m.Truthy(cakebearrt.TruthyNumber) {\n",
		"\tif ms.Truthy(func(v string) bool { return (v != \"\") }) {\n",
		"\tif mi.Truthy(func(v int32) bool { return (v != 0) }) {\n",
		"\tif mb.Truthy(func(v bool) bool { return v }) {\n",
		"\tif mf.Truthy(func(v float32) bool { return cakebearrt.TruthyNumber(float64(v)) }) {\n",
		"\tif !mp.IsEmpty() {\n",
		"\tif (cakebearrt.Maybe[float64]{}).Truthy(cakebearrt.TruthyNumber) {\n",
		"\tif false {\n",
		"\t_ = (!(t != \"\"))\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}

// TestTruthyImportsTheRuntimeOnlyForItsCalls: a string's test is a
// comparison, which needs nothing of the runtime, and a number's is the
// runtime's, so a program doing nothing else imports the runtime for the
// second, without which it wouldn't build, and not for the first.
//
//	const s: string = "x"; if (s) { }
//	const n: number = 1; if (n) { }
func TestTruthyImportsTheRuntimeOnlyForItsCalls(t *testing.T) {
	t.Parallel()

	for _, v := range []ir.Expr{str("x"), num(1)} {
		p := ir.ProgramOf(&ir.Module{Name: "t", Main: []ir.Stmt{
			decl("v", v.ExprType(), v),
			&ir.If{Cond: truthyOf(stable("v", v.ExprType())), Span: span},
		}})
		src := Emit(p)
		if imports, want := strings.Contains(src, "import "+rtPkg+" "), v.ExprType() == ir.Number; imports != want {
			t.Errorf("testing a %s imports the runtime: %v, want %v\n%s", v.ExprType(), imports, want, src)
		}
		if got := buildAndRun(t, p); got != "" {
			t.Errorf("testing a %s printed %q", v.ExprType(), got)
		}
	}
}

// TestTruthyOfAConditionalMatchesNode: an optional chain and `??` tested,
// in an if, a while, under `!` and as a ternary's test, whose value is a
// function literal called in place, and the receiver of a maybe's Truthy in
// an if's header. A call held by `??` runs once per test.
//
//	interface User { name: string }
//	const ada: User | undefined = { name: "Ada" };
//	const blank: User | undefined = { name: "" };
//	const nobody = null as User | null;
//	function next(v: number | undefined): number | undefined { console.log("next"); return v; }
//	console.log(!ada?.name, !blank?.name, !nobody?.name);
//	if (ada?.name) { console.log("if ada"); }
//	if (blank?.name) { console.log("if blank"); } else { console.log("else blank"); }
//	while (nobody?.name) { console.log("never"); break; }
//	console.log(ada?.name ? "named" : "anonymous", blank?.name ? "named" : "anonymous");
//	console.log(!(next(undefined) ?? 0), !(next(undefined) ?? 7), !(next(0) ?? 7));
//	if (next(undefined) ?? 1) { console.log("default"); }
func TestTruthyOfAConditionalMatchesNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	user := shapes.Intern("User", []ir.Field{{Name: "name", Type: ir.String}})
	userM, numM := ir.MaybeOf(ir.ObjectOf(user)), ir.MaybeOf(ir.Number)
	name := func(who string) ir.Expr {
		return optional("ts held1", stable(who, userM), func(x ir.Expr) ir.Expr { return get(x, "name") })
	}
	next := func(v ir.Expr, d float64) ir.Expr { return heldNullish("ts held1", callOf("next", numM, v), num(d)) }
	named := func(who string) ir.Expr { return cond(truthyOf(name(who)), str("named"), str("anonymous")) }
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "next", Params: []ir.Param{{Name: "v", Type: numM, Span: span}}, Result: numM, Span: span,
			Body: []ir.Stmt{logStr("next"), ret(stable("v", numM))}}},
		Main: []ir.Stmt{
			decl("ada", userM, some(lit(user, "name", str("Ada")))),
			decl("blank", userM, some(lit(user, "name", str("")))),
			decl("nobody", userM, null(ir.ObjectOf(user))),
			logAll(not(truthyOf(name("ada"))), not(truthyOf(name("blank"))), not(truthyOf(name("nobody")))),
			&ir.If{Cond: truthyOf(name("ada")), Then: []ir.Stmt{logStr("if ada")}, Span: span},
			&ir.If{Cond: truthyOf(name("blank")), Then: []ir.Stmt{logStr("if blank")}, Else: []ir.Stmt{logStr("else blank")}, Span: span},
			&ir.While{Cond: truthyOf(name("nobody")), Body: []ir.Stmt{logStr("never"), &ir.Break{Span: span}}, Span: span},
			logAll(named("ada"), named("blank")),
			logAll(not(truthyOf(next(undef(ir.Number), 0))), not(truthyOf(next(undef(ir.Number), 7))), not(truthyOf(next(some(num(0)), 7)))),
			&ir.If{Cond: truthyOf(next(undef(ir.Number), 1)), Then: []ir.Stmt{logStr("default")}, Span: span},
		}}
	want := "false true true\nif ada\nelse blank\nnamed anonymous\nnext\nnext\nnext\ntrue false true\nnext\ndefault\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestTruthyEvaluatesOnce: a value that is always truthy is still
// evaluated, once, so a call's side effects run, as they do for the values
// whose truthiness depends on what they hold. calls, read after each, has
// gone up by one per test; a node:http server is truthy too.
//
//	import { createServer } from "node:http";
//	let calls: number = 0;
//	function make(): Point { calls = calls + 1; return { x: 0, y: 0 }; }
//	function list(): number[] { calls = calls + 1; return []; }
//	function pick(): () => number { calls = calls + 1; return (): number => 0; }
//	function later(): Promise<number> { calls = calls + 1; return new Promise((resolve) => { resolve(1); }); }
//	function zero(): number { calls = calls + 1; return 0; }
//	function blank(): string { calls = calls + 1; return ""; }
//	function none(): number | undefined { calls = calls + 1; return undefined; }
//	function boxed(): Point | null { calls = calls + 1; return { x: 0, y: 0 }; }
//	if (make()) { console.log("make", calls); }
//	console.log(!list(), calls);
//	console.log(pick() ? "pick" : "none", calls);
//	while (later()) { console.log("later", calls); break; }
//	console.log(!zero(), zero() ? 1 : 2, calls);
//	if (blank()) { console.log("blank"); } else { console.log("no blank", calls); }
//	console.log(!none(), !boxed(), calls);
//	const server = createServer((req, res) => { res.end(); });
//	console.log(!server, server ? "server" : "none");
func TestTruthyEvaluatesOnce(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	obj, nums, thunk := ir.ObjectOf(pt), ir.ArrayOf(ir.Number), ir.FuncType(nil, ir.Number)
	numP, numM, objM := ir.PromiseOf(ir.Number), ir.MaybeOf(ir.Number), ir.MaybeOf(obj)
	calls := ident("calls", ir.Number)
	counted := func(name string, result ir.Type, v ir.Expr) *ir.Func {
		return &ir.Func{Name: name, Result: result, Span: span,
			Body: []ir.Stmt{assign("calls", binary(ir.OpAdd, calls, num(1), ir.Number)), ret(v)}}
	}
	executor := &ir.FuncLit{Params: []ir.Param{{Name: "resolve", Type: ir.ResolveType(ir.Number), Span: span}},
		Result: ir.Void, Typ: ir.ExecutorType(ir.Number), Span: span, Body: []ir.Stmt{do(callOf("resolve", ir.Void, num(1)))}}
	req, res := ir.HostOf(ir.HostIncomingMessage), ir.HostOf(ir.HostServerResponse)
	server := ir.HostOf(ir.HostHTTPServer)
	handler := &ir.FuncLit{Params: []ir.Param{{Name: "req", Type: req, Span: span}, {Name: "res", Type: res, Span: span}},
		Result: ir.Void, Typ: ir.FuncType([]ir.Type{req, res}, ir.Void), Span: span,
		Body: []ir.Stmt{do(&ir.HostCall{Op: ir.HostEnd, Recv: stable("res", res), Typ: res, Span: span})}}
	srv := stable("server", server)
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{
			counted("make", obj, lit(pt, "x", num(0), "y", num(0))),
			counted("list", nums, arrayOf(ir.Number)),
			counted("pick", thunk, arrow(nil, ir.Number, ret(num(0)))),
			counted("later", numP, &ir.NewPromise{Executor: executor, Typ: numP, Span: span}),
			counted("zero", ir.Number, num(0)),
			counted("blank", ir.String, str("")),
			counted("none", numM, undef(ir.Number)),
			counted("boxed", objM, some(lit(pt, "x", num(0), "y", num(0)))),
		},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "calls", Type: ir.Number, Init: num(0), Mutable: true, Span: span},
			&ir.If{Cond: truthyOf(callOf("make", obj)), Then: []ir.Stmt{logAll(str("make"), calls)}, Span: span},
			logAll(not(truthyOf(callOf("list", nums))), calls),
			logAll(cond(truthyOf(callOf("pick", thunk)), str("pick"), str("none")), calls),
			&ir.While{Cond: truthyOf(callOf("later", numP)), Body: []ir.Stmt{logAll(str("later"), calls), &ir.Break{Span: span}}, Span: span},
			logAll(not(truthyOf(callOf("zero", ir.Number))), cond(truthyOf(callOf("zero", ir.Number)), num(1), num(2)), calls),
			&ir.If{Cond: truthyOf(callOf("blank", ir.String)), Span: span,
				Then: []ir.Stmt{logStr("blank")}, Else: []ir.Stmt{logAll(str("no blank"), calls)}},
			logAll(not(truthyOf(callOf("none", numM))), not(truthyOf(callOf("boxed", objM))), calls),
			decl("server", server, &ir.HostCall{Op: ir.HostCreateServer, Args: []ir.Expr{handler}, Typ: server, Span: span}),
			logAll(not(truthyOf(srv)), cond(truthyOf(srv), str("server"), str("none"))),
		}}
	want := "make 1\nfalse 2\npick 3\nlater 4\ntrue 2 6\nno blank 7\ntrue false 9\nfalse server\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestTruthyEvaluationOrderMatchesNode: JavaScript's left-to-right order
// holds through a Truthy: a read beside a call inside one, and the reads
// inside one beside a call that changes what they read, to its left and to
// its right, of a string, a number and a maybe.
//
//	let s: string = "x"; let n: number = 1; let m: number | undefined = 1;
//	function clear(): number { s = ""; n = 0; m = undefined; return 0; }
//	console.log("s", s, !clear());
//	s = "x"; n = 1; m = 1;
//	console.log(!s, !n, !m, clear());
//	s = "x"; n = 1; m = 1;
//	console.log(clear(), !s, !n, !m);
//	s = "x";
//	const r: boolean = !s === (clear() === 0);
//	console.log(r);
//	s = "x";
//	console.log(s ? "kept" : "cleared", clear());
func TestTruthyEvaluationOrderMatchesNode(t *testing.T) {
	t.Parallel()

	numM := ir.MaybeOf(ir.Number)
	s, n, m := ident("s", ir.String), ident("n", ir.Number), ident("m", numM)
	wipe := callOf("clear", ir.Number)
	reset := []ir.Stmt{assign("s", str("x")), assign("n", num(1)), assign("m", some(num(1)))}
	main := []ir.Stmt{
		&ir.VarDecl{Name: "s", Type: ir.String, Init: str("x"), Mutable: true, Span: span},
		&ir.VarDecl{Name: "n", Type: ir.Number, Init: num(1), Mutable: true, Span: span},
		&ir.VarDecl{Name: "m", Type: numM, Init: some(num(1)), Mutable: true, Span: span},
		logAll(str("s"), s, not(truthyOf(wipe))),
	}
	main = append(main, reset...)
	main = append(main, logAll(not(truthyOf(s)), not(truthyOf(n)), not(truthyOf(m)), wipe))
	main = append(main, reset...)
	main = append(main, logAll(wipe, not(truthyOf(s)), not(truthyOf(n)), not(truthyOf(m))),
		assign("s", str("x")),
		decl("r", ir.Boolean, binary(ir.OpEqual, not(truthyOf(s)), binary(ir.OpEqual, wipe, num(0), ir.Boolean), ir.Boolean)),
		logNum(stable("r", ir.Boolean)),
		assign("s", str("x")),
		logAll(cond(truthyOf(s), str("kept"), str("cleared")), wipe),
	)
	fn := &ir.Func{Name: "clear", Result: ir.Number, Span: span,
		Body: []ir.Stmt{assign("s", str("")), assign("n", num(0)), assign("m", undef(ir.Number)), ret(num(0))}}
	want := "s x true\nfalse false false 0\n0 true true true\nfalse\nkept 0\n"
	if got := buildAndRun(t, ir.ProgramOf(&ir.Module{Name: "t", Funcs: []*ir.Func{fn}, Main: main})); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}
