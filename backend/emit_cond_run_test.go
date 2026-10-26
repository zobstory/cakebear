package backend

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// Conditionals, `??`, `?.` and `!` (M5 step 4), built from IR by hand in the
// shapes lowering builds them in. What each program prints is what Node
// 24.18 printed for the TypeScript in its comment, the same in three runs.

// TestConditionalsMatchNode: `?:` as each kind of operand, nested in either
// branch, and in closures, with only the chosen branch's side effects.
//
//	function side(tag: string, v: number): number { console.log(tag); return v; }
//	function say(s: string): void { console.log(s); }
//	function pick(c: boolean): string { return c ? "yes" : "no"; }
//	const c: boolean = true; const d: boolean = false;
//	const a: number = c ? side("then", 1) : side("else", 2);
//	console.log(a);
//	console.log(d ? side("then", 1) : side("else", 2));
//	const n: number = 7;
//	const grade: string = n >= 9 ? "A" : n >= 5 ? "B" : "C";
//	console.log(grade);
//	console.log(c ? (d ? 1 : 2) : 3);
//	const p: Point = { x: c ? 1 : 2, y: d ? 3 : 4 };
//	console.log(p);
//	console.log((c ? 10 : 20) + (d ? 1 : 2));
//	console.log((c ? "ab" : "cd").toUpperCase());
//	const xs: number[] = [1, 2]; const ys: number[] = [3];
//	console.log((d ? xs : ys).length);
//	const origin: Point = { x: 0, y: 0 };
//	console.log((c ? p : origin).x);
//	if (c ? n > 5 : false) { console.log("if"); }
//	let i: number = 0;
//	while (i < 3 ? true : false) { i = i + 1; }
//	console.log(i);
//	for (let j: number = 0; c ? j < 2 : false; j = j + 1) { console.log("for", j); }
//	for (let q: number = 0; q < 1; c ? say("post") : say("never")) { q = 1; }
//	c ? side("stmt then", 0) : side("stmt else", 0);
//	d ? say("void then") : say("void else");
//	c ? say("void then") : say("void else");
//	console.log(pick(true), pick(false));
//	const f: (k: number) => number = c ? (k: number): number => k * 2 : (k: number): number => k;
//	console.log(f(4));
//	const abs = (v: number): number => v < 0 ? -v : v;
//	console.log(abs(-3), abs(2));
//	console.log(xs.map((x: number): number => x > 1 ? x * 10 : 0));
func TestConditionalsMatchNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	obj, nums := ir.ObjectOf(pt), ir.ArrayOf(ir.Number)
	c, d, n := stable("c", ir.Boolean), stable("d", ir.Boolean), stable("n", ir.Number)
	p, xs, ys := stable("p", obj), stable("xs", nums), stable("ys", nums)
	i, j := ident("i", ir.Number), ident("j", ir.Number)
	k, v, x := stable("k", ir.Number), stable("v", ir.Number), stable("x", ir.Number)
	side := func(tag string, v float64) ir.Expr { return callOf("side", ir.Number, str(tag), num(v)) }
	say := func(s string) ir.Expr { return callOf("say", ir.Void, str(s)) }
	cmp := func(op ir.BinaryOp, l ir.Expr, r float64) ir.Expr { return binary(op, l, num(r), ir.Boolean) }
	param := func(name string, t ir.Type) []ir.Param { return []ir.Param{{Name: name, Type: t, Span: span}} }
	fn := ir.FuncType([]ir.Type{ir.Number}, ir.Number)
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{
			{Name: "side", Params: append(param("tag", ir.String), param("v", ir.Number)...), Result: ir.Number, Span: span,
				Body: []ir.Stmt{logNum(stable("tag", ir.String)), ret(v)}},
			{Name: "say", Params: param("s", ir.String), Result: ir.Void, Span: span, Body: []ir.Stmt{logNum(stable("s", ir.String))}},
			{Name: "pick", Params: param("c", ir.Boolean), Result: ir.String, Span: span, Body: []ir.Stmt{ret(cond(c, str("yes"), str("no")))}},
		},
		Main: []ir.Stmt{
			decl("c", ir.Boolean, boolLit(true)), decl("d", ir.Boolean, boolLit(false)),
			decl("a", ir.Number, cond(c, side("then", 1), side("else", 2))),
			logNum(stable("a", ir.Number)),
			logNum(cond(d, side("then", 1), side("else", 2))),
			decl("n", ir.Number, num(7)),
			decl("grade", ir.String, cond(cmp(ir.OpGreaterEq, n, 9), str("A"), cond(cmp(ir.OpGreaterEq, n, 5), str("B"), str("C")))),
			logNum(stable("grade", ir.String)),
			logNum(cond(c, cond(d, num(1), num(2)), num(3))),
			decl("p", obj, lit(pt, "x", cond(c, num(1), num(2)), "y", cond(d, num(3), num(4)))),
			logNum(p),
			logNum(binary(ir.OpAdd, cond(c, num(10), num(20)), cond(d, num(1), num(2)), ir.Number)),
			logNum(strMethod(ir.StrToUpperCase, cond(c, str("ab"), str("cd")))),
			decl("xs", nums, arrayOf(ir.Number, num(1), num(2))), decl("ys", nums, arrayOf(ir.Number, num(3))),
			logNum(method(ir.ArrayLength, cond(d, xs, ys), ir.Number)),
			decl("origin", obj, lit(pt, "x", num(0), "y", num(0))),
			logNum(get(cond(c, p, stable("origin", obj)), "x")),
			&ir.If{Cond: cond(c, cmp(ir.OpGreater, n, 5), boolLit(false)), Then: []ir.Stmt{logStr("if")}, Span: span},
			&ir.VarDecl{Name: "i", Type: ir.Number, Init: num(0), Mutable: true, Span: span},
			&ir.While{Cond: cond(cmp(ir.OpLess, i, 3), boolLit(true), boolLit(false)), Span: span,
				Body: []ir.Stmt{assign("i", binary(ir.OpAdd, i, num(1), ir.Number))}},
			logNum(i),
			&ir.For{Init: []ir.Stmt{&ir.VarDecl{Name: "j", Type: ir.Number, Init: num(0), Mutable: true, Span: span}},
				Cond: cond(c, cmp(ir.OpLess, j, 2), boolLit(false)),
				Post: []ir.Stmt{assign("j", binary(ir.OpAdd, j, num(1), ir.Number))},
				Body: []ir.Stmt{logAll(str("for"), j)}, Span: span},
			&ir.For{Init: []ir.Stmt{&ir.VarDecl{Name: "q", Type: ir.Number, Init: num(0), Mutable: true, Span: span}},
				Cond: cmp(ir.OpLess, ident("q", ir.Number), 1), Post: []ir.Stmt{do(cond(c, say("post"), say("never")))},
				Body: []ir.Stmt{assign("q", num(1))}, Span: span},
			do(cond(c, side("stmt then", 0), side("stmt else", 0))),
			do(cond(d, say("void then"), say("void else"))),
			do(cond(c, say("void then"), say("void else"))),
			logAll(callOf("pick", ir.String, boolLit(true)), callOf("pick", ir.String, boolLit(false))),
			decl("f", fn, cond(c, arrow(param("k", ir.Number), ir.Number, ret(binary(ir.OpMul, k, num(2), ir.Number))),
				arrow(param("k", ir.Number), ir.Number, ret(k)))),
			logNum(callOf("f", ir.Number, num(4))),
			decl("abs", fn, arrow(param("v", ir.Number), ir.Number,
				ret(cond(cmp(ir.OpLess, v, 0), &ir.Unary{Op: ir.OpNeg, Operand: v, Typ: ir.Number, Span: span}, v)))),
			logAll(callOf("abs", ir.Number, num(-3)), callOf("abs", ir.Number, num(2))),
			logNum(method(ir.ArrayMap, xs, nums, arrow(visit(ir.Number, "x", "", ""), ir.Number,
				ret(cond(cmp(ir.OpGreater, x, 1), binary(ir.OpMul, x, num(10), ir.Number), num(0)))))),
		}}
	want := "then\n1\nelse\n2\nB\n2\n{ x: 1, y: 4 }\n12\nAB\n1\n1\nif\n3\nfor 0\nfor 1\npost\nstmt then\nvoid else\nvoid then\nyes no\n8\n3 2\n[ 0, 20 ]\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestNullishMatchesNode: `??` on plain reads, which lowering tests and
// unwraps in place, and on calls, which a Let holds so they run once: count,
// read after each, has gone up by one. 0 isn't nullish.
//
//	let count: number = 0;
//	function next(v: number | undefined): number | undefined { count = count + 1; return v; }
//	const u: number | undefined = undefined; const z: number | null = null; const five: number | undefined = 5;
//	console.log(u ?? 1, z ?? 2, five ?? 3);
//	console.log(next(undefined) ?? 4, count);
//	console.log(next(6) ?? 7, count);
//	const zero: number | undefined = 0;
//	console.log(zero ?? 8);
//	const s: string | undefined = undefined;
//	const t: string = s ?? "default";
//	console.log(t);
//	const m: number | undefined = u ?? five;
//	console.log(m);
//	const chained: number = u ?? z ?? 9;
//	console.log(chained);
//	console.log(next(undefined) ?? next(10) ?? 11, count);
func TestNullishMatchesNode(t *testing.T) {
	t.Parallel()

	numM, strM := ir.MaybeOf(ir.Number), ir.MaybeOf(ir.String)
	count := ident("count", ir.Number)
	next := func(v ir.Expr) ir.Expr { return callOf("next", numM, v) }
	u, z, five := stable("u", numM), stable("z", numM), stable("five", numM)
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "next", Params: []ir.Param{{Name: "v", Type: numM, Span: span}}, Result: numM, Span: span,
			Body: []ir.Stmt{assign("count", binary(ir.OpAdd, count, num(1), ir.Number)), ret(stable("v", numM))}}},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "count", Type: ir.Number, Init: num(0), Mutable: true, Span: span},
			decl("u", numM, undef(ir.Number)), decl("z", numM, null(ir.Number)), decl("five", numM, some(num(5))),
			logAll(nullish(u, num(1)), nullish(z, num(2)), nullish(five, num(3))),
			logAll(heldNullish("ts held1", next(undef(ir.Number)), num(4)), count),
			logAll(heldNullish("ts held1", next(some(num(6))), num(7)), count),
			decl("zero", numM, some(num(0))),
			logNum(nullish(stable("zero", numM), num(8))),
			decl("s", strM, undef(ir.String)),
			decl("t", ir.String, nullish(stable("s", strM), str("default"))),
			logNum(stable("t", ir.String)),
			decl("m", numM, nullish(u, five)),
			logNum(stable("m", numM)),
			decl("chained", ir.Number, heldNullish("ts held1", nullish(u, z), num(9))),
			logNum(stable("chained", ir.Number)),
			logAll(heldNullish("ts held2", heldNullish("ts held1", next(undef(ir.Number)), next(some(num(10)))), num(11)), count),
		}}
	want := "1 2 5\n4 1\n6 2\n0\ndefault\n5\n9\n10 4\n"
	if got := buildAndRun(t, ir.ProgramOf(m)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestOptionalChainsMatchNode: `?.` through a field, an element and a
// method, of strings and arrays, from null and undefined heads, a call's
// held and run once; and `!` where it holds, as a value and as the receiver
// of reads, writes and for…of.
//
//	interface Inner { name: string; tags: string[] }
//	interface Outer { inner: Inner | undefined }
//	const o1: Outer = { inner: { name: " Ada ", tags: ["a", "b"] } };
//	const o2: Outer = { inner: undefined };
//	const n1: Inner | null = null;
//	function get(o: Outer): Inner | undefined { console.log("get"); return o.inner; }
//	console.log(o1.inner?.name); console.log(o2.inner?.name); console.log(n1?.name);
//	console.log(o1.inner?.tags[1]); console.log(o2.inner?.tags[0]);
//	console.log(o1.inner?.name.trim()); console.log(o2.inner?.name.trim());
//	console.log(get(o1)?.name, get(o2)?.name);
//	console.log(o1.inner?.tags.length); console.log(n1?.tags.length);
//	const nums: number[] | undefined = [5, 6]; const none: number[] | undefined = undefined;
//	console.log(nums?.[1], none?.[0]);
//	const m: number | undefined = 5;
//	console.log(m! + 1);
//	console.log(o1.inner!.name, o1.inner!.tags[0], o1.inner!.tags.length);
//	o1.inner!.name = "Bo";
//	console.log(o1.inner!.name.toUpperCase());
//	for (const t of o1.inner!.tags) { console.log(t); }
//	for (const v of nums!) { console.log(v); }
//	nums![0] = 7;
//	console.log(nums!.length, nums);
//	console.log(o1);
func TestOptionalChainsMatchNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	inner := shapes.Intern("Inner", []ir.Field{{Name: "name", Type: ir.String}, {Name: "tags", Type: ir.ArrayOf(ir.String)}})
	innerM := ir.MaybeOf(ir.ObjectOf(inner))
	outer := shapes.Intern("Outer", []ir.Field{{Name: "inner", Type: innerM}})
	numsM := ir.MaybeOf(ir.ArrayOf(ir.Number))
	o1, o2, n1 := stable("o1", ir.ObjectOf(outer)), stable("o2", ir.ObjectOf(outer)), stable("n1", innerM)
	nums, none := stable("nums", numsM), stable("none", numsM)
	name := func(x ir.Expr) ir.Expr { return get(x, "name") }
	tag := func(i float64) func(ir.Expr) ir.Expr {
		return func(x ir.Expr) ir.Expr { return elem(get(x, "tags"), num(i)) }
	}
	trimmed := func(x ir.Expr) ir.Expr { return strMethod(ir.StrTrim, name(x)) }
	length := func(x ir.Expr) ir.Expr { return method(ir.ArrayLength, x, ir.Number) }
	tags := func(x ir.Expr) ir.Expr { return length(get(x, "tags")) }
	at := func(i float64) func(ir.Expr) ir.Expr { return func(x ir.Expr) ir.Expr { return elem(x, num(i)) } }
	in1 := func() ir.Expr { return asserted(get(o1, "inner")) }
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "get", Params: []ir.Param{{Name: "o", Type: ir.ObjectOf(outer), Span: span}}, Result: innerM, Span: span,
			Body: []ir.Stmt{logStr("get"), ret(get(stable("o", ir.ObjectOf(outer)), "inner"))}}},
		Main: []ir.Stmt{
			decl("o1", ir.ObjectOf(outer), lit(outer, "inner", some(lit(inner, "name", str(" Ada "), "tags", arrayOf(ir.String, str("a"), str("b")))))),
			decl("o2", ir.ObjectOf(outer), lit(outer, "inner", undef(ir.ObjectOf(inner)))),
			decl("n1", innerM, null(ir.ObjectOf(inner))),
			logNum(optional("ts held1", get(o1, "inner"), name)),
			logNum(optional("ts held1", get(o2, "inner"), name)),
			logNum(optional("ts held1", n1, name)),
			logNum(optional("ts held1", get(o1, "inner"), tag(1))),
			logNum(optional("ts held1", get(o2, "inner"), tag(0))),
			logNum(optional("ts held1", get(o1, "inner"), trimmed)),
			logNum(optional("ts held1", get(o2, "inner"), trimmed)),
			logAll(optional("ts held1", callOf("get", innerM, o1), name), optional("ts held1", callOf("get", innerM, o2), name)),
			logNum(optional("ts held1", get(o1, "inner"), tags)),
			logNum(optional("ts held1", n1, tags)),
			decl("nums", numsM, some(arrayOf(ir.Number, num(5), num(6)))), decl("none", numsM, undef(ir.ArrayOf(ir.Number))),
			logAll(optional("ts held1", nums, at(1)), optional("ts held1", none, at(0))),
			decl("m", ir.MaybeOf(ir.Number), some(num(5))),
			logNum(binary(ir.OpAdd, asserted(stable("m", ir.MaybeOf(ir.Number))), num(1), ir.Number)),
			logAll(name(in1()), tag(0)(in1()), tags(in1())),
			set(in1(), "name", str("Bo")),
			logNum(strMethod(ir.StrToUpperCase, name(in1()))),
			&ir.ForOf{Name: "t", Type: ir.String, Array: get(in1(), "tags"), Body: []ir.Stmt{logNum(stable("t", ir.String))}, Span: span},
			&ir.ForOf{Name: "v", Type: ir.Number, Array: asserted(nums), Body: []ir.Stmt{logNum(stable("v", ir.Number))}, Span: span},
			&ir.IndexSet{Array: asserted(nums), Index: num(0), Value: num(7), Span: span},
			logAll(length(asserted(nums)), nums),
			logNum(o1),
		}}
	want := " Ada \nundefined\nundefined\nb\nundefined\nAda\nundefined\nget\nget\n Ada  undefined\n2\nundefined\n6 undefined\n6\n" +
		" Ada  a 2\nBO\na\nb\n5\n6\n2 [ 7, 6 ]\n{ inner: { name: 'Bo', tags: [ 'a', 'b' ] } }\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestUnwrapFailuresSayWhatNodeDoes: an unwrap of an empty value does what
// Node does (M5 plan D4). Where Node carries on, printing NaN for the first,
// it stops the program, exit 2, naming the value and whether the checker
// narrowed it or a ! asserted it. For each of the others it throws the
// TypeError Node throws, which, uncaught, ends the program as Node's does,
// exit 1 (M6 plan D9, checkFailure); Node names the key it reads, sets or
// iterates. The one difference is xs[i]: Node names the index, 3, and
// cakebear fails before evaluating it.
//
//	const m: number | undefined = undefined; console.log(m! + 1);
//	const box: Box = { p: { x: 1, y: 2 } };          // interface Box { p: Point | undefined }
//	function clear(): void { box.p = undefined; }
//	if (box.p !== undefined) { clear(); console.log(box.p.x); }
//	const s: string | null = null; console.log(s!.trim());
//	console.log(m!.toFixed(1));
//	function zero(): number { return 0; }
//	const p: Point | undefined = undefined; console.log(p!.x + zero());
//	const xs: number[] | undefined = undefined; console.log(xs![0]);
//	const i: number = 3; console.log(xs![i]);
//	const p: Point | null = null; p!.x = 5;
//	const ns: number[] | null = null; ns![0] = 1;
//	for (const v of xs!) { console.log(v); }
//	console.log(xs!.length); xs!.push(1); xs!.length = 0;
func TestUnwrapFailuresSayWhatNodeDoes(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	ptM := ir.MaybeOf(ir.ObjectOf(pt))
	box := shapes.Intern("Box", []ir.Field{{Name: "p", Type: ptM}})
	numM, strM, numsM := ir.MaybeOf(ir.Number), ir.MaybeOf(ir.String), ir.MaybeOf(ir.ArrayOf(ir.Number))
	b := stable("box", ir.ObjectOf(box))
	xs := stable("xs", numsM)
	undefXs := decl("xs", numsM, undef(ir.ArrayOf(ir.Number)))
	funcs := []*ir.Func{
		{Name: "clear", Result: ir.Void, Span: span, Body: []ir.Stmt{set(b, "p", undef(ir.ObjectOf(pt)))}},
		{Name: "zero", Result: ir.Number, Span: span, Body: []ir.Stmt{ret(num(0))}},
	}

	for _, tt := range []struct {
		main []ir.Stmt
		want string
	}{
		{[]ir.Stmt{decl("m", numM, undef(ir.Number)), logNum(binary(ir.OpAdd, asserted(stable("m", numM)), num(1), ir.Number))},
			"cakebear: m is undefined, though a ! asserted it isn't (Node carries on with undefined)"},
		{[]ir.Stmt{
			&ir.If{Cond: &ir.Unary{Op: ir.OpNot, Operand: isEmpty(get(b, "p"), true, false), Typ: ir.Boolean, Span: span}, Span: span,
				Then: []ir.Stmt{do(callOf("clear", ir.Void)), logNum(get(must(get(b, "p")), "x"))}},
		}, "TypeError: Cannot read properties of undefined (reading 'x')"},
		{[]ir.Stmt{decl("s", strM, null(ir.String)), logNum(strMethod(ir.StrTrim, asserted(stable("s", strM))))},
			"TypeError: Cannot read properties of null (reading 'trim')"},
		{[]ir.Stmt{decl("m", numM, undef(ir.Number)),
			logNum(&ir.BuiltinCall{Fn: ir.NumToFixed, Args: []ir.Expr{asserted(stable("m", numM)), num(1)}, Typ: ir.String, Span: span})},
			"TypeError: Cannot read properties of undefined (reading 'toFixed')"},
		// The read is ordered against the call beside it, through ts_read.
		{[]ir.Stmt{decl("p", ptM, undef(ir.ObjectOf(pt))), logNum(binary(ir.OpAdd, get(asserted(stable("p", ptM)), "x"), callOf("zero", ir.Number), ir.Number))},
			"TypeError: Cannot read properties of undefined (reading 'x')"},
		{[]ir.Stmt{undefXs, logNum(elem(asserted(xs), num(0)))},
			"TypeError: Cannot read properties of undefined (reading '0')"},
		{[]ir.Stmt{undefXs, decl("i", ir.Number, num(3)), logNum(elem(asserted(xs), stable("i", ir.Number)))},
			"TypeError: Cannot read properties of undefined (reading an index)"},
		{[]ir.Stmt{decl("p", ptM, null(ir.ObjectOf(pt))), set(asserted(stable("p", ptM)), "x", num(5))},
			"TypeError: Cannot set properties of null (setting 'x')"},
		{[]ir.Stmt{decl("ns", numsM, null(ir.ArrayOf(ir.Number))), &ir.IndexSet{Array: asserted(stable("ns", numsM)), Index: num(0), Value: num(1), Span: span}},
			"TypeError: Cannot set properties of null (setting '0')"},
		{[]ir.Stmt{undefXs, &ir.ForOf{Name: "v", Type: ir.Number, Array: asserted(xs), Body: []ir.Stmt{logNum(stable("v", ir.Number))}, Span: span}},
			"TypeError: xs is not iterable"},
		{[]ir.Stmt{undefXs, logNum(method(ir.ArrayLength, asserted(xs), ir.Number))},
			"TypeError: Cannot read properties of undefined (reading 'length')"},
		{[]ir.Stmt{undefXs, do(method(ir.ArrayPush, asserted(xs), ir.Number, num(1)))},
			"TypeError: Cannot read properties of undefined (reading 'push')"},
		{[]ir.Stmt{undefXs, do(method(ir.ArraySetLength, asserted(xs), ir.Void, num(0)))},
			"TypeError: Cannot set properties of undefined (setting 'length')"},
	} {
		// Every program has box and the functions, which only some use.
		boxed := decl("box", ir.ObjectOf(box), lit(box, "p", some(lit(pt, "x", num(1), "y", num(2)))))
		m := &ir.Module{Name: "t", Funcs: funcs, Main: append([]ir.Stmt{boxed}, tt.main...)}
		out := filepath.Join(t.TempDir(), "prog")
		if src, err := Build(withShapes(m, &shapes), Options{Output: out}); err != nil {
			t.Fatalf("Build: %v\n%s", err, src)
		}
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(out)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		checkFailure(t, err, stdout.String(), stderr.String(), tt.want)
	}
}

// TestCondEvaluationOrderMatchesNode: JavaScript's left-to-right order
// holds through a conditional and a Let, with calls in a branch, the test
// and the held value changing what is read beside them, to the left and to
// the right, and a field read too.
//
//	let a: number = 1; let c: boolean = true; let held: number | undefined = undefined;
//	const p: Point = { x: 1, y: 0 };
//	function f(): number { a = 10; return 5; }
//	function g(): number | undefined { a = 20; return held; }
//	function settle(): number { a = 2; return 1; }
//	function flip(): boolean { c = !c; return c; }
//	function bump(): number { p.x = p.x + 100; return 0; }
//	console.log(a + (c ? f() : 0));
//	a = 1; console.log((c ? a : 0) + f());
//	a = 1; console.log(f() + (c ? a : 0));
//	a = 1; console.log(a === settle() ? "old" : "new");
//	a = 1; console.log(a + (g() ?? 0));
//	a = 1; console.log((g() ?? a) + a);
//	a = 1; console.log(a + (g() ?? a));
//	a = 1; console.log((flip() ? a : f()) + a);
//	console.log(p.x + (c ? 0 : bump()) + p.x);
func TestCondEvaluationOrderMatchesNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	numM := ir.MaybeOf(ir.Number)
	a, c, p := ident("a", ir.Number), ident("c", ir.Boolean), stable("p", ir.ObjectOf(pt))
	sum := func(l, r ir.Expr) ir.Expr { return binary(ir.OpAdd, l, r, ir.Number) }
	f, g := callOf("f", ir.Number), callOf("g", numM)
	writes := func(name string, result ir.Type, body ...ir.Stmt) *ir.Func {
		return &ir.Func{Name: name, Result: result, Span: span, Body: body}
	}
	reset := assign("a", num(1))
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{
			writes("f", ir.Number, assign("a", num(10)), ret(num(5))),
			writes("g", numM, assign("a", num(20)), ret(ident("held", numM))),
			writes("settle", ir.Number, assign("a", num(2)), ret(num(1))),
			writes("flip", ir.Boolean, assign("c", &ir.Unary{Op: ir.OpNot, Operand: c, Typ: ir.Boolean, Span: span}), ret(c)),
			writes("bump", ir.Number, set(p, "x", sum(get(p, "x"), num(100))), ret(num(0))),
		},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "a", Type: ir.Number, Init: num(1), Mutable: true, Span: span},
			&ir.VarDecl{Name: "c", Type: ir.Boolean, Init: boolLit(true), Mutable: true, Span: span},
			&ir.VarDecl{Name: "held", Type: numM, Init: undef(ir.Number), Mutable: true, Span: span},
			decl("p", ir.ObjectOf(pt), lit(pt, "x", num(1), "y", num(0))),
			logNum(sum(a, cond(c, f, num(0)))),
			reset, logNum(sum(cond(c, a, num(0)), f)),
			reset, logNum(sum(f, cond(c, a, num(0)))),
			reset, logNum(cond(binary(ir.OpEqual, a, callOf("settle", ir.Number), ir.Boolean), str("old"), str("new"))),
			reset, logNum(sum(a, heldNullish("ts held1", g, num(0)))),
			reset, logNum(sum(heldNullish("ts held1", g, a), a)),
			reset, logNum(sum(a, heldNullish("ts held1", g, a))),
			reset, logNum(sum(cond(callOf("flip", ir.Boolean), a, f), a)),
			logNum(sum(sum(get(p, "x"), cond(c, num(0), callOf("bump", ir.Number))), get(p, "x"))),
		}}
	want := "6\n6\n15\nold\n1\n40\n21\n15\n102\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestCondAwaitsInABranch: an await in a conditional's branch, and in
// `??`'s default, in async functions and an async arrow, awaits in the
// coroutine around it, and only when that branch is the one chosen.
//
//	function delay(v: number): Promise<number> {
//	  return new Promise((resolve) => { setTimeout(() => { resolve(v); }, 10); });
//	}
//	async function choose(c: boolean): Promise<number> {
//	  const v: number = c ? await delay(1) : 2;
//	  console.log("chose", v);
//	  return c ? 10 + await delay(3) : await delay(4);
//	}
//	async function nullish(m: number | undefined): Promise<number> { return m ?? await delay(7); }
//	console.log(await choose(true), await choose(false));
//	console.log(await nullish(undefined), await nullish(8));
//	const later = async (c: boolean): Promise<number> => c ? await delay(5) : 6;
//	console.log(await later(true), await later(false));
func TestCondAwaitsInABranch(t *testing.T) {
	t.Parallel()

	numP, numM := ir.PromiseOf(ir.Number), ir.MaybeOf(ir.Number)
	resolve := ir.ResolveType(ir.Number)
	awaited := func(x ir.Expr) ir.Expr { return &ir.Await{Value: x, Typ: ir.Number, Span: span} }
	delay := func(v float64) ir.Expr { return awaited(callOf("delay", numP, num(v))) }
	c := stable("c", ir.Boolean)
	param := func(name string, t ir.Type) []ir.Param { return []ir.Param{{Name: name, Type: t, Span: span}} }
	timer := &ir.HostCall{Op: ir.HostSetTimeout, Typ: ir.Number, Span: span, Args: []ir.Expr{
		arrow(nil, ir.Void, do(callOf("resolve", ir.Void, stable("v", ir.Number)))), num(10),
	}}
	executor := &ir.FuncLit{Params: param("resolve", resolve), Result: ir.Void, Typ: ir.ExecutorType(ir.Number), Span: span, Body: []ir.Stmt{do(timer)}}
	later := &ir.FuncLit{Params: param("c", ir.Boolean), Result: numP, Async: true, Typ: ir.FuncType([]ir.Type{ir.Boolean}, numP), Span: span,
		Body: []ir.Stmt{ret(cond(c, delay(5), num(6)))}}
	m := &ir.Module{Name: "t", File: "/src/t.ts", Async: true,
		Funcs: []*ir.Func{
			{Name: "delay", Params: param("v", ir.Number), Result: numP, Span: span,
				Body: []ir.Stmt{ret(&ir.NewPromise{Executor: executor, Typ: numP, Span: span})}},
			{Name: "choose", Params: param("c", ir.Boolean), Result: numP, Async: true, Span: span, Body: []ir.Stmt{
				decl("v", ir.Number, cond(c, delay(1), num(2))),
				logAll(str("chose"), stable("v", ir.Number)),
				ret(cond(c, binary(ir.OpAdd, num(10), delay(3), ir.Number), delay(4))),
			}},
			{Name: "nullish", Params: param("m", numM), Result: numP, Async: true, Span: span,
				Body: []ir.Stmt{ret(nullish(stable("m", numM), delay(7)))}},
		},
		Main: []ir.Stmt{
			logAll(awaited(callOf("choose", numP, boolLit(true))), awaited(callOf("choose", numP, boolLit(false)))),
			logAll(awaited(callOf("nullish", numP, undef(ir.Number))), awaited(callOf("nullish", numP, some(num(8))))),
			decl("later", later.Typ, later),
			logAll(awaited(callOf("later", numP, boolLit(true))), awaited(callOf("later", numP, boolLit(false)))),
		}}
	want := "chose 1\nchose 2\n13 4\n7 8\n5 6\n"
	if got := buildAndRun(t, ir.ProgramOf(m)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}
