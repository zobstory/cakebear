package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

func TestGoTypeForHostTypes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   ir.Type
		want string
	}{
		{ir.HostOf(ir.HostHTTPServer), "*" + rtPkg + ".Server"},
		{ir.HostOf(ir.HostIncomingMessage), "*" + rtPkg + ".IncomingMessage"},
		{ir.HostOf(ir.HostServerResponse), "*" + rtPkg + ".ServerResponse"},
		{
			ir.FuncType([]ir.Type{ir.HostOf(ir.HostIncomingMessage), ir.HostOf(ir.HostServerResponse)}, ir.Void),
			"func(*" + rtPkg + ".IncomingMessage, *" + rtPkg + ".ServerResponse)",
		},
	} {
		if got := goType(tt.in); got != tt.want {
			t.Errorf("goType(%s) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// helloModule is the IR lower/ produces for the M1 acceptance program.
func helloModule() *ir.Module {
	span := ir.Span{File: "hello.ts", Line: 1, Col: 1}
	server, req, res := ir.HostOf(ir.HostHTTPServer), ir.HostOf(ir.HostIncomingMessage), ir.HostOf(ir.HostServerResponse)
	handlerType := ir.FuncType([]ir.Type{req, res}, ir.Void)
	resIdent := &ir.Ident{Name: "res", Typ: res, Span: span}
	str := func(s string) ir.Expr { return &ir.StringLit{Value: s, Span: span} }

	handler := &ir.FuncLit{
		Params: []ir.Param{{Name: "req", Type: req, Span: span}, {Name: "res", Type: res, Span: span}},
		Result: ir.Void, Typ: handlerType, Span: span,
		Body: []ir.Stmt{
			&ir.ExprStmt{Span: span, Expr: &ir.HostCall{
				Op: ir.HostEnd, Typ: res, Span: span, Args: []ir.Expr{str("c")},
				Recv: &ir.HostCall{Op: ir.HostSetHeader, Recv: resIdent, Args: []ir.Expr{str("a"), str("b")}, Typ: res, Span: span},
			}},
			&ir.If{Span: span, Cond: &ir.Binary{
				Op: ir.OpEqual, Typ: ir.Boolean, Span: span, Right: str("/x"),
				Left: &ir.HostProp{Prop: ir.HostURL, Recv: &ir.Ident{Name: "req", Typ: req, Span: span}, Typ: ir.String, Span: span},
			}, Then: []ir.Stmt{&ir.ExprStmt{Span: span, Expr: &ir.HostCall{Op: ir.HostWriteHead, Recv: resIdent, Args: []ir.Expr{&ir.NumberLit{Value: 404, Span: span}}, Typ: res, Span: span}}}},
		},
	}
	return &ir.Module{Name: "hello", Main: []ir.Stmt{
		&ir.VarDecl{Name: "server", Type: server, Span: span, Init: &ir.HostCall{Op: ir.HostCreateServer, Args: []ir.Expr{handler}, Typ: server, Span: span}},
		&ir.ExprStmt{Span: span, Expr: &ir.HostCall{
			Op: ir.HostListen, Recv: &ir.Ident{Name: "server", Typ: server, Span: span}, Typ: server, Span: span,
			Args: []ir.Expr{&ir.NumberLit{Value: 3000, Span: span}, &ir.FuncLit{Result: ir.Void, Typ: ir.FuncType(nil, ir.Void), Span: span}},
		}},
	}}
}

func TestEmitHostServer(t *testing.T) {
	t.Parallel()

	got := Emit(helloModule())
	mustParse(t, got)
	for _, want := range []string{
		"import " + rtPkg + " ",
		"var server *" + rtPkg + ".Server\n",
		"\tserver = " + rtPkg + ".CreateServer(func(req *" + rtPkg + ".IncomingMessage, res *" + rtPkg + ".ServerResponse) {\n",
		"\t\tres.SetHeader(\"a\", \"b\").End(\"c\")\n", // a chained call, emitted bare
		"\t\tif (req.URL() == \"/x\") {\n",
		"\t\t\tres.WriteHead(404.0)\n",
		"\tserver.Listen(3000.0, func() {\n",
		"\t" + rtPkg + ".Run()\n}\n", // the last statement of main
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}

// Run keeps the process alive while a server listens. A program without one
// must not have it, if only to keep generated code readable.
func TestEmitRunOnlyWhenNodeHTTPIsUsed(t *testing.T) {
	t.Parallel()

	span := ir.Span{File: "t.ts", Line: 1, Col: 1}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{&ir.ExprStmt{Span: span, Expr: &ir.ConsoleLog{Arg: &ir.NumberLit{Value: 1, Span: span}, Span: span}}}}
	if got := Emit(m); strings.Contains(got, "Run()") {
		t.Errorf("a program without a server calls Run:\n%s", got)
	}
}

// A host type in a signature names the runtime package even when nothing is
// called, and Go rejects the missing import as surely as an unused one.
func TestEmitImportsRuntimeForAHostTypeInASignature(t *testing.T) {
	t.Parallel()

	span := ir.Span{File: "t.ts", Line: 1, Col: 1}
	m := &ir.Module{Name: "t", Funcs: []*ir.Func{{
		Name: "handle", Result: ir.Void, Span: span,
		Params: []ir.Param{{Name: "res", Type: ir.HostOf(ir.HostServerResponse), Span: span}},
	}}}
	got := Emit(m)
	mustParse(t, got)
	if !strings.Contains(got, "import "+rtPkg+" ") || !strings.Contains(got, "func handle(res *"+rtPkg+".ServerResponse)") {
		t.Errorf("host-typed parameter without the runtime import:\n%s", got)
	}
}

// Top-level variables live at package level, so a top-level function can use
// them, and are assigned in main at their place in the source, so side effects
// keep their order.
func TestEmitHoistsTopLevelVariables(t *testing.T) {
	t.Parallel()

	span := ir.Span{File: "t.ts", Line: 1, Col: 1}
	m := &ir.Module{
		Name: "t",
		Funcs: []*ir.Func{{Name: "show", Result: ir.Void, Span: span, Body: []ir.Stmt{&ir.ExprStmt{Span: span,
			Expr: &ir.ConsoleLog{Arg: &ir.Ident{Name: "n", Typ: ir.Number, Span: span}, Span: span}}}}},
		Main: []ir.Stmt{
			&ir.ExprStmt{Span: span, Expr: &ir.Call{Callee: "show", Typ: ir.Void, Span: span}},
			&ir.VarDecl{Name: "n", Type: ir.Number, Mutable: true, Init: &ir.NumberLit{Value: 7, Span: span}, Span: span},
		},
	}
	got := Emit(m)
	mustParse(t, got)
	if !strings.Contains(got, "\nvar n float64\n") || strings.Contains(got, "var n float64 =") {
		t.Errorf("n should be declared at package level without an initialiser:\n%s", got)
	}
	if !strings.Contains(got, "\tshow()\n\tn = 7.0\n") {
		t.Errorf("n should be assigned in main after the call that precedes it:\n%s", got)
	}
}
