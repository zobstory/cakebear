package backend

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// mustParse fails the test unless src is syntactically valid Go: a substring
// check alone would pass on output Go rejects.
func mustParse(t *testing.T, src string) {
	t.Helper()
	if _, err := parser.ParseFile(token.NewFileSet(), "out.go", src, 0); err != nil {
		t.Fatalf("emitted source does not parse: %v\n%s", err, src)
	}
}

func TestGoFuncType(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   ir.Type
		want string
	}{
		{ir.FuncType([]ir.Type{ir.Number, ir.String}, ir.Boolean), "func(float64, string) bool"},
		{ir.FuncType([]ir.Type{ir.Number}, ir.Void), "func(float64)"},
		{ir.FuncType(nil, ir.FuncType(nil, ir.Number)), "func() func() float64"},
		{ir.FuncType([]ir.Type{ir.FuncType([]ir.Type{ir.Int32}, ir.Void)}, ir.Void), "func(func(int32))"},
	} {
		if got := goType(tt.in); got != tt.want {
			t.Errorf("goType(%s) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A function-typed local is declared before it is assigned, so the literal can
// call itself, and a parameter the literal ignores is emitted as `_`.
func TestEmitFuncLit(t *testing.T) {
	t.Parallel()

	span := ir.Span{File: "t.ts", Line: 1, Col: 1}
	typ := ir.FuncType([]ir.Type{ir.Number, ir.String}, ir.Number)
	m := &ir.Module{
		Name: "t",
		Funcs: []*ir.Func{{Name: "g", Result: ir.Void, Span: span, Body: []ir.Stmt{&ir.VarDecl{
			Name: "f", Type: typ, Span: span,
			Init: &ir.FuncLit{
				Params: []ir.Param{{Name: "x", Type: ir.Number, Span: span}, {Type: ir.String, Span: span}},
				Result: ir.Number,
				Body: []ir.Stmt{&ir.Return{
					Value: &ir.Call{Callee: "f", Args: []ir.Expr{
						&ir.Ident{Name: "x", Typ: ir.Number, Span: span},
						&ir.StringLit{Value: "s", Span: span},
					}, Typ: ir.Number, Span: span},
					Span: span,
				}},
				Typ:  typ,
				Span: span,
			},
		}}}},
	}

	got := Emit(m)
	mustParse(t, got)
	for _, want := range []string{
		"\tvar f func(float64, string) float64\n",
		"\tf = func(x float64, _ string) float64 {\n",
		"\t\treturn f(x, \"s\")\n",
		"\t}\n\t_ = f\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}

// Go accepts only calls as expression statements, so any other expression is
// assigned to the blank identifier rather than emitted bare.
func TestEmitExprStmtDiscardsNonCalls(t *testing.T) {
	t.Parallel()

	span := ir.Span{File: "t.ts", Line: 1, Col: 1}
	x := &ir.Ident{Name: "x", Typ: ir.Number, Span: span}
	m := &ir.Module{
		Name: "t",
		Funcs: []*ir.Func{{
			Name: "g", Params: []ir.Param{{Name: "x", Type: ir.Number, Span: span}}, Result: ir.Void, Span: span,
			Body: []ir.Stmt{
				&ir.ExprStmt{Expr: &ir.Binary{Op: ir.OpMul, Left: x, Right: x, Typ: ir.Number, Span: span}, Span: span},
				&ir.ExprStmt{Expr: &ir.Call{Callee: "g", Args: []ir.Expr{x}, Typ: ir.Void, Span: span}, Span: span},
			},
		}},
	}

	got := Emit(m)
	mustParse(t, got)
	if !strings.Contains(got, "\t_ = (x * x)\n") {
		t.Errorf("non-call expression statement not discarded:\n%s", got)
	}
	if !strings.Contains(got, "\tg(x)\n") || strings.Contains(got, "_ = g(x)") {
		t.Errorf("call statement should be emitted bare:\n%s", got)
	}
}

// A literal nested inside another's body indents to its own depth.
func TestEmitNestedFuncLitIndentation(t *testing.T) {
	t.Parallel()

	span := ir.Span{File: "t.ts", Line: 1, Col: 1}
	inner := ir.FuncType(nil, ir.Number)
	outer := ir.FuncType(nil, inner)
	m := &ir.Module{
		Name: "t",
		Main: []ir.Stmt{&ir.VarDecl{
			Name: "mk", Type: outer, Span: span,
			Init: &ir.FuncLit{Result: inner, Typ: outer, Span: span, Body: []ir.Stmt{&ir.Return{
				Value: &ir.FuncLit{Result: ir.Number, Typ: inner, Span: span, Body: []ir.Stmt{&ir.Return{
					Value: &ir.NumberLit{Value: 1, Span: span}, Span: span,
				}}},
				Span: span,
			}}},
		}},
	}

	got := Emit(m)
	mustParse(t, got)
	want := "\tmk = func() func() float64 {\n" +
		"\t\treturn func() float64 {\n" +
		"\t\t\treturn 1.0\n" +
		"\t\t}\n" +
		"\t}\n"
	if !strings.Contains(got, want) {
		t.Errorf("nested literal indentation wrong; want\n%s\ngot:\n%s", want, got)
	}
}
