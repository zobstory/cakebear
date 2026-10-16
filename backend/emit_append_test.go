package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestEmitAppends: an Appended binding has its accumulator beside it, in its
// own scope (a local, a top-level let at package level, a parameter at the
// top of its function's body); an append goes through it; `.length` asks it;
// and a chain of string `+` is one StrConcat.
func TestEmitAppends(t *testing.T) {
	t.Parallel()

	marked := func(name string) *ir.Ident {
		return &ir.Ident{Name: name, Typ: ir.String, Appended: true, Span: span}
	}
	length := func(name string) ir.Expr {
		return &ir.BuiltinCall{Fn: ir.StrLength, Args: []ir.Expr{marked(name)}, Typ: ir.Number, Span: span}
	}
	add := func(a, b ir.Expr) ir.Expr {
		return &ir.Binary{Op: ir.OpAdd, Left: a, Right: b, Typ: ir.String, Span: span}
	}
	fn := &ir.Func{
		Name: "f", Params: []ir.Param{{Name: "p", Type: ir.String, Appended: true, Span: span}}, Result: ir.Void, Span: span,
		Body: []ir.Stmt{
			&ir.VarDecl{Name: "s", Type: ir.String, Init: str(""), Mutable: true, Appended: true, Span: span},
			&ir.Append{Target: marked("s"), Parts: []ir.Expr{str("a"), marked("p")}, Span: span},
			&ir.Append{Target: marked("p"), Parts: []ir.Expr{str("!")}, Span: span},
			logNum(length("s")),
		},
	}
	m := &ir.Module{Name: "t", Funcs: []*ir.Func{fn}, Main: []ir.Stmt{
		&ir.VarDecl{Name: "top", Type: ir.String, Init: str(""), Mutable: true, Appended: true, Span: span},
		&ir.Append{Target: marked("top"), Parts: []ir.Expr{str("x")}, Span: span},
		&ir.ExprStmt{Expr: &ir.ConsoleLog{Args: []ir.Expr{add(add(str("a"), str("b")), str("c"))}, Span: span}, Span: span},
	}}
	got := Emit(ir.ProgramOf(m))
	for _, want := range []string{
		"var top string\nvar ts_acc_top cakebearrt.StrAcc\n",
		"{\n\tvar ts_acc_p cakebearrt.StrAcc\n",
		"\tvar s string = \"\"\n\t_ = s\n\tvar ts_acc_s cakebearrt.StrAcc\n",
		"\ts = ts_acc_s.Append(s, \"a\", p)\n",
		"\tp = ts_acc_p.Append(p, \"!\")\n",
		"ts_acc_s.Length(ts_read(s))", // inside a call, the read is ordered
		"top = ts_acc_top.Append(top, \"x\")",
		"cakebearrt.StrConcat(\"a\", \"b\", \"c\")",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in\n%s", want, got)
		}
	}
}

// TestEmitAppendReadsItsTargetFirst: in `s += f()`, s is read before f runs,
// as JavaScript reads the left of += first, so with a call among the pieces
// the read goes through ts_read (order.go).
func TestEmitAppendReadsItsTargetFirst(t *testing.T) {
	t.Parallel()

	target := &ir.Ident{Name: "s", Typ: ir.String, Appended: true, Span: span}
	call := &ir.Call{Callee: "f", Typ: ir.String, Span: span}
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "f", Result: ir.String, Body: []ir.Stmt{&ir.Return{Value: str("f"), Span: span}}, Span: span}},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "s", Type: ir.String, Init: str(""), Mutable: true, Appended: true, Span: span},
			&ir.Append{Target: target, Parts: []ir.Expr{call}, Span: span},
		}}
	if got := Emit(ir.ProgramOf(m)); !strings.Contains(got, "s = ts_acc_s.Append(ts_read(s), f(") {
		t.Errorf("the append doesn't read s through ts_read:\n%s", got)
	}
}
