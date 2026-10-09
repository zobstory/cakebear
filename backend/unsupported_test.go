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
func TestBuildRefusesIRItCannotEmit(t *testing.T) {
	t.Parallel()

	at := func(line int) ir.Span { return ir.Span{File: "t.ts", Line: line, Col: 1} }
	server := ir.HostOf(ir.HostHTTPServer)
	m := &ir.Module{
		Name: "t",
		Funcs: []*ir.Func{{
			Name: "f", Result: ir.Void, Span: at(1),
			Body: []ir.Stmt{&ir.ExprStmt{Span: at(2), Expr: &ir.HostProp{Prop: ir.HostURL, Typ: ir.String, Span: at(2)}}},
		}},
		Main: []ir.Stmt{&ir.VarDecl{
			Name: "s", Type: server, Span: at(1),
			Init: &ir.HostCall{Op: ir.HostCreateServer, Typ: server, Span: at(1)},
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
	if got := unsupported.Nodes[0]; got.Span.Line != 1 || !strings.Contains(got.What, "node:http's createServer") {
		t.Errorf("first = %+v, want createServer on line 1", got)
	}
	if got := unsupported.Nodes[1]; got.Span.Line != 2 || !strings.Contains(got.What, "node:http's IncomingMessage.url") {
		t.Errorf("second = %+v, want IncomingMessage.url on line 2", got)
	}
	if !strings.Contains(src, "#error") {
		t.Errorf("returned source should still show the placeholders for --emit-go:\n%s", src)
	}
}
