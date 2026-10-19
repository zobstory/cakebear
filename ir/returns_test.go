package ir

import (
	"strings"
	"testing"
)

// TestValidateChecksReturns: every return carries exactly what its function
// returns, a function literal's its own, so lowering that gets a result
// wrong is caught here rather than inside go build.
func TestValidateChecksReturns(t *testing.T) {
	t.Parallel()

	prog := func(fn *Func) *Program { return ProgramOf(&Module{Name: "t", Funcs: []*Func{fn}}) }
	ret := func(v Expr) Stmt { return &Return{Value: v} }
	lit := func(result Type, body ...Stmt) Stmt {
		return expr(&FuncLit{Result: result, Typ: FuncType(nil, result), Body: body})
	}
	for _, tt := range []struct {
		name string
		p    *Program
		want string
	}{
		{"a value from a void function", prog(&Func{Name: "f", Result: Void, Body: []Stmt{ret(n(1))}}), "a return of a number from a function that returns nothing"},
		{"nothing from a number function", prog(&Func{Name: "f", Result: Number, Body: []Stmt{ret(nil)}}), "a return of nothing from a function that returns a number"},
		{"the wrong type", prog(&Func{Name: "f", Result: String, Body: []Stmt{ret(n(1))}}), "a return of a number from a function that returns a string"},
		{"inside an if", prog(&Func{Name: "f", Result: Number, Body: []Stmt{&If{Cond: &BoolLit{Value: true}, Then: []Stmt{ret(str("a"))}}, ret(n(1))}}), "a return of a string"},
		{"inside a loop", prog(&Func{Name: "f", Result: Void, Body: []Stmt{&While{Cond: &BoolLit{Value: true}, Body: []Stmt{ret(n(1))}}}}), "a return of a number"},
		{"inside a literal", prog(&Func{Name: "f", Result: Void, Body: []Stmt{lit(Number, ret(str("a")))}}), "a return of a string from a function that returns a number"},
		{"an async function's", prog(&Func{Name: "f", Result: PromiseOf(Number), Async: true, Body: []Stmt{ret(str("a"))}}), "a return of a string from a function that returns a number"},
	} {
		err := tt.p.Validate()
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want an error containing %q", tt.name, err, tt.want)
		}
	}

	for _, tt := range []struct {
		name string
		p    *Program
	}{
		{"an async function resolving its promise", prog(&Func{Name: "f", Result: PromiseOf(Number), Async: true, Body: []Stmt{ret(n(1))}})},
		{"an async void function", prog(&Func{Name: "f", Result: PromiseOf(Void), Async: true, Body: []Stmt{ret(nil)}})},
		{"a base64 as a string", prog(&Func{Name: "f", Params: []Param{{Name: "b", Type: Base64}}, Result: String, Body: []Stmt{ret(ref("b", Base64))}})},
		{"a literal returning its own type in a void function", prog(&Func{Name: "f", Result: Void, Body: []Stmt{lit(Number, ret(n(1))), ret(nil)}})},
	} {
		if err := tt.p.Validate(); err != nil {
			t.Errorf("%s: Validate = %v, want nil", tt.name, err)
		}
	}
}

// TestValidateChecksDecls: a declaration's value is of its type, or a base64
// for a string, so a lowering that gets one wrong is caught here too.
func TestValidateChecksDecls(t *testing.T) {
	t.Parallel()

	prog := func(d *VarDecl) *Program { return ProgramOf(&Module{Name: "t", Main: []Stmt{d}}) }
	err := prog(&VarDecl{Name: "xs", Type: ArrayOf(Number), Init: &ArrayLit{Typ: ArrayOf(MaybeOf(Number))}}).Validate()
	if want := "xs, a number[], is given a (number | null | undefined)[]"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Validate = %v, want an error containing %q", err, want)
	}
	if err := prog(&VarDecl{Name: "s", Type: String, Init: &Ident{Name: "b", Typ: Base64}}).Validate(); err != nil && strings.Contains(err.Error(), "is given") {
		t.Errorf("a base64 as a string: Validate = %v", err)
	}
}
