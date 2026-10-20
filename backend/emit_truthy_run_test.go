package backend

import (
	"math"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// Truthiness (M5 step 7), built from IR by hand in the shapes lowering
// builds it in. What each program prints is what Node 24.18 printed for the
// TypeScript in its comment, the same in three runs, with i32 = x => x | 0,
// u32 = x => x >>> 0, f32 = Math.fround, and i64, u64 and base64 the
// identity.
//
// tsc refuses a few of the conditions: a function tested by name in an if
// or a ternary (TS2774), and a literal that is always falsy or always
// truthy, null, undefined and `"x" as string | undefined` (TS2873, TS2872).
// Lowering won't meet those, but Node runs them, and the backend emits them
// as it emits the rest.

// truthyOf is ToBoolean of v, and not is `!x`.
func truthyOf(v ir.Expr) *ir.Truthy { return &ir.Truthy{Value: v, Span: span} }
func not(x ir.Expr) *ir.Unary {
	return &ir.Unary{Op: ir.OpNot, Operand: x, Typ: ir.Boolean, Span: span}
}

// tested is v as a condition in each place lowering makes a Truthy, with
// the results printed on one line after tag: an if with an else, a while
// left by a break, unless loop is false, a `!` and a ternary's test.
//
//	if (v) { a = true; } else { a = false; }
//	w = false; while (v) { w = true; break; }
//	console.log("tag", a, w, !v, v ? "yes" : "no");
func tested(tag string, v ir.Expr, loop bool) []ir.Stmt {
	a, w := ident("a", ir.Boolean), ident("w", ir.Boolean)
	stmts := []ir.Stmt{&ir.If{Cond: truthyOf(v), Span: span,
		Then: []ir.Stmt{assign("a", boolLit(true))}, Else: []ir.Stmt{assign("a", boolLit(false))}}}
	results := []ir.Expr{str(tag), a, w, not(truthyOf(v)), cond(truthyOf(v), str("yes"), str("no"))}
	if loop {
		stmts = append(stmts, assign("w", boolLit(false)),
			&ir.While{Cond: truthyOf(v), Body: []ir.Stmt{assign("w", boolLit(true)), &ir.Break{Span: span}}, Span: span})
	} else {
		results = append(results[:2], results[3:]...)
	}
	return append(stmts, logAll(results...))
}

// truthyProgram declares each value as a const of its own type, then tests
// it, by name, as tested does.
func truthyProgram(shapes *ir.Shapes, values []namedValue, extra ...ir.Stmt) *ir.Program {
	main := []ir.Stmt{
		&ir.VarDecl{Name: "a", Type: ir.Boolean, Init: boolLit(false), Mutable: true, Span: span},
		&ir.VarDecl{Name: "w", Type: ir.Boolean, Init: boolLit(false), Mutable: true, Span: span},
	}
	for _, v := range values {
		main = append(main, decl(v.name, v.init.ExprType(), v.init))
	}
	for _, v := range values {
		main = append(main, tested(v.name, stable(v.name, v.init.ExprType()), true)...)
	}
	return withShapes(&ir.Module{Name: "t", Main: append(main, extra...)}, shapes)
}

type namedValue struct {
	name string
	init ir.Expr
}

func conv(t ir.Type, v ir.Expr) ir.Expr { return &ir.Convert{Value: v, Typ: t, Span: span} }
func div(l, r float64) ir.Expr          { return binary(ir.OpDiv, num(l), num(r), ir.Number) }

var negZero = num(math.Copysign(0, -1))

// TestTruthinessMatchesNode: every kind as a condition, in an if, a while,
// under `!` and as a ternary's test. A number is falsy for 0, -0 and NaN, a
// string only for "", an integer for 0, an f32 as a number, a boolean is
// itself, and an object, an array and a function are truthy, whatever they
// hold. null and undefined are falsy.
//
//	interface Point { x: number; y: number }
//	let a: boolean = false; let w: boolean = false;
//	const zero: number = 0; const negZero: number = -0; const nan: number = 0 / 0; const one: number = 1;
//	const minusOne: number = -1; const inf: number = 1 / 0; const tiny: number = 5e-324;
//	const empty: string = ""; const zeroStr: string = "0"; const space: string = " "; const falseStr: string = "false";
//	const yes: boolean = true; const no: boolean = false;
//	const i0: i32 = i32(0); const iNeg: i32 = i32(-1); const l0: i64 = i64(0); const l1: i64 = i64(1);
//	const u0: u32 = u32(0); const uMax: u32 = u32(4294967295); const ul0: u64 = u64(0); const ul1: u64 = u64(1);
//	const f0: f32 = f32(0); const fNeg0: f32 = f32(-0); const fNaN: f32 = f32(0 / 0); const fHalf: f32 = f32(0.5);
//	const b0: base64 = base64(""); const b1: base64 = base64("aGk=");
//	const o: Point = { x: 0, y: 0 }; const xs: number[] = [];
//	const f = (): number => 0;
//	// then each of them, in that order, as tested shows
//	if (0) { a = true; } else { a = false; }
//	console.log("0", a, !0, 0 ? "yes" : "no");
//	// and the same for 1, null and undefined
func TestTruthinessMatchesNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	values := []namedValue{
		{"zero", num(0)}, {"negZero", negZero}, {"nan", div(0, 0)}, {"one", num(1)},
		{"minusOne", num(-1)}, {"inf", div(1, 0)}, {"tiny", num(5e-324)},
		{"empty", str("")}, {"zeroStr", str("0")}, {"space", str(" ")}, {"falseStr", str("false")},
		{"yes", boolLit(true)}, {"no", boolLit(false)},
		{"i0", conv(ir.Int32, num(0))}, {"iNeg", conv(ir.Int32, num(-1))}, {"l0", conv(ir.Int64, num(0))}, {"l1", conv(ir.Int64, num(1))},
		{"u0", conv(ir.Uint32, num(0))}, {"uMax", conv(ir.Uint32, num(4294967295))}, {"ul0", conv(ir.Uint64, num(0))}, {"ul1", conv(ir.Uint64, num(1))},
		{"f0", conv(ir.Float32, num(0))}, {"fNeg0", conv(ir.Float32, negZero)}, {"fNaN", conv(ir.Float32, div(0, 0))}, {"fHalf", conv(ir.Float32, num(0.5))},
		{"b0", conv(ir.Base64, str(""))}, {"b1", conv(ir.Base64, str("aGk="))},
		{"o", lit(pt, "x", num(0), "y", num(0))}, {"xs", arrayOf(ir.Number)},
		{"f", arrow(nil, ir.Number, ret(num(0)))},
	}
	var literals []ir.Stmt
	for _, l := range []namedValue{{"0", num(0)}, {"1", num(1)}, {"null", &ir.NullLit{Span: span}}, {"undefined", &ir.UndefinedLit{Span: span}}} {
		literals = append(literals, tested(l.name, l.init, false)...)
	}
	want := "zero false false true no\nnegZero false false true no\nnan false false true no\none true true false yes\n" +
		"minusOne true true false yes\ninf true true false yes\ntiny true true false yes\n" +
		"empty false false true no\nzeroStr true true false yes\nspace true true false yes\nfalseStr true true false yes\n" +
		"yes true true false yes\nno false false true no\n" +
		"i0 false false true no\niNeg true true false yes\nl0 false false true no\nl1 true true false yes\n" +
		"u0 false false true no\nuMax true true false yes\nul0 false false true no\nul1 true true false yes\n" +
		"f0 false false true no\nfNeg0 false false true no\nfNaN false false true no\nfHalf true true false yes\n" +
		"b0 false false true no\nb1 true true false yes\n" +
		"o true true false yes\nxs true true false yes\nf true true false yes\n" +
		"0 false true no\n1 true false yes\nnull false true no\nundefined false true no\n"
	if got := buildAndRun(t, truthyProgram(&shapes, values, literals...)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestMaybeTruthinessMatchesNode: a maybe of each kind is falsy when it is
// undefined or null, and otherwise as its value is, so a maybe holding 0,
// -0, NaN, "" or false is falsy too. Then an undefined, a null and two
// values in a maybe's slot, tested where they stand.
//
//	const n0: number | undefined = 0; const nNeg0: number | null = -0; const nNaN: number | undefined = 0 / 0;
//	const n5: number | undefined = 5; const nU: number | undefined = undefined; const nN: number | null = null;
//	const s0: string | undefined = ""; const sX: string | null = "x"; const sU: string | undefined = undefined; const sN: string | null = null;
//	const bF: boolean | undefined = false; const bT: boolean | null = true; const bU: boolean | undefined = undefined; const bN: boolean | null = null;
//	const i0: i32 | undefined = i32(0); const i7: i32 | null = i32(7); const iU: i32 | undefined = undefined;
//	const l0: i64 | undefined = i64(0); const u0: u32 | null = u32(0); const ul1: u64 | undefined = u64(1);
//	const fNaN: f32 | undefined = f32(0 / 0); const fHalf: f32 | null = f32(0.5); const fN: f32 | null = null;
//	const e0: base64 | undefined = base64(""); const e1: base64 | undefined = base64("aGk=");
//	const p: Point | undefined = { x: 0, y: 0 }; const pU: Point | undefined = undefined; const pN: Point | null = null;
//	const xs: number[] | undefined = []; const xsU: number[] | undefined = undefined;
//	const g: (() => number) | undefined = (): number => 0; const gN: (() => number) | null = null;
//	// then each of them, in that order, as tested shows
//	if (undefined as number | undefined) { a = true; } else { a = false; }
//	console.log("undefined", a, !(undefined as number | undefined), (undefined as number | undefined) ? "yes" : "no");
//	// and the same for null as string | null, 0 as number | undefined and "x" as string | undefined
func TestMaybeTruthinessMatchesNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	obj, nums, fn := ir.ObjectOf(pt), ir.ArrayOf(ir.Number), ir.FuncType(nil, ir.Number)
	values := []namedValue{
		{"n0", some(num(0))}, {"nNeg0", some(negZero)}, {"nNaN", some(div(0, 0))},
		{"n5", some(num(5))}, {"nU", undef(ir.Number)}, {"nN", null(ir.Number)},
		{"s0", some(str(""))}, {"sX", some(str("x"))}, {"sU", undef(ir.String)}, {"sN", null(ir.String)},
		{"bF", some(boolLit(false))}, {"bT", some(boolLit(true))}, {"bU", undef(ir.Boolean)}, {"bN", null(ir.Boolean)},
		{"i0", some(conv(ir.Int32, num(0)))}, {"i7", some(conv(ir.Int32, num(7)))}, {"iU", undef(ir.Int32)},
		{"l0", some(conv(ir.Int64, num(0)))}, {"u0", some(conv(ir.Uint32, num(0)))}, {"ul1", some(conv(ir.Uint64, num(1)))},
		{"fNaN", some(conv(ir.Float32, div(0, 0)))}, {"fHalf", some(conv(ir.Float32, num(0.5)))}, {"fN", null(ir.Float32)},
		{"e0", some(conv(ir.Base64, str("")))}, {"e1", some(conv(ir.Base64, str("aGk=")))},
		{"p", some(lit(pt, "x", num(0), "y", num(0)))}, {"pU", undef(obj)}, {"pN", null(obj)},
		{"xs", some(arrayOf(ir.Number))}, {"xsU", undef(nums)},
		{"g", some(arrow(nil, ir.Number, ret(num(0))))}, {"gN", null(fn)},
	}
	var direct []ir.Stmt
	for _, d := range []namedValue{{"undefined", undef(ir.Number)}, {"null", null(ir.String)}, {"some 0", some(num(0))}, {"some x", some(str("x"))}} {
		direct = append(direct, tested(d.name, d.init, false)...)
	}
	want := "n0 false false true no\nnNeg0 false false true no\nnNaN false false true no\n" +
		"n5 true true false yes\nnU false false true no\nnN false false true no\n" +
		"s0 false false true no\nsX true true false yes\nsU false false true no\nsN false false true no\n" +
		"bF false false true no\nbT true true false yes\nbU false false true no\nbN false false true no\n" +
		"i0 false false true no\ni7 true true false yes\niU false false true no\n" +
		"l0 false false true no\nu0 false false true no\nul1 true true false yes\n" +
		"fNaN false false true no\nfHalf true true false yes\nfN false false true no\n" +
		"e0 false false true no\ne1 true true false yes\n" +
		"p true true false yes\npU false false true no\npN false false true no\n" +
		"xs true true false yes\nxsU false false true no\n" +
		"g true true false yes\ngN false false true no\n" +
		"undefined false true no\nnull false true no\nsome 0 false true no\nsome x true false yes\n"
	if got := buildAndRun(t, truthyProgram(&shapes, values, direct...)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}
