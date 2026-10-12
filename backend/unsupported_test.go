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

	src, err := Build(m, Options{Output: t.TempDir() + "/out"})
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

// M2's IR lands before its emitter. Until then every async construct must be
// refused by name. An Async flag in particular has no node of its own to fall
// through to the default case: ignoring it would emit an ordinary function
// that builds and returns a value where its callers expect a promise.
func TestBuildRefusesAsyncIRItCannotEmitYet(t *testing.T) {
	t.Parallel()

	at := func(line int) ir.Span { return ir.Span{File: "t.ts", Line: line, Col: 1} }
	num := ir.PromiseOf(ir.Number)
	void := ir.PromiseOf(ir.Void)
	m := &ir.Module{
		Name: "t",
		Funcs: []*ir.Func{{
			Name: "g", Result: num, Async: true, Span: at(1),
			Body: []ir.Stmt{&ir.Return{Span: at(2), Value: &ir.Await{
				Value: &ir.Call{Callee: "g", Typ: num, Span: at(2)}, Typ: ir.Number, Span: at(2),
			}}},
		}},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "p", Type: void, Span: at(3), Init: &ir.NewPromise{
				Executor: &ir.FuncLit{Typ: ir.ExecutorType(ir.Void), Result: ir.Void, Span: at(3)},
				Typ:      void, Span: at(3),
			}},
			&ir.VarDecl{Name: "h", Type: ir.FuncType(nil, void), Span: at(4), Init: &ir.FuncLit{
				Async: true, Result: void, Typ: ir.FuncType(nil, void), Span: at(4),
			}},
		},
	}

	_, err := Build(m, Options{Output: t.TempDir() + "/out"})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Build error = %v, want *UnsupportedError", err)
	}
	want := []struct {
		line int
		what string
	}{
		{1, "async function g"},
		{2, "await"},
		{3, "new Promise"},
		{4, "async function value"},
	}
	if len(unsupported.Nodes) != len(want) {
		t.Fatalf("got %d unsupported nodes, want %d: %+v", len(unsupported.Nodes), len(want), unsupported.Nodes)
	}
	for i, w := range want {
		if got := unsupported.Nodes[i]; got.Span.Line != w.line || !strings.Contains(got.What, "cannot emit "+w.what+" yet") {
			t.Errorf("node %d = %+v, want %q on line %d", i, got, w.what, w.line)
		}
	}
}
