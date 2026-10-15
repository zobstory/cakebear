package ir

import (
	"strings"
	"testing"
)

// aggregates is the fixture for the Validate tests: a Point shape, a number
// array, and a builder for programs over them.
type aggregates struct {
	shapes *Shapes
	point  *Shape
	pt     Type
	nums   Type
}

func newAggregates() aggregates {
	shapes, point := pointShapes()
	return aggregates{shapes: shapes, point: point, pt: ObjectOf(point), nums: ArrayOf(Number)}
}

func (a aggregates) program(main ...Stmt) *Program {
	return &Program{Modules: []*Module{{Name: "t", Main: main}}, Shapes: a.shapes.All()}
}

func ref(name string, t Type) *Ident { return &Ident{Name: name, Typ: t, Stable: true} }
func n(v float64) Expr               { return &NumberLit{Value: v} }
func str(v string) Expr              { return &StringLit{Value: v} }
func expr(e Expr) Stmt               { return &ExprStmt{Expr: e} }

func (a aggregates) op(m ArrayMethod, typ Type, args ...Expr) Stmt {
	return expr(&ArrayOp{Method: m, Array: ref("xs", a.nums), Args: args, Typ: typ})
}

func (a aggregates) visitor(result Type) Expr {
	return ref("f", FuncType([]Type{Number, Number, a.nums}, result))
}

// TestValidateAcceptsEveryAggregateNode: each new node, used as lowering
// will use it, and every array method with the arguments D16 gives it.
func TestValidateAcceptsEveryAggregateNode(t *testing.T) {
	t.Parallel()

	a := newAggregates()
	p := a.program(
		// { y: 2, x: 1 }: source order, not the shape's.
		&VarDecl{Name: "pt", Type: a.pt, Init: &ObjectLit{Typ: a.pt, Fields: []FieldInit{{Field: 1, Value: n(2)}, {Field: 0, Value: n(1)}}}},
		&FieldSet{Object: ref("pt", a.pt), Field: 0, Value: n(3)},
		expr(&FieldGet{Object: ref("pt", a.pt), Field: 1, Typ: Number}),
		&VarDecl{Name: "xs", Type: a.nums, Init: &ArrayLit{Typ: a.nums, Elems: []Expr{n(1), n(2)}}},
		&VarDecl{Name: "none", Type: a.nums, Init: &ArrayLit{Typ: a.nums}},
		&IndexSet{Array: ref("xs", a.nums), Index: n(0), Value: n(5)},
		expr(&IndexGet{Array: ref("xs", a.nums), Index: n(0), Typ: Number}),
		&ForOf{Name: "x", Type: Number, Array: ref("xs", a.nums)},
		a.op(ArrayLength, Number),
		a.op(ArraySetLength, Void, n(1)),
		a.op(ArrayPush, Number, n(1)),
		a.op(ArrayPush, Number, n(1), n(2)),
		a.op(ArrayPop, Void),
		a.op(ArrayMap, ArrayOf(String), a.visitor(String)),
		a.op(ArrayFilter, a.nums, a.visitor(Boolean)),
		a.op(ArraySome, Boolean, a.visitor(Boolean)),
		a.op(ArrayEvery, Boolean, a.visitor(Boolean)),
		a.op(ArrayFindIndex, Number, a.visitor(Boolean)),
		a.op(ArrayForEach, Void, a.visitor(Void)),
		a.op(ArrayJoin, String),
		a.op(ArrayJoin, String, str(", ")),
		a.op(ArrayIncludes, Boolean, n(1)),
		a.op(ArrayIndexOf, Number, n(1)),
		a.op(ArraySlice, a.nums),
		a.op(ArraySlice, a.nums, n(1)),
		a.op(ArraySlice, a.nums, n(1), n(2)),
		a.op(ArraySort, a.nums, ref("cmp", FuncType([]Type{Number, Number}, Number))),
		a.op(ArrayReduce, String, ref("f", FuncType([]Type{String, Number, Number, a.nums}, String)), str("")),
	)
	if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestValidateRefusesBadAggregates: each fixture breaks one invariant, and
// Validate names it.
func TestValidateRefusesBadAggregates(t *testing.T) {
	t.Parallel()

	a := newAggregates()
	var elsewhere Shapes
	foreign := ObjectOf(elsewhere.Intern("", []Field{{"x", Number}}))
	lit := func(fields ...FieldInit) Stmt { return expr(&ObjectLit{Typ: a.pt, Fields: fields}) }

	for _, tt := range []struct {
		name string
		p    *Program
		want string
	}{
		{"an object of a shape not in the program", a.program(&VarDecl{Name: "o", Type: foreign, Init: ref("p", foreign)}), "not one of the program's shapes"},
		{"a function parameter of a foreign shape", &Program{Shapes: a.shapes.All(), Modules: []*Module{{Name: "t", Funcs: []*Func{{Name: "f", Params: []Param{{Name: "o", Type: foreign}}}}}}}, "parameter o: an object of"},
		{"an array type with no element", a.program(expr(ref("xs", Type{Kind: KindArray}))), "has no element type"},
		{"a builtin's argument of the wrong type", a.program(expr(&BuiltinCall{Fn: StrSlice, Args: []Expr{n(1)}, Typ: String})), "slice's argument 1 is a number, not a string"},
		{"a builtin with too many arguments", a.program(expr(&BuiltinCall{Fn: StrTrim, Args: []Expr{str("a"), str("b")}, Typ: String})), "trim takes 1 to 1 arguments, not 2"},
		{"a builtin's result of the wrong type", a.program(expr(&BuiltinCall{Fn: StrLength, Args: []Expr{str("a")}, Typ: String})), "length gives a number, not a string"},
		{"ToString of an object", a.program(expr(&BuiltinCall{Fn: ToString, Args: []Expr{ref("p", a.pt)}, Typ: String})), "String takes one primitive"},
		{"a template part that isn't a string", a.program(expr(&Concat{Parts: []Expr{str("a"), n(1)}})), "part 2 of a template is a number, not a string"},
		{"JSON.stringify of a function", a.program(expr(&JSONStringify{Value: ref("f", FuncType(nil, Number))})), "JSON.stringify of a () => number, which holds a function"},
		{"JSON.stringify of an array of promises", a.program(expr(&JSONStringify{Value: ref("ps", ArrayOf(PromiseOf(Number)))})), "which holds a promise"},
		{"JSON.stringify with a boolean space", a.program(expr(&JSONStringify{Value: n(1), Space: &BoolLit{Value: true}})), "space is a boolean"},
		{"a literal leaving a field unset", a.program(lit(FieldInit{Field: 0, Value: n(1)})), "leaves y unset"},
		{"a literal setting a field twice", a.program(lit(FieldInit{Field: 0, Value: n(1)}, FieldInit{Field: 0, Value: n(1)})), "sets x twice"},
		{"a literal setting a field out of range", a.program(lit(FieldInit{Field: 2, Value: n(1)})), "sets field 2 of 2"},
		{"a literal setting a field to the wrong type", a.program(lit(FieldInit{Field: 0, Value: str("1")}, FieldInit{Field: 1, Value: n(1)})), "sets x, a number, to a string"},
		{"a field read out of range", a.program(expr(&FieldGet{Object: ref("pt", a.pt), Field: 5, Typ: Number})), "field 5 of Point"},
		{"a field read as the wrong type", a.program(expr(&FieldGet{Object: ref("pt", a.pt), Field: 0, Typ: String})), "x of Point is a number, used as a string"},
		{"a field of a non-object", a.program(expr(&FieldGet{Object: n(1), Field: 0, Typ: Number})), "a field of a number"},
		{"a field write of the wrong type", a.program(&FieldSet{Object: ref("pt", a.pt), Field: 1, Value: str("")}), "y of Point is a number, used as a string"},
		{"an array indexed by a string", a.program(expr(&IndexGet{Array: ref("xs", a.nums), Index: str("0"), Typ: Number})), "indexed by a string"},
		{"an element read as the wrong type", a.program(expr(&IndexGet{Array: ref("xs", a.nums), Index: n(0), Typ: String})), "an element of a number[] used as a string"},
		{"an element write of the wrong type", a.program(&IndexSet{Array: ref("xs", a.nums), Index: n(0), Value: str("")}), "used as a string"},
		{"an array literal with an element of the wrong type", a.program(expr(&ArrayLit{Typ: a.nums, Elems: []Expr{n(1), str("2")}})), "element 1 of a number[] literal is a string"},
		{"for…of binding the wrong type", a.program(&ForOf{Name: "x", Type: String, Array: ref("xs", a.nums)}), "binds x as a string"},
		{"for…of over a non-array", a.program(&ForOf{Name: "x", Type: Number, Array: n(1)}), "an array operation on a number"},
		{"push of the wrong type", a.program(a.op(ArrayPush, Number, n(1), str(""))), "push's argument 2 is a string"},
		{"push of nothing", a.program(a.op(ArrayPush, Number)), "push takes 1 argument(s), not 0"},
		{"length given an argument", a.program(a.op(ArrayLength, Number, n(1))), "length takes 0 argument(s), not 1"},
		{"length typed as a string", a.program(a.op(ArrayLength, String)), "gives a number, typed string"},
		{"a callback missing the index parameter", a.program(a.op(ArrayFilter, a.nums, ref("f", FuncType([]Type{Number}, Boolean)))), "filter's argument 1 is a (number) => boolean"},
		{"map typed as the wrong array", a.program(a.op(ArrayMap, a.nums, a.visitor(String))), "gives a string[], typed number[]"},
		{"filter's callback returning a number", a.program(a.op(ArrayFilter, a.nums, a.visitor(Number))), "filter's argument 1"},
		{"slice of three arguments", a.program(a.op(ArraySlice, a.nums, n(1), n(2), n(3))), "slice takes at most 2"},
		{"join of a number", a.program(a.op(ArrayJoin, String, n(1))), "join's argument 1 is a number"},
		{"sort with a visitor's signature", a.program(a.op(ArraySort, a.nums, a.visitor(Number))), "sort's argument 1"},
		{"reduce without an initial value", a.program(a.op(ArrayReduce, Number, ref("f", FuncType([]Type{Number, Number, Number, a.nums}, Number)))), "reduce takes 2 argument(s), not 1"},
		{"reduce typed as the wrong result", a.program(a.op(ArrayReduce, Number, ref("f", FuncType([]Type{String, Number, Number, a.nums}, String)), str(""))), "gives a string, typed number"},
		{"an unknown method", a.program(a.op(ArrayMethodInvalid, Void)), "invalid array method on a number[]"},
	} {
		err := tt.p.Validate()
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want an error containing %q", tt.name, err, tt.want)
		}
	}
}

// TestValidateRefusesBadShapes: Program.Shapes as interning leaves it, each
// fixture breaking it one way.
func TestValidateRefusesBadShapes(t *testing.T) {
	t.Parallel()

	shape := func(id int, fields ...Field) *Shape { return &Shape{ID: id, Fields: fields} }
	for _, tt := range []struct {
		name   string
		shapes []*Shape
		want   string
	}{
		{"a shape at the wrong index", []*Shape{shape(1, Field{"x", Number})}, "shape 0 ({ x: number }) has ID 1"},
		{"fields out of order", []*Shape{shape(0, Field{"y", Number}, Field{"x", Number})}, `fields "y" and "x" out of order`},
		{"a field twice", []*Shape{shape(0, Field{"x", Number}, Field{"x", Number})}, `fields "x" and "x" out of order, or one twice`},
		{"one structure twice", []*Shape{shape(0, Field{"x", Number}), shape(1, Field{"x", Number})}, "shapes 0 and 1 have the same fields"},
		{"a reserved shape never defined", []*Shape{{ID: 0, pending: true, Names: []string{"Tree"}}}, "shape 0 (Tree) was reserved and never defined"},
		{"a nil shape", []*Shape{nil}, "shape 0 is nil"},
		{"a field of a foreign shape", []*Shape{shape(0, Field{"next", ObjectOf(shape(0))})}, "field next of { next: {} }: an object of {}, which is not one of the program's shapes"},
	} {
		p := &Program{Modules: []*Module{{Name: "t"}}, Shapes: tt.shapes}
		if err := p.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want an error containing %q", tt.name, err, tt.want)
		}
	}
}
