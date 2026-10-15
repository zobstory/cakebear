package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestEmitArrayMethods: the Go for a method taking a callback, which gets
// the caller's domain, one whose result has an element type of its own, a
// default separator, and an array read in the order JavaScript reads it,
// before a call among the arguments.
//
//	const xs: number[] = [3, 1, 2];
//	const ys: string[] = xs.map((x: number, i: number): string => [x, i].join(":"));
//	xs.sort((a: number, b: number): number => a - b);
//	console.log(xs.join());
//	let zs: number[] = [1];
//	function swap(): number { zs = [5]; return 5; }
//	console.log(zs.includes(swap()));
func TestEmitArrayMethods(t *testing.T) {
	t.Parallel()

	nums := ir.ArrayOf(ir.Number)
	xs, x, i := stable("xs", nums), stable("x", ir.Number), stable("i", ir.Number)
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "swap", Result: ir.Number, Span: span, Body: []ir.Stmt{
			&ir.Assign{Name: "zs", Value: arrayOf(ir.Number, num(5)), Span: span}, ret(num(5))}}},
		Main: []ir.Stmt{
			decl("xs", nums, arrayOf(ir.Number, num(3), num(1), num(2))),
			decl("ys", ir.ArrayOf(ir.String), method(ir.ArrayMap, xs, ir.ArrayOf(ir.String),
				arrow(visit(ir.Number, "x", "i", ""), ir.String, ret(method(ir.ArrayJoin, arrayOf(ir.Number, x, i), ir.String, str(":")))))),
			do(method(ir.ArraySort, xs, nums, arrow([]ir.Param{{Name: "a", Type: ir.Number}, {Name: "b", Type: ir.Number}}, ir.Number,
				ret(binary(ir.OpSub, stable("a", ir.Number), stable("b", ir.Number), ir.Number))))),
			logNum(method(ir.ArrayJoin, xs, ir.String)),
			&ir.VarDecl{Name: "zs", Type: nums, Init: arrayOf(ir.Number, num(1)), Mutable: true, Span: span},
			logNum(method(ir.ArrayIncludes, ident("zs", nums), ir.Boolean, &ir.Call{Callee: "swap", Typ: ir.Number, Span: span})),
		}}
	src := Emit(ir.ProgramOf(m))
	for _, want := range []string{
		"ys = cakebearrt.Map(ts_domain, xs, func(ts_domain *cakebearrt.Domain, x float64, i float64, _ *cakebearrt.Array[float64]) string {\n" +
			"\t\treturn cakebearrt.NewArray[float64](x, i).Join(\":\")\n\t})\n",
		"\txs.Sort(ts_domain, func(ts_domain *cakebearrt.Domain, a float64, b float64) float64 {\n\t\treturn (a - b)\n\t})\n",
		`cakebearrt.LogString(xs.Join(","))`,
		"cakebearrt.LogBool(cakebearrt.Includes(ts_read(zs), swap(ts_domain)))",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the Go lacks\n%s\nin\n%s", want, src)
		}
	}
	mustParse(t, src)
}

// TestUnjoinableAndUncomparableElementsAreUnsupported: join of an element
// the runtime can't convert to a string, and includes or indexOf of a
// function, which Go can't compare, are reported rather than emitted.
func TestUnjoinableAndUncomparableElementsAreUnsupported(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	own := shapes.Intern("Own", []ir.Field{{Name: "toString", Type: ir.FuncType(nil, ir.String)}})
	plain := point(&shapes)
	fn := ir.FuncType(nil, ir.Number)
	at := func(line int) ir.Span { return ir.Span{File: "t.ts", Line: line, Col: 1} }
	op := func(line int, m ir.ArrayMethod, xs string, elem, t ir.Type, args ...ir.Expr) ir.Stmt {
		return logNum(&ir.ArrayOp{Method: m, Array: stable(xs, ir.ArrayOf(elem)), Args: args, Typ: t, Span: at(line)})
	}
	f := stable("f", fn)
	m := &ir.Module{Name: "t", Funcs: []*ir.Func{{Name: "g", Span: span, Result: ir.Void,
		Params: []ir.Param{
			{Name: "fs", Type: ir.ArrayOf(fn)}, {Name: "os", Type: ir.ArrayOf(ir.ObjectOf(own))},
			{Name: "pss", Type: ir.ArrayOf(ir.ArrayOf(ir.PromiseOf(ir.Number)))}, {Name: "ps", Type: ir.ArrayOf(ir.ObjectOf(plain))},
			{Name: "f", Type: fn},
		},
		Body: []ir.Stmt{
			op(1, ir.ArrayJoin, "fs", fn, ir.String),
			op(2, ir.ArrayJoin, "os", ir.ObjectOf(own), ir.String),
			op(3, ir.ArrayJoin, "pss", ir.ArrayOf(ir.PromiseOf(ir.Number)), ir.String, str("-")),
			op(4, ir.ArrayIncludes, "fs", fn, ir.Boolean, f),
			op(5, ir.ArrayIndexOf, "fs", fn, ir.Number, f),
			op(6, ir.ArrayJoin, "ps", ir.ObjectOf(plain), ir.String),
		}}}}
	_, unsupported, err := emit(withShapes(m, &shapes))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"the backend cannot join an array of () => number yet: a () => number has no string the runtime can give",
		"the backend cannot join an array of Own yet: an object with its own toString",
		"the backend cannot join an array of Promise<number>[] yet: a Promise<number> has no string the runtime can give",
		"the backend cannot emit includes on an array of () => number: Go can't compare functions",
		"the backend cannot emit indexOf on an array of () => number: Go can't compare functions",
	}
	if len(unsupported) != len(want) {
		t.Fatalf("got %d unsupported, want %d: %+v", len(unsupported), len(want), unsupported)
	}
	for j, u := range unsupported {
		if u.Span.Line != j+1 || u.What != want[j] {
			t.Errorf("unsupported %d = line %d %q, want line %d %q", j, u.Span.Line, u.What, j+1, want[j])
		}
	}
}
