package ir

import (
	"strings"
	"testing"
)

func TestMaybeType(t *testing.T) {
	t.Parallel()

	if got := MaybeOf(Number).String(); got != "number | null | undefined" {
		t.Errorf("MaybeOf(Number) is %q", got)
	}
	if got := MaybeOf(FuncType(nil, Void)).String(); got != "(() => void) | null | undefined" {
		t.Errorf("a maybe function is %q", got)
	}
	if got := ArrayOf(MaybeOf(Number)).String(); got != "(number | null | undefined)[]" {
		t.Errorf("an array of maybes is %q", got)
	}
	if !MaybeOf(Number).Equal(MaybeOf(Number)) || MaybeOf(Number).Equal(MaybeOf(String)) || MaybeOf(Number).Equal(Number) {
		t.Errorf("maybe types compare by their element types")
	}
	if Unserializable(MaybeOf(Number), nil) != "" || Unserializable(MaybeOf(FuncType(nil, Void)), nil) != "a function" {
		t.Errorf("a maybe serialises as its element does")
	}
}

// TestValidateChecksMaybes: each node moves between the types it says it
// does, and a maybe's element is a value no state of its own already is.
func TestValidateChecksMaybes(t *testing.T) {
	t.Parallel()

	maybe := MaybeOf(Number)
	v := &Ident{Name: "v", Typ: maybe}
	prog := func(e Expr) *Program {
		decl := &VarDecl{Name: "v", Type: maybe, Init: &Empty{Typ: maybe}}
		return ProgramOf(&Module{Name: "t", Main: []Stmt{decl, expr(e)}})
	}
	for _, tt := range []struct {
		name string
		e    Expr
		want string
	}{
		{"a maybe of a maybe", &Empty{Typ: MaybeOf(maybe)}, "a maybe of number | null | undefined"},
		{"a maybe of void", &Empty{Typ: MaybeOf(Void)}, "a maybe of void"},
		{"a maybe of undefined", &Empty{Typ: MaybeOf(Undefined)}, "a maybe of undefined"},
		{"a wrap of the wrong type", &Wrap{Value: str("a"), Typ: maybe}, "a string wrapped as a number | null | undefined"},
		{"an unwrap of a number", &Unwrap{Value: n(1), Typ: Number}, "a number unwrapped as a number"},
		{"an unwrap to the wrong type", &Unwrap{Value: v, Typ: String}, "unwrapped as a string"},
		{"an empty number", &Empty{Typ: Number}, "an empty number"},
		{"a test of a number", &IsEmpty{Value: n(1), Undefined: true}, "a number tested for null or undefined"},
		{"a test for nothing", &IsEmpty{Value: v}, "a test for neither null nor undefined"},
		{"a conditional on a number", &Cond{Test: n(1), Then: n(2), Else: n(3), Typ: Number}, "a conditional testing a number"},
		{"branches of two types", &Cond{Test: &BoolLit{Value: true}, Then: n(2), Else: str("a"), Typ: Number}, "a number conditional choosing between a number and a string"},
		// Inspect must look inside each node, or a bad one below it goes unseen.
		{"a bad node in a branch", &Cond{Test: &BoolLit{Value: true}, Then: &Empty{Typ: Number}, Else: n(1), Typ: Number}, "an empty number"},
		{"a bad node wrapped", &Wrap{Value: &Unwrap{Value: n(1), Typ: Number}, Typ: maybe}, "a number unwrapped as a number"},
		{"a let of a name the program could declare", &Let{Name: "x", Value: n(1), Body: n(2)}, "a let of \"x\""},
		{"a bad node in a let's body", &Let{Name: "ts held1", Value: n(1), Body: &Empty{Typ: Number}}, "an empty number"},
		{"a bad node held", &Let{Name: "ts held1", Value: &Empty{Typ: Number}, Body: n(1)}, "an empty number"},
		{"ToString of a maybe of an array", &BuiltinCall{Fn: ToString, Args: []Expr{&Empty{Typ: MaybeOf(ArrayOf(Number))}}, Typ: String}, "String takes one primitive, or a maybe of one"},
	} {
		if err := prog(tt.e).Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want an error containing %q", tt.name, err, tt.want)
		}
	}
	for _, e := range []Expr{
		&Wrap{Value: n(1), Typ: maybe},
		&Wrap{Value: &Ident{Name: "b", Typ: Base64}, Typ: MaybeOf(String)},
		&Unwrap{Value: v, Typ: Number},
		&Empty{Null: true, Typ: maybe},
		&IsEmpty{Value: v, Undefined: true, Null: true},
		&Let{Name: "ts held1", Value: v, Body: &Cond{Test: &IsEmpty{Value: &Ident{Name: "ts held1", Typ: maybe}, Null: true, Undefined: true},
			Then: n(0), Else: &Unwrap{Value: &Ident{Name: "ts held1", Typ: maybe}, Typ: Number}, Typ: Number}},
		&Unwrap{Value: v, Typ: Number, Asserted: true},
		&BuiltinCall{Fn: ToString, Args: []Expr{v}, Typ: String},
		&Cond{Test: &IsEmpty{Value: v, Undefined: true}, Then: n(0), Else: &Unwrap{Value: v, Typ: Number}, Typ: Number},
	} {
		if err := prog(e).Validate(); err != nil {
			t.Errorf("a well-formed %T: %v", e, err)
		}
	}
}

// TestOptionalFields: an optional field is a shape of its own, spelled as
// TypeScript spells it, holds a maybe, and may be left out of a literal,
// where a required one may not.
func TestOptionalFields(t *testing.T) {
	t.Parallel()

	var shapes Shapes
	opt := shapes.Intern("", []Field{{Name: "a", Type: Number}, {Name: "b", Type: MaybeOf(String), Optional: true}})
	req := shapes.Intern("", []Field{{Name: "a", Type: Number}, {Name: "b", Type: MaybeOf(String)}})
	if opt == req {
		t.Fatalf("{ a; b?: string } and { a; b: string | undefined } are one shape; Node prints them differently")
	}
	if got := opt.String(); got != "{ a: number; b?: string }" {
		t.Errorf("the optional shape is %q", got)
	}
	lit := func(s *Shape, fields ...FieldInit) *Program {
		return &Program{Modules: []*Module{{Name: "t", Main: []Stmt{expr(&ObjectLit{Fields: fields, Typ: ObjectOf(s)})}}}, Shapes: shapes.All()}
	}
	if err := lit(opt, FieldInit{Field: 0, Value: n(1)}).Validate(); err != nil {
		t.Errorf("a literal leaving out an optional field: %v", err)
	}
	if err := lit(req, FieldInit{Field: 0, Value: n(1)}).Validate(); err == nil || !strings.Contains(err.Error(), "leaves b unset") {
		t.Errorf("a literal leaving out a required field: %v", err)
	}

	// delete takes only an optional field; `in` any field of the shape.
	o := &Ident{Name: "o", Typ: ObjectOf(opt)}
	stmts := func(ss ...Stmt) *Program {
		return &Program{Modules: []*Module{{Name: "t", Main: ss}}, Shapes: shapes.All()}
	}
	if err := stmts(&FieldDelete{Object: o, Field: 1}, expr(&HasField{Object: o, Field: 0})).Validate(); err != nil {
		t.Errorf("delete of an optional field, and in: %v", err)
	}
	if err := stmts(&FieldDelete{Object: o, Field: 0}).Validate(); err == nil || !strings.Contains(err.Error(), "delete of a, a required field") {
		t.Errorf("delete of a required field: %v", err)
	}
	if err := stmts(expr(&HasField{Object: o, Field: 2})).Validate(); err == nil || !strings.Contains(err.Error(), "field 2 of") {
		t.Errorf("in of a field the shape doesn't have: %v", err)
	}
	if err := stmts(&FieldDelete{Object: n(1), Field: 0}).Validate(); err == nil || !strings.Contains(err.Error(), "a field of a number") {
		t.Errorf("delete from a number: %v", err)
	}

	// An unknown holds undefined itself, so it is optional as it is.
	var unknowns Shapes
	u := unknowns.Intern("", []Field{{Name: "b", Type: Unknown, Optional: true}})
	if err := (&Program{Modules: []*Module{{Name: "t"}}, Shapes: unknowns.All()}).Validate(); err != nil {
		t.Errorf("an optional unknown: %v", err)
	}
	if got := u.String(); got != "{ b?: unknown }" {
		t.Errorf("the optional unknown's shape is %q", got)
	}

	var bad Shapes
	bad.Intern("", []Field{{Name: "b", Type: String, Optional: true}})
	p := &Program{Modules: []*Module{{Name: "t"}}, Shapes: bad.All()}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "optional field b of { b: string } is a string") {
		t.Errorf("an optional field that isn't a maybe: %v", err)
	}
}

// TestUndefinedReads (D9): a read that may be undefined is of OrUndefined of
// what it reads, an array's elements, a maybe's own type for an array of
// them: an unchecked index, at, find, findLast, and pop and shift as values.
func TestUndefinedReads(t *testing.T) {
	t.Parallel()

	if !OrUndefined(Number).Equal(MaybeOf(Number)) || !OrUndefined(MaybeOf(Number)).Equal(MaybeOf(Number)) {
		t.Fatalf("OrUndefined is %s and %s", OrUndefined(Number), OrUndefined(MaybeOf(Number)))
	}
	nums := &Ident{Name: "xs", Typ: ArrayOf(Number)}
	maybes := &Ident{Name: "ms", Typ: ArrayOf(MaybeOf(Number))}
	found := &FuncLit{Typ: FuncType([]Type{Number, Number, ArrayOf(Number)}, Boolean), Result: Boolean}
	prog := func(e Expr) *Program { return ProgramOf(&Module{Name: "t", Main: []Stmt{expr(e)}}) }
	for _, e := range []Expr{
		&IndexGet{Array: nums, Index: n(0), Typ: MaybeOf(Number), OrUndefined: true},
		&IndexGet{Array: maybes, Index: n(0), Typ: MaybeOf(Number), OrUndefined: true},
		&ArrayOp{Method: ArrayPop, Array: nums, Typ: MaybeOf(Number)},
		&ArrayOp{Method: ArrayPop, Array: nums, Typ: Void},
		&ArrayOp{Method: ArrayShift, Array: maybes, Typ: MaybeOf(Number)},
		&ArrayOp{Method: ArrayAt, Array: nums, Args: []Expr{n(-1)}, Typ: MaybeOf(Number)},
		&ArrayOp{Method: ArrayFind, Array: nums, Args: []Expr{found}, Typ: MaybeOf(Number)},
		&ArrayOp{Method: ArrayFindLast, Array: nums, Args: []Expr{found}, Typ: MaybeOf(Number)},
		&BuiltinCall{Fn: StrAt, Args: []Expr{str("ab"), n(-1)}, Typ: MaybeOf(String)},
		&BuiltinCall{Fn: StrIndexOrUndefined, Args: []Expr{str("ab"), n(5)}, Typ: MaybeOf(String)},
	} {
		if err := prog(e).Validate(); err != nil {
			t.Errorf("a well-formed %#v: %v", e, err)
		}
	}
	for _, tt := range []struct {
		name string
		e    Expr
		want string
	}{
		{"an unchecked read typed as the element", &IndexGet{Array: nums, Index: n(0), Typ: Number, OrUndefined: true}, "used as a number"},
		{"a checked read typed as a maybe", &IndexGet{Array: nums, Index: n(0), Typ: MaybeOf(Number)}, "used as a number | null | undefined"},
		{"pop typed as the element", &ArrayOp{Method: ArrayPop, Array: nums, Typ: Number}, "pop on a number[] gives a number | null | undefined"},
		{"at with no index", &ArrayOp{Method: ArrayAt, Array: nums, Typ: MaybeOf(Number)}, "at takes 1 argument(s)"},
	} {
		if err := prog(tt.e).Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want an error containing %q", tt.name, err, tt.want)
		}
	}
}
