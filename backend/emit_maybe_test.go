package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// some is v as a `T | null | undefined`, where T is v's type.
func some(v ir.Expr) *ir.Wrap { return &ir.Wrap{Value: v, Typ: ir.MaybeOf(v.ExprType()), Span: span} }

// undef and null are undefined and null as a `T | null | undefined`.
func undef(t ir.Type) *ir.Empty { return &ir.Empty{Typ: ir.MaybeOf(t), Span: span} }
func null(t ir.Type) *ir.Empty  { return &ir.Empty{Null: true, Typ: ir.MaybeOf(t), Span: span} }

// must is v, a maybe, as the T TypeScript narrowed it to.
func must(v ir.Expr) *ir.Unwrap { return &ir.Unwrap{Value: v, Typ: *v.ExprType().Elem, Span: span} }

// isEmpty is `v === undefined` (u), `v === null` (n) or `v == null` (both).
func isEmpty(v ir.Expr, u, n bool) *ir.IsEmpty {
	return &ir.IsEmpty{Value: v, Undefined: u, Null: n, Span: span}
}

func boolLit(b bool) ir.Expr { return &ir.BoolLit{Value: b, Span: span} }

func assign(name string, v ir.Expr) ir.Stmt { return &ir.Assign{Name: name, Value: v, Span: span} }

// TestEmitMaybe: each node as the Go a reader would check it against. An
// undefined Empty is parenthesised where it is a method's receiver, since Go
// would read a composite literal starting an if's condition as its block.
func TestEmitMaybe(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	box := shapes.Intern("Box", []ir.Field{{Name: "n", Type: ir.MaybeOf(ir.Number)}, {Name: "p", Type: ir.MaybeOf(ir.ObjectOf(pt))}})
	numM := ir.MaybeOf(ir.Number)
	v := stable("v", numM)
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "pick", Params: []ir.Param{{Name: "c", Type: ir.Boolean, Span: span}}, Result: numM, Span: span,
			Body: []ir.Stmt{&ir.If{Cond: stable("c", ir.Boolean), Then: []ir.Stmt{ret(some(num(1)))}, Span: span}}}},
		Main: []ir.Stmt{
			decl("v", numM, some(num(5))),
			decl("s", ir.MaybeOf(ir.String), null(ir.String)),
			decl("b", ir.MaybeOf(ir.Boolean), undef(ir.Boolean)),
			decl("o", ir.ObjectOf(box), lit(box, "n", undef(ir.Number), "p", some(lit(pt, "x", num(1), "y", num(2))))),
			logNum(isEmpty(v, true, false)), logNum(isEmpty(v, false, true)), logNum(isEmpty(v, true, true)),
			logNum(binary(ir.OpAdd, must(v), num(1), ir.Number)),
			&ir.If{Cond: binary(ir.OpEqual, undef(ir.Number), v, ir.Boolean), Then: []ir.Stmt{logNum(v)}, Span: span},
			&ir.If{Cond: isEmpty(undef(ir.Number), true, false), Then: []ir.Stmt{logNum(v)}, Span: span},
		}}
	got := Emit(withShapes(m, &shapes))
	mustParse(t, got)
	for _, want := range []string{
		"\tv = cakebearrt.Some[float64](5.0)\n",
		"\ts = cakebearrt.NullOf[string]()\n",
		"\tb = cakebearrt.Maybe[bool]{}\n",
		"var v cakebearrt.Maybe[float64]\n",
		"\tn cakebearrt.Maybe[float64]\n\tp cakebearrt.Maybe[*ts_shape0]\n",
		"o = &ts_shape1{ts_layout: ts_layout0, n: cakebearrt.Maybe[float64]{}, p: cakebearrt.Some[*ts_shape0](&ts_shape0{",
		"cakebearrt.LogBool(v.IsUndefined())", "cakebearrt.LogBool(v.IsNull())", "cakebearrt.LogBool(v.IsEmpty())",
		"cakebearrt.LogNumber((v.Must(\"v\") + 1.0))",
		"\tif (cakebearrt.Maybe[float64]{} == v) {\n",
		"\tif (cakebearrt.Maybe[float64]{}).IsUndefined() {\n",
		// A function whose end is reached returns undefined, as in JavaScript.
		"\t\treturn cakebearrt.Some[float64](1.0)\n\t}\n\treturn cakebearrt.Maybe[float64]{}\n}\n",
		// A Maybe holding an object is copied with its object; a Maybe of a
		// number is a value, copied with the struct.
		"\tcopied.p = cakebearrt.CloneValue(c, o.p)\n\treturn copied\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "copied.n =") {
		t.Errorf("a Maybe of a number was cloned on its own:\n%s", got)
	}
}

// TestUnwrappedNames: Must's message names the value as the source writes
// it, by its TypeScript name, not its Go one, and anything else by where it
// is.
func TestUnwrappedNames(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	headers := shapes.Intern("", []ir.Field{{Name: "content-type", Type: ir.MaybeOf(ir.String)}, {Name: "len", Type: ir.MaybeOf(ir.Number)}})
	o := stable("o", ir.ObjectOf(headers))
	xs := stable("xs", ir.ArrayOf(ir.MaybeOf(ir.Number)))
	idx := func(i ir.Expr) ir.Expr {
		return &ir.IndexGet{Array: xs, Index: i, Typ: ir.MaybeOf(ir.Number), Span: span}
	}
	at := ir.Span{File: "src/main.ts", Line: 4, Col: 9}
	awaited := &ir.Await{Value: stable("p", ir.PromiseOf(ir.MaybeOf(ir.Number))), Typ: ir.MaybeOf(ir.Number), Span: at}
	for _, tt := range []struct {
		x    ir.Expr
		want string
	}{
		{stable("type", ir.MaybeOf(ir.Number)), "type"},
		{get(o, "len"), "o.len"},
		{get(o, "content-type"), `o["content-type"]`},
		{idx(stable("i", ir.Number)), "xs[i]"},
		{idx(num(0)), "xs[0]"},
		{idx(binary(ir.OpAdd, stable("i", ir.Number), num(1), ir.Number)), "xs[…]"},
		{&ir.Call{Callee: "find", Args: []ir.Expr{num(1)}, Typ: ir.MaybeOf(ir.Number), Span: span}, "find(…)"},
		{&ir.Call{Callee: "next", Typ: ir.MaybeOf(ir.Number), Span: span}, "next()"},
		{method(ir.ArrayPop, xs, ir.MaybeOf(ir.Number)), "xs.pop()"},
		{awaited, "the value at src/main.ts:4:9"},
	} {
		if got := new(emitter).unwrapped(tt.x); got != tt.want {
			t.Errorf("unwrapped(%T) = %q, want %q", tt.x, got, tt.want)
		}
	}
}

// TestMaybeConstructsGoCantEmitAreRefused: what would build into a program
// that quietly does the wrong thing, or fail inside go build, is reported at
// its span.
func TestMaybeConstructsGoCantEmitAreRefused(t *testing.T) {
	t.Parallel()

	at := ir.Span{File: "t.ts", Line: 3, Col: 5}
	var shapes ir.Shapes
	opt := shapes.Intern("Opt", []ir.Field{{Name: "a", Type: ir.Number}, {Name: "b", Type: ir.MaybeOf(ir.String), Optional: true}})
	o := stable("o", ir.ObjectOf(opt))
	fnM := ir.MaybeOf(ir.FuncType(nil, ir.Void))
	f := stable("f", fnM)
	for _, tt := range []struct {
		name string
		stmt ir.Stmt
		want string
	}{
		// Writing an absent optional property gives the object its key
		// (step 5); a plain write would leave it unprinted.
		{"a write to an optional property", &ir.FieldSet{Object: o, Field: opt.Index("b"), Value: some(str("x")), Span: at},
			"cannot write the optional property b yet"},
		{"=== on maybes of functions", &ir.ExprStmt{Expr: &ir.Binary{Op: ir.OpEqual, Left: f, Right: f, Typ: ir.Boolean, Span: at}, Span: at},
			"cannot compare a (() => void) | null | undefined: Go can't compare functions"},
		{"includes over maybes of functions", do(&ir.ArrayOp{Method: ir.ArrayIncludes, Array: stable("fs", ir.ArrayOf(fnM)), Args: []ir.Expr{f}, Typ: ir.Boolean, Span: at}),
			"Go can't compare functions"},
		{"console.log of a maybe of a function", &ir.ExprStmt{Expr: &ir.ConsoleLog{Args: []ir.Expr{f}, Span: at}, Span: at},
			"cannot print a (() => void) | null | undefined yet"},
	} {
		m := &ir.Module{Name: "t", Funcs: []*ir.Func{{Name: "g", Span: span, Result: ir.Void, Params: []ir.Param{
			{Name: "o", Type: ir.ObjectOf(opt)}, {Name: "f", Type: fnM}, {Name: "fs", Type: ir.ArrayOf(fnM)},
		}, Body: []ir.Stmt{tt.stmt}}}}
		src, unsupported, err := emit(withShapes(m, &shapes))
		if err != nil {
			t.Errorf("%s: emit: %v", tt.name, err)
			continue
		}
		if len(unsupported) != 1 || unsupported[0].Span != at || !strings.Contains(unsupported[0].What, tt.want) {
			t.Errorf("%s: unsupported = %+v, want one at %v containing %q\n%s", tt.name, unsupported, at, tt.want, src)
		}
	}

	// A shape with a field the backend can't spell is reported where the
	// program first uses it, here through a maybe of it.
	var more ir.Shapes
	odd := more.Intern("Odd", []ir.Field{{Name: "f", Type: ir.Type{Kind: ir.KindMaybe + 100}}})
	oddM := ir.MaybeOf(ir.ObjectOf(odd))
	m := &ir.Module{Name: "t", Main: []ir.Stmt{&ir.VarDecl{Name: "o", Type: oddM, Init: &ir.Empty{Typ: oddM, Span: at}, Span: at}}}
	src, unsupported, err := emit(withShapes(m, &more))
	if err != nil || len(unsupported) != 1 || unsupported[0].Span != at || !strings.Contains(unsupported[0].What, "its f is a invalid") {
		t.Errorf("a shape reached through a maybe: %v, unsupported = %+v, want one at %v\n%s", err, unsupported, at, src)
	}
}
