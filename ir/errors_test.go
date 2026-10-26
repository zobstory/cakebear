package ir

import (
	"strings"
	"testing"
)

func TestErrorTypes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		typ  Type
		want string
	}{
		{Unknown, "unknown"},
		{Error, "Error"},
		{MaybeOf(Error), "Error | null | undefined"},
		{ArrayOf(Unknown), "unknown[]"},
	} {
		if got := tt.typ.String(); got != tt.want {
			t.Errorf("%#v is %q, want %q", tt.typ, got, tt.want)
		}
	}
	if !Unknown.Equal(Unknown) || !Error.Equal(Error) || Unknown.Equal(Error) || Error.Equal(String) {
		t.Error("unknown and Error are each equal to themselves and to nothing else")
	}
	if Unserializable(Error, nil) != "" || Unserializable(ArrayOf(Unknown), nil) != "" {
		t.Error("an Error, and an unknown, which holds only data, serialise")
	}

	for i, name := range []string{"Error", "TypeError", "RangeError", "URIError", "SyntaxError", "ReferenceError", "EvalError"} {
		k, ok := ErrorKindNamed(name)
		if !ok || k != ErrorKind(i) || k.String() != name {
			t.Errorf("ErrorKindNamed(%q) = %v, %v; want kind %d, named so", name, k, ok, i)
		}
	}
	if _, ok := ErrorKindNamed("AggregateError"); ok {
		t.Error("AggregateError is not one of the kinds M6 builds")
	}
	for i, name := range []string{"undefined", "object", "boolean", "number", "string", "function", "symbol", "bigint"} {
		tag, ok := TypeTagNamed(name)
		if !ok || tag != TypeTag(i) || tag.String() != name {
			t.Errorf("TypeTagNamed(%q) = %v, %v; want tag %d, named so", name, tag, ok, i)
		}
	}
	if _, ok := TypeTagNamed("Object"); ok {
		t.Error("typeof gives lower-case names only")
	}

	for p, want := range map[ErrorProp]Type{ErrorMessage: String, ErrorName: String, ErrorStack: MaybeOf(String), ErrorCause: Unknown} {
		if got := (&ErrorGet{Error: ref("e", Error), Prop: p}).ExprType(); !got.Equal(want) {
			t.Errorf("an error's %s is a %s, want a %s, as TypeScript's libs declare it", p, got, want)
		}
	}
}

// TestValidateChecksErrors: each node carries the types it says it does, an
// unknown lets in only data, and Inspect looks inside every node and every
// block of a try, or a bad node there would go unseen.
func TestValidateChecksErrors(t *testing.T) {
	t.Parallel()

	var shapes Shapes
	holdsFn := shapes.Intern("H", []Field{{Name: "f", Type: FuncType(nil, Void)}})
	errM := MaybeOf(Error)
	result := shapes.Intern("Result", []Field{{Name: "error", Type: errM}, {Name: "detail", Type: Unknown}})
	e := ref("e", Error)
	u := ref("u", Unknown)
	prog := func(stmts ...Stmt) *Program {
		decls := []Stmt{
			&VarDecl{Name: "e", Type: Error, Init: &NewError{Kind: ErrorType, Message: str("m")}},
			&VarDecl{Name: "u", Type: Unknown, Init: &Box{Value: n(1)}},
		}
		p := ProgramOf(&Module{Name: "t", Main: append(decls, stmts...)})
		p.Shapes = shapes.All()
		return p
	}
	bad := &Unbox{Value: n(1), Typ: Number} // a node Validate refuses, for nesting
	const badMsg = "a number unboxed as a number"
	fn := &FuncLit{Result: Void, Typ: FuncType(nil, Void)}
	for _, tt := range []struct {
		name string
		s    Stmt
		want string
	}{
		{"a throw of nothing", &Throw{}, "a throw of nothing"},
		{"a throw of a function", &Throw{Value: fn}, "a throw of a () => void, which holds a function"},
		{"a try with neither clause", &Try{Body: []Stmt{expr(n(1))}}, "a try with neither a catch nor a finally"},
		{"a new error of no kind", expr(&NewError{Kind: 9}), "a new error of ErrorKind(9)"},
		{"a new error with a number message", expr(&NewError{Kind: ErrorRange, Message: n(5)}), "a new RangeError with a number message"},
		{"a message of a string", expr(&ErrorGet{Error: str("a"), Prop: ErrorMessage}), "an error's message read from a string"},
		{"an error's property of no kind", expr(&ErrorGet{Error: e, Prop: 9}), "an error's ErrorProp(9)"},
		{"an unknown boxed", expr(&Box{Value: u}), "an unknown boxed as unknown"},
		{"an i32 boxed", expr(&Box{Value: ref("i", Int32)}), "an i32 boxed as unknown"},
		{"a function boxed", expr(&Box{Value: fn}), "boxed as unknown, which holds a function"},
		{"an object holding a function boxed", expr(&Box{Value: ref("o", ObjectOf(holdsFn))}), "which holds a function"},
		{"a promise boxed", expr(&Box{Value: ref("p", PromiseOf(Number))}), "which holds a promise"},
		{"a number unboxed", expr(bad), badMsg},
		{"an unknown unboxed as unknown", expr(&Unbox{Value: u, Typ: Unknown}), "an unknown unboxed as an unknown"},
		{"an unknown unboxed as void", expr(&Unbox{Value: u, Typ: Void}), "an unknown unboxed as a void"},
		{"instanceof on a string", expr(&InstanceOf{Value: str("a"), Kind: ErrorBase}), "a string tested for instanceof Error"},
		{"instanceof on a maybe of a number", expr(&InstanceOf{Value: ref("m", MaybeOf(Number)), Kind: ErrorBase}), "tested for instanceof Error"},
		{"instanceof no kind", expr(&InstanceOf{Value: u, Kind: 9}), "instanceof ErrorKind(9)"},
		{"typeof of a string", expr(&TypeOf{Value: str("a"), Tag: TagString}), "typeof of a string, which is known while compiling"},
		{"typeof against no tag", expr(&TypeOf{Value: u, Tag: 9}), "typeof against TypeTag(9)"},
		{"an Error tested for null", expr(&IsEmpty{Value: e, Null: true}), "a Error tested for null or undefined"},
		{"ToString of a function", expr(&BuiltinCall{Fn: ToString, Args: []Expr{fn}, Typ: String}), "String takes one primitive"},
		// Inspect must look inside each of them.
		{"a bad node thrown", &Throw{Value: bad}, badMsg},
		{"a bad node in a try", &Try{Body: []Stmt{expr(bad)}, Finally: []Stmt{}}, badMsg},
		{"a bad node in a catch", &Try{Catch: &Catch{Name: "x", Body: []Stmt{expr(bad)}}}, badMsg},
		{"a bad node in a finally", &Try{Finally: []Stmt{expr(bad)}}, badMsg},
		{"a bad message", expr(&NewError{Message: &BuiltinCall{Fn: ToString, Args: []Expr{bad}, Typ: String}}), badMsg},
		{"a bad error read", expr(&ErrorGet{Error: &Unbox{Value: n(1), Typ: Error}, Prop: ErrorName}), "a number unboxed as an Error"},
		{"a bad node boxed", expr(&Box{Value: bad}), badMsg},
		{"a bad node unboxed", expr(&Unbox{Value: &Box{Value: bad}, Typ: Number}), badMsg},
		{"a bad node tested", expr(&InstanceOf{Value: &Box{Value: bad}, Kind: ErrorBase}), badMsg},
		{"a bad node typeof", expr(&TypeOf{Value: &Box{Value: bad}, Tag: TagNumber}), badMsg},
	} {
		if err := prog(tt.s).Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want an error containing %q", tt.name, err, tt.want)
		}
	}

	for _, s := range []Stmt{
		&Throw{Value: e},
		&Throw{Value: u},
		&Throw{Value: str("m")},
		&Try{Body: []Stmt{&Throw{Value: e}}, Catch: &Catch{Name: "x", Body: []Stmt{expr(ref("x", Unknown))}}},
		&Try{Body: []Stmt{expr(n(1))}, Catch: &Catch{Body: []Stmt{}}},
		&Try{Body: []Stmt{expr(n(1))}, Finally: []Stmt{}},
		&Try{Body: []Stmt{expr(n(1))}, Catch: &Catch{Name: "x"}, Finally: []Stmt{expr(n(2))}},
		expr(&NewError{Kind: ErrorURI}),
		expr(&ErrorGet{Error: e, Prop: ErrorStack}),
		expr(&ErrorGet{Error: &Unbox{Value: u, Typ: Error}, Prop: ErrorMessage}),
		expr(&Box{Value: ref("m", MaybeOf(Number))}),
		expr(&Box{Value: e}),
		expr(&Box{Value: ref("r", ObjectOf(result))}),
		expr(&Unbox{Value: u, Typ: MaybeOf(String)}),
		expr(&InstanceOf{Value: u, Kind: ErrorType}),
		expr(&InstanceOf{Value: e, Kind: ErrorBase}),
		expr(&InstanceOf{Value: ref("em", errM), Kind: ErrorBase}),
		expr(&TypeOf{Value: u, Tag: TagObject}),
		// An unknown is tested for null as it is, and an Error and an unknown
		// convert to a string (M6 step 3).
		expr(&IsEmpty{Value: u, Null: true}),
		expr(&BuiltinCall{Fn: ToString, Args: []Expr{u}, Typ: String}),
		expr(&BuiltinCall{Fn: ToString, Args: []Expr{e}, Typ: String}),
	} {
		if err := prog(s).Validate(); err != nil {
			t.Errorf("a well-formed %T: %v", s, err)
		}
	}
}

// TestValidateChecksReturnsInATry: a return in any of a try's blocks is held
// to its function's result, as one anywhere else is.
func TestValidateChecksReturnsInATry(t *testing.T) {
	t.Parallel()

	ret := func(v Expr) Stmt { return &Return{Value: v} }
	prog := func(try *Try) *Program {
		return ProgramOf(&Module{Name: "t", Funcs: []*Func{{Name: "f", Result: Number, Body: []Stmt{try, ret(n(0))}}}})
	}
	for name, try := range map[string]*Try{
		"the try block":     {Body: []Stmt{ret(str("a"))}, Finally: []Stmt{}},
		"the catch block":   {Catch: &Catch{Name: "x", Body: []Stmt{ret(str("a"))}}},
		"the finally block": {Finally: []Stmt{ret(str("a"))}},
	} {
		if err := prog(try).Validate(); err == nil || !strings.Contains(err.Error(), "a return of a string from a function that returns a number") {
			t.Errorf("a string returned in %s: Validate = %v", name, err)
		}
	}
	if err := prog(&Try{Body: []Stmt{ret(n(1))}, Catch: &Catch{Body: []Stmt{ret(n(2))}}, Finally: []Stmt{ret(n(3))}}).Validate(); err != nil {
		t.Errorf("numbers returned in every block: %v", err)
	}
}
