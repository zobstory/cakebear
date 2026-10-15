package backend

import (
	"errors"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// IR the backend cannot emit is refused by Build before go build runs, as a
// typed error naming each construct in source order. go build's own error
// would point at generated code the user never wrote.
//
// Every node lower/ produces today is emittable, so this uses the zero
// HostOp and HostProperty: values the backend has no mapping for.
func TestBuildRefusesIRItCannotEmit(t *testing.T) {
	t.Parallel()

	at := func(line int) ir.Span { return ir.Span{File: "t.ts", Line: line, Col: 1} }
	server := ir.HostOf(ir.HostHTTPServer)
	m := &ir.Module{
		Name: "t",
		Funcs: []*ir.Func{{
			Name: "f", Result: ir.Void, Span: at(1),
			Body: []ir.Stmt{&ir.ExprStmt{Span: at(2), Expr: &ir.HostProp{Prop: ir.HostPropInvalid, Typ: ir.String, Span: at(2)}}},
		}},
		Main: []ir.Stmt{&ir.VarDecl{
			Name: "s", Type: server, Span: at(1),
			Init: &ir.HostCall{Op: ir.HostOpInvalid, Typ: server, Span: at(1)},
		}},
	}

	src, err := Build(ir.ProgramOf(m), Options{Output: t.TempDir() + "/out"})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Build error = %v, want *UnsupportedError", err)
	}
	if len(unsupported.Nodes) != 2 {
		t.Fatalf("got %d unsupported nodes, want 2: %+v", len(unsupported.Nodes), unsupported.Nodes)
	}
	// Source order, although emission visits the function on line 2 first.
	if got := unsupported.Nodes[0]; got.Span.Line != 1 || !strings.Contains(got.What, "invalid host operation") {
		t.Errorf("first = %+v, want the host operation on line 1", got)
	}
	if got := unsupported.Nodes[1]; got.Span.Line != 2 || !strings.Contains(got.What, "invalid host property") {
		t.Errorf("second = %+v, want the host property on line 2", got)
	}
	if !strings.Contains(src, "#error") {
		t.Errorf("returned source should still show the placeholders for --emit-go:\n%s", src)
	}
}

// TestUnspellableIsUnsupported: a type, a printed value or an operator the
// backend has no Go for is reported at its span, never emitted as a stand-in
// (`any`, LogUndefined, `!`) that would build and quietly do the wrong
// thing. unknown stands in for a kind the IR gains before the backend learns
// it, as KindObject will.
func TestUnspellableIsUnsupported(t *testing.T) {
	t.Parallel()

	at := ir.Span{File: "t.ts", Line: 7, Col: 3}
	unknown := ir.Type{Kind: ir.KindPromise + 100}
	callback := ir.FuncType([]ir.Type{unknown}, ir.Void)
	fn := func(params []ir.Param, result ir.Type, async bool) *ir.Module {
		return &ir.Module{Name: "t", Funcs: []*ir.Func{{Name: "f", Params: params, Result: result, Async: async, Span: at}}}
	}
	stmt := func(e ir.Expr) *ir.Module {
		return &ir.Module{Name: "t", Main: []ir.Stmt{&ir.ExprStmt{Expr: e, Span: at}}}
	}

	for _, tt := range []struct {
		name string
		m    *ir.Module
		want string
	}{
		{"a variable of a kind it doesn't know", &ir.Module{Name: "t", Main: []ir.Stmt{
			&ir.VarDecl{Name: "x", Type: unknown, Init: &ir.Ident{Name: "y", Typ: unknown, Stable: true, Span: at}, Span: at},
		}}, "cannot emit a value of type invalid yet"},
		{"a void parameter", fn([]ir.Param{{Name: "x", Type: ir.Void, Span: at}}, ir.Void, false), "a value of type void"},
		{"a host type it has no runtime type for", fn([]ir.Param{{Name: "x", Type: ir.HostOf(ir.HostNone), Span: at}}, ir.Void, false), "a value of type host"},
		{"a function type with an unknown parameter", fn([]ir.Param{{Name: "cb", Type: callback, Span: at}}, ir.Void, false), "a value of type (invalid) => void"},
		{"a function type with an unknown result", fn([]ir.Param{{Name: "get", Type: ir.FuncType(nil, unknown), Span: at}}, ir.Void, false), "a value of type () => invalid"},
		{"a promise of an unknown type", fn(nil, ir.PromiseOf(unknown), true), "a value of type Promise<invalid>"},
		{"a new Promise of an unknown type", stmt(&ir.NewPromise{Executor: &ir.FuncLit{Result: ir.Void, Span: at}, Typ: ir.PromiseOf(unknown), Span: at}), "a value of type Promise<invalid>"},
		{"console.log of a function", stmt(&ir.ConsoleLog{Args: []ir.Expr{&ir.Ident{Name: "f", Typ: callback, Stable: true, Span: at}}, Span: at}), "cannot print a (invalid) => void yet"},
		{"an unknown binary operator", stmt(&ir.Binary{Op: ir.OpOr + 50, Left: num(1), Right: num(2), Typ: ir.Number, Span: at}), "cannot emit the operator"},
		{"an unknown unary operator", stmt(&ir.Unary{Op: ir.OpNot + 50, Operand: num(1), Typ: ir.Number, Span: at}), "cannot emit the operator ?"},
	} {
		src, unsupported, err := emit(ir.ProgramOf(tt.m))
		if err != nil {
			t.Errorf("%s: emit: %v", tt.name, err)
			continue
		}
		if len(unsupported) != 1 || unsupported[0].Span != at || !strings.Contains(unsupported[0].What, tt.want) {
			t.Errorf("%s: unsupported = %+v, want one at %v containing %q\n%s", tt.name, unsupported, at, tt.want, src)
		}
	}
}
