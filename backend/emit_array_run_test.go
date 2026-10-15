package backend

import (
	"errors"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// The array methods of M4 step 7, built from IR by hand. What each program
// prints is what Node 24.18 printed for the TypeScript in its comment.

func arrayOf(elem ir.Type, elems ...ir.Expr) *ir.ArrayLit {
	return &ir.ArrayLit{Elems: elems, Typ: ir.ArrayOf(elem), Span: span}
}

// method is xs.m(args…), of type t.
func method(m ir.ArrayMethod, xs ir.Expr, t ir.Type, args ...ir.Expr) *ir.ArrayOp {
	return &ir.ArrayOp{Method: m, Array: xs, Args: args, Typ: t, Span: span}
}

// arrow is a function literal taking params, typed by them.
func arrow(params []ir.Param, result ir.Type, body ...ir.Stmt) *ir.FuncLit {
	types := make([]ir.Type, len(params))
	for i, p := range params {
		types[i] = p.Type
	}
	return &ir.FuncLit{Params: params, Result: result, Body: body, Typ: ir.FuncType(types, result), Span: span}
}

// visit is the parameters of a callback over an array of elem, (x, i, a),
// named as given, "" for one the callback ignores.
func visit(elem ir.Type, x, i, a string) []ir.Param {
	return []ir.Param{{Name: x, Type: elem}, {Name: i, Type: ir.Number}, {Name: a, Type: ir.ArrayOf(elem)}}
}

func decl(name string, t ir.Type, init ir.Expr) *ir.VarDecl {
	return &ir.VarDecl{Name: name, Type: t, Init: init, Span: span}
}

func do(x ir.Expr) ir.Stmt { return &ir.ExprStmt{Expr: x, Span: span} }

func ret(x ir.Expr) ir.Stmt { return &ir.Return{Value: x, Span: span} }

// TestArrayMethodsMatchNode: each method, with its result printed.
//
//	const xs: number[] = [3, 1, 2];
//	console.log(xs.map((x: number, i: number): string => [x, i].join(":")));
//	console.log(xs.filter((x: number): boolean => x > 1));
//	console.log(xs.join(), xs.join(" + "));
//	console.log(xs.includes(2), xs.indexOf(2), xs.indexOf(5));
//	console.log(xs.slice(1), xs.slice(-2, -1), xs.slice());
//	console.log(xs.some((x: number): boolean => x > 2), xs.every((x: number): boolean => x > 2));
//	xs.forEach((x: number, i: number, a: number[]): void => { console.log(x, i, a === xs); });
//	console.log(xs.reduce((acc: string, x: number): string => acc + [x].join(""), ">"));
//	console.log(xs.findIndex((x: number): boolean => x === 2), xs.findIndex((x: number): boolean => x === 7));
//	const s: number[] = xs.sort((a: number, b: number): number => a - b);
//	console.log(s === xs, xs);
func TestArrayMethodsMatchNode(t *testing.T) {
	t.Parallel()

	nums := ir.ArrayOf(ir.Number)
	xs, s := stable("xs", nums), stable("s", nums)
	x, i, arr := stable("x", ir.Number), stable("i", ir.Number), stable("a", nums)
	a, b, acc := stable("a", ir.Number), stable("b", ir.Number), stable("acc", ir.String)
	test := func(op ir.BinaryOp, n float64) *ir.FuncLit {
		return arrow(visit(ir.Number, "x", "", ""), ir.Boolean, ret(binary(op, x, num(n), ir.Boolean)))
	}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("xs", nums, arrayOf(ir.Number, num(3), num(1), num(2))),
		logNum(method(ir.ArrayMap, xs, ir.ArrayOf(ir.String), arrow(visit(ir.Number, "x", "i", ""), ir.String,
			ret(method(ir.ArrayJoin, arrayOf(ir.Number, x, i), ir.String, str(":")))))),
		logNum(method(ir.ArrayFilter, xs, nums, test(ir.OpGreater, 1))),
		logAll(method(ir.ArrayJoin, xs, ir.String), method(ir.ArrayJoin, xs, ir.String, str(" + "))),
		logAll(method(ir.ArrayIncludes, xs, ir.Boolean, num(2)), method(ir.ArrayIndexOf, xs, ir.Number, num(2)),
			method(ir.ArrayIndexOf, xs, ir.Number, num(5))),
		logAll(method(ir.ArraySlice, xs, nums, num(1)), method(ir.ArraySlice, xs, nums, num(-2), num(-1)), method(ir.ArraySlice, xs, nums)),
		logAll(method(ir.ArraySome, xs, ir.Boolean, test(ir.OpGreater, 2)), method(ir.ArrayEvery, xs, ir.Boolean, test(ir.OpGreater, 2))),
		do(method(ir.ArrayForEach, xs, ir.Void, arrow(visit(ir.Number, "x", "i", "a"), ir.Void,
			logAll(x, i, binary(ir.OpEqual, arr, xs, ir.Boolean))))),
		logNum(method(ir.ArrayReduce, xs, ir.String,
			arrow([]ir.Param{{Name: "acc", Type: ir.String}, {Name: "x", Type: ir.Number}, {Type: ir.Number}, {Type: nums}}, ir.String,
				ret(binary(ir.OpAdd, acc, method(ir.ArrayJoin, arrayOf(ir.Number, x), ir.String, str("")), ir.String))),
			str(">"))),
		logAll(method(ir.ArrayFindIndex, xs, ir.Number, test(ir.OpEqual, 2)), method(ir.ArrayFindIndex, xs, ir.Number, test(ir.OpEqual, 7))),
		decl("s", nums, method(ir.ArraySort, xs, nums,
			arrow([]ir.Param{{Name: "a", Type: ir.Number}, {Name: "b", Type: ir.Number}}, ir.Number,
				ret(binary(ir.OpSub, a, b, ir.Number))))),
		logAll(binary(ir.OpEqual, s, xs, ir.Boolean), xs),
	}}
	want := "[ '3:0', '1:1', '2:2' ]\n[ 3, 2 ]\n3,1,2 3 + 1 + 2\ntrue 2 -1\n[ 1, 2 ] [ 1 ] [ 3, 1, 2 ]\ntrue false\n" +
		"3 0 true\n1 1 true\n2 2 true\n>312\n2 -1\ntrue [ 1, 2, 3 ]\n"
	if got := buildAndRun(t, ir.ProgramOf(m)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestArrayMethodEdgesMatchNode: NaN and -0 in includes and indexOf, which
// differ only for NaN; join of -0, NaN, nested arrays, booleans and
// objects; and slice's bounds, fractional, NaN, infinite and out of range.
//
//	const zero: number = 0;
//	const nan: number = zero / zero;
//	const inf: number = 1 / zero;
//	const ns: number[] = [1, nan, -0, 3];
//	console.log(ns.includes(nan), ns.indexOf(nan), ns.includes(0), ns.indexOf(0));
//	const zs: number[] = [0];
//	console.log(zs.includes(-0), zs.indexOf(-0));
//	console.log(ns.join(), [[1, 2], [3, 4], []].join(";"), [true, false].join("|"));
//	const p: Point = { x: 1, y: 2 };
//	const pts: Point[] = [p, { x: 3, y: 4 }];
//	console.log(pts.join("+"), pts.includes(p), pts.indexOf({ x: 1, y: 2 }), pts.indexOf(p));
//	const s5: number[] = [0, 1, 2, 3, 4];
//	console.log(s5.slice(-0.5), s5.slice(1.9), s5.slice(-1.9), s5.slice(nan, 2));
//	console.log(s5.slice(10), s5.slice(-10), s5.slice(inf), s5.slice(0, -inf), s5.slice(3, 1));
func TestArrayMethodEdgesMatchNode(t *testing.T) {
	t.Parallel()

	nums := ir.ArrayOf(ir.Number)
	var shapes ir.Shapes
	pt := point(&shapes)
	pts := ir.ArrayOf(ir.ObjectOf(pt))
	zero, nan, inf := stable("zero", ir.Number), stable("nan", ir.Number), stable("inf", ir.Number)
	ns, zs, s5, p, ps := stable("ns", nums), stable("zs", nums), stable("s5", nums), stable("p", ir.ObjectOf(pt)), stable("pts", pts)
	negZero := num(math.Copysign(0, -1))
	slice := func(bounds ...ir.Expr) ir.Expr { return method(ir.ArraySlice, s5, nums, bounds...) }
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("zero", ir.Number, num(0)),
		decl("nan", ir.Number, binary(ir.OpDiv, zero, zero, ir.Number)),
		decl("inf", ir.Number, binary(ir.OpDiv, num(1), zero, ir.Number)),
		decl("ns", nums, arrayOf(ir.Number, num(1), nan, negZero, num(3))),
		logAll(method(ir.ArrayIncludes, ns, ir.Boolean, nan), method(ir.ArrayIndexOf, ns, ir.Number, nan),
			method(ir.ArrayIncludes, ns, ir.Boolean, num(0)), method(ir.ArrayIndexOf, ns, ir.Number, num(0))),
		decl("zs", nums, arrayOf(ir.Number, num(0))),
		logAll(method(ir.ArrayIncludes, zs, ir.Boolean, negZero), method(ir.ArrayIndexOf, zs, ir.Number, negZero)),
		logAll(method(ir.ArrayJoin, ns, ir.String),
			method(ir.ArrayJoin, arrayOf(nums, arrayOf(ir.Number, num(1), num(2)), arrayOf(ir.Number, num(3), num(4)), arrayOf(ir.Number)), ir.String, str(";")),
			method(ir.ArrayJoin, arrayOf(ir.Boolean, &ir.BoolLit{Value: true, Span: span}, &ir.BoolLit{Span: span}), ir.String, str("|"))),
		decl("p", ir.ObjectOf(pt), lit(pt, "x", num(1), "y", num(2))),
		decl("pts", pts, arrayOf(ir.ObjectOf(pt), p, lit(pt, "x", num(3), "y", num(4)))),
		logAll(method(ir.ArrayJoin, ps, ir.String, str("+")), method(ir.ArrayIncludes, ps, ir.Boolean, p),
			method(ir.ArrayIndexOf, ps, ir.Number, lit(pt, "x", num(1), "y", num(2))), method(ir.ArrayIndexOf, ps, ir.Number, p)),
		decl("s5", nums, arrayOf(ir.Number, num(0), num(1), num(2), num(3), num(4))),
		logAll(slice(num(-0.5)), slice(num(1.9)), slice(num(-1.9)), slice(nan, num(2))),
		logAll(slice(num(10)), slice(num(-10)), slice(inf), slice(num(0), &ir.Unary{Op: ir.OpNeg, Operand: inf, Typ: ir.Number, Span: span}),
			slice(num(3), num(1))),
	}}
	want := "true -1 true 2\ntrue 0\n1,NaN,0,3 1,2;3,4; true|false\n[object Object]+[object Object] true -1 0\n" +
		"[ 0, 1, 2, 3, 4 ] [ 1, 2, 3, 4 ] [ 4 ] [ 0, 1 ]\n[] [ 0, 1, 2, 3, 4 ] [] [] []\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestArrayMutationAndOrderMatchNode: a push during forEach isn't visited, a
// replacement during map is seen, sort is stable with a comparator giving
// NaN for equal keys, and the array and each argument are evaluated once,
// in source order.
//
//	const fe: number[] = [1, 2, 3];
//	fe.forEach((x: number, i: number, a: number[]): void => { console.log(x); if (i === 0) { a.push(99); } });
//	console.log(fe);
//	const mp: number[] = [1, 2, 3];
//	console.log(mp.map((x: number, i: number, a: number[]): number => { if (i === 0) { a[2] = 30; } return x * 10; }));
//	const zero: number = 0;
//	const nan: number = zero / zero;
//	const people: KN[] = [{ n: "a", k: 2 }, { n: "b", k: 1 }, … { n: "g", k: 1 }];
//	people.sort((p: KN, q: KN): number => { if (p.k === q.k) { return nan; } return p.k - q.k; });
//	console.log(people.map((p: KN): string => p.n).join(""));
//	console.log([3, 1, 2].sort((a: number, b: number): number => nan));
//	let ys: number[] = [1, 2];
//	function swap(): number { ys = [5]; return 5; }
//	console.log(ys.includes(swap()), ys.includes(5));
//	let ws: number[] = [1, 2];
//	function swapAll(): Test { ws = [5]; return (x, i, a) => x === 5; }
//	console.log(ws.some(swapAll()), ws.some(swapAll()));
//	function make(): number[] { console.log("make"); return [1, 2, 3]; }
//	console.log(make().filter((x: number): boolean => x !== 2));
//	function pick(): Fold { console.log("pick"); return (acc, x, i, a) => acc + x; }
//	function seed(): number { console.log("seed"); return 100; }
//	console.log(make().reduce(pick(), seed()));
func TestArrayMutationAndOrderMatchNode(t *testing.T) {
	t.Parallel()

	nums := ir.ArrayOf(ir.Number)
	var shapes ir.Shapes
	kn := shapes.Intern("KN", []ir.Field{{Name: "k", Type: ir.Number}, {Name: "n", Type: ir.String}})
	person := ir.ObjectOf(kn)
	x, i, a := stable("x", ir.Number), stable("i", ir.Number), stable("a", nums)
	nan, ys := stable("nan", ir.Number), ident("ys", nums)
	pp, q := stable("p", person), stable("q", person)
	atFirst := func(then ir.Stmt) *ir.If {
		return &ir.If{Cond: binary(ir.OpEqual, i, num(0), ir.Boolean), Then: []ir.Stmt{then}, Span: span}
	}
	pushFirst := atFirst(do(method(ir.ArrayPush, a, ir.Number, num(99))))
	writeFirst := atFirst(&ir.IndexSet{Array: a, Index: num(2), Value: num(30), Span: span})
	sameKey := &ir.If{Cond: binary(ir.OpEqual, get(pp, "k"), get(q, "k"), ir.Boolean), Then: []ir.Stmt{ret(nan)}, Span: span}
	people := arrayOf(person)
	for j, n := range "abcdefg" {
		people.Elems = append(people.Elems, lit(kn, "n", str(string(n)), "k", num([]float64{2, 1, 2, 1, 0, 2, 1}[j])))
	}
	fold := ir.FuncType([]ir.Type{ir.Number, ir.Number, ir.Number, nums}, ir.Number)
	five := arrow(visit(ir.Number, "x", "", ""), ir.Boolean, ret(binary(ir.OpEqual, x, num(5), ir.Boolean)))
	call := func(name string, t ir.Type) *ir.Call { return &ir.Call{Callee: name, Typ: t, Span: span} }
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{
			{Name: "swap", Result: ir.Number, Span: span, Body: []ir.Stmt{
				&ir.Assign{Name: "ys", Value: arrayOf(ir.Number, num(5)), Span: span}, ret(num(5))}},
			{Name: "make", Result: nums, Span: span, Body: []ir.Stmt{logStr("make"), ret(arrayOf(ir.Number, num(1), num(2), num(3)))}},
			{Name: "pick", Result: fold, Span: span, Body: []ir.Stmt{logStr("pick"),
				ret(arrow([]ir.Param{{Name: "acc", Type: ir.Number}, {Name: "x", Type: ir.Number}, {Type: ir.Number}, {Type: nums}}, ir.Number,
					ret(binary(ir.OpAdd, stable("acc", ir.Number), x, ir.Number))))}},
			{Name: "seed", Result: ir.Number, Span: span, Body: []ir.Stmt{logStr("seed"), ret(num(100))}},
			{Name: "swapAll", Result: five.Typ, Span: span, Body: []ir.Stmt{
				&ir.Assign{Name: "ws", Value: arrayOf(ir.Number, num(5)), Span: span}, ret(five)}},
		},
		Main: []ir.Stmt{
			decl("fe", nums, arrayOf(ir.Number, num(1), num(2), num(3))),
			do(method(ir.ArrayForEach, stable("fe", nums), ir.Void, arrow(visit(ir.Number, "x", "i", "a"), ir.Void, logNum(x), pushFirst))),
			logNum(stable("fe", nums)),
			decl("mp", nums, arrayOf(ir.Number, num(1), num(2), num(3))),
			logNum(method(ir.ArrayMap, stable("mp", nums), nums, arrow(visit(ir.Number, "x", "i", "a"), ir.Number,
				writeFirst, ret(binary(ir.OpMul, x, num(10), ir.Number))))),
			decl("zero", ir.Number, num(0)),
			decl("nan", ir.Number, binary(ir.OpDiv, stable("zero", ir.Number), stable("zero", ir.Number), ir.Number)),
			decl("people", ir.ArrayOf(person), people),
			do(method(ir.ArraySort, stable("people", ir.ArrayOf(person)), ir.ArrayOf(person),
				arrow([]ir.Param{{Name: "p", Type: person}, {Name: "q", Type: person}}, ir.Number,
					sameKey, ret(binary(ir.OpSub, get(pp, "k"), get(q, "k"), ir.Number))))),
			logNum(method(ir.ArrayJoin, method(ir.ArrayMap, stable("people", ir.ArrayOf(person)), ir.ArrayOf(ir.String),
				arrow(visit(person, "p", "", ""), ir.String, ret(get(pp, "n")))), ir.String, str(""))),
			logNum(method(ir.ArraySort, arrayOf(ir.Number, num(3), num(1), num(2)), nums,
				arrow([]ir.Param{{Type: ir.Number}, {Type: ir.Number}}, ir.Number, ret(nan)))),
			&ir.VarDecl{Name: "ys", Type: nums, Init: arrayOf(ir.Number, num(1), num(2)), Mutable: true, Span: span},
			logAll(method(ir.ArrayIncludes, ys, ir.Boolean, call("swap", ir.Number)), method(ir.ArrayIncludes, ys, ir.Boolean, num(5))),
			&ir.VarDecl{Name: "ws", Type: nums, Init: arrayOf(ir.Number, num(1), num(2)), Mutable: true, Span: span},
			logAll(method(ir.ArraySome, ident("ws", nums), ir.Boolean, call("swapAll", five.Typ)),
				method(ir.ArraySome, ident("ws", nums), ir.Boolean, call("swapAll", five.Typ))),
			logNum(method(ir.ArrayFilter, call("make", nums), nums,
				arrow(visit(ir.Number, "x", "", ""), ir.Boolean, ret(binary(ir.OpNotEqual, x, num(2), ir.Boolean))))),
			logNum(method(ir.ArrayReduce, call("make", nums), ir.Number, call("pick", fold), call("seed", ir.Number))),
		}}
	want := "1\n2\n3\n[ 1, 2, 3, 99 ]\n[ 10, 20, 300 ]\nebdgacf\n[ 3, 1, 2 ]\nfalse true\nfalse true\nmake\n[ 1, 3 ]\nmake\npick\nseed\n106\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestArrayCallbacksRunInTheCallersDomain: a callback runs in the domain of
// the code calling the method, so a timer it sets fires there.
//
//	const xs: number[] = [1, 2, 3];
//	xs.forEach((x: number): void => { setTimeout((): void => { console.log("later", x); }, 0); });
//	console.log("now", xs.map((x: number): number => setTimeout((): void => { console.log("mapped", x); }, 0) * 0));
func TestArrayCallbacksRunInTheCallersDomain(t *testing.T) {
	t.Parallel()

	nums := ir.ArrayOf(ir.Number)
	x, xs := stable("x", ir.Number), stable("xs", nums)
	later := func(label string) *ir.HostCall {
		return &ir.HostCall{Op: ir.HostSetTimeout, Typ: ir.Number, Span: span, Args: []ir.Expr{
			arrow(nil, ir.Void, logAll(str(label), x)), num(0)}}
	}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("xs", nums, arrayOf(ir.Number, num(1), num(2), num(3))),
		do(method(ir.ArrayForEach, xs, ir.Void, arrow(visit(ir.Number, "x", "", ""), ir.Void, do(later("later"))))),
		logAll(str("now"), method(ir.ArrayMap, xs, nums, arrow(visit(ir.Number, "x", "", ""), ir.Number,
			ret(binary(ir.OpMul, later("mapped"), num(0), ir.Number))))),
	}}
	want := "now [ 0, 0, 0 ]\nlater 1\nlater 2\nlater 3\nmapped 1\nmapped 2\nmapped 3\n"
	if got := buildAndRun(t, ir.ProgramOf(m)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestArrayShrinkingInForEachPanics (D9): where Node skips the elements a
// callback removed, cakebear panics, saying so, and the program exits 2, as
// a Go panic does. Node prints 1 and 2, then `done [ 1, 2 ]`.
//
//	const xs: number[] = [1, 2, 3, 4];
//	xs.forEach((x: number, i: number, a: number[]): void => { console.log(x); if (i === 1) { a.length = 2; } });
func TestArrayShrinkingInForEachPanics(t *testing.T) {
	t.Parallel()

	nums := ir.ArrayOf(ir.Number)
	x, i, a := stable("x", ir.Number), stable("i", ir.Number), stable("a", nums)
	cut := &ir.If{Cond: binary(ir.OpEqual, i, num(1), ir.Boolean), Span: span,
		Then: []ir.Stmt{do(method(ir.ArraySetLength, a, ir.Void, num(2)))}}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("xs", nums, arrayOf(ir.Number, num(1), num(2), num(3), num(4))),
		do(method(ir.ArrayForEach, stable("xs", nums), ir.Void, arrow(visit(ir.Number, "x", "i", "a"), ir.Void, logNum(x), cut))),
		logStr("done"),
	}}
	out := filepath.Join(t.TempDir(), "prog")
	if src, err := Build(ir.ProgramOf(m), Options{Output: out}); err != nil {
		t.Fatalf("Build: %v\n%s", err, src)
	}
	var stderr strings.Builder
	cmd := exec.Command(out)
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	var exit *exec.ExitError
	const want = "cakebear: the array shrank to 2 elements while forEach ran over 4 (Node skips the rest)"
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(stderr.String(), want) {
		t.Errorf("%v, stderr\n%s\nwant exit 2 and %q", err, stderr.String(), want)
	}
	if string(stdout) != "1\n2\n" {
		t.Errorf("the program printed %q before the panic, want %q", stdout, "1\n2\n")
	}
}
