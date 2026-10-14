package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

func TestInnerCall(t *testing.T) {
	t.Parallel()

	a := &ir.Ident{Name: "a", Typ: ir.Number}
	f := &ir.Call{Callee: "f", Typ: ir.Number}
	sum := func(l, r ir.Expr) ir.Expr { return &ir.Binary{Op: ir.OpAdd, Left: l, Right: r, Typ: ir.Number} }
	for _, tt := range []struct {
		name string
		x    ir.Expr
		want bool
	}{
		{"a read alone", a, false},
		{"a call alone", f, false},
		{"a read beside a call", sum(a, f), true},
		{"a call's own argument", &ir.Call{Callee: "g", Args: []ir.Expr{a}}, false},
		{"a call beside an argument", &ir.Call{Callee: "g", Args: []ir.Expr{a, f}}, true},
		{"a call inside a literal's body is not evaluated", &ir.Call{Callee: "g", Args: []ir.Expr{a, &ir.FuncLit{Body: []ir.Stmt{&ir.ExprStmt{Expr: f}}}}}, false},
		{"an await", sum(a, &ir.Await{Value: a}), true},
		{"a host property", sum(a, &ir.HostProp{Prop: ir.HostURL, Recv: a}), true},
		{"console.log of a sum with a call", &ir.ConsoleLog{Arg: sum(a, f)}, true},
		{"console.log of a read", &ir.ConsoleLog{Arg: a}, false},
	} {
		if got := innerCall(tt.x); got != tt.want {
			t.Errorf("%s: innerCall = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestOrderedReads: in an expression with a call below its root, a read of a
// binding code writes to goes through ts_read, and a stable one doesn't; with
// no such call, nothing does.
func TestOrderedReads(t *testing.T) {
	t.Parallel()

	written := &ir.Ident{Name: "a", Typ: ir.Number}
	stable := &ir.Ident{Name: "k", Stable: true, Typ: ir.Number}
	f := &ir.Call{Callee: "f", Typ: ir.Number}
	sum := func(l, r ir.Expr) ir.Expr { return &ir.Binary{Op: ir.OpAdd, Left: l, Right: r, Typ: ir.Number} }
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "f", Result: ir.Number, Body: []ir.Stmt{&ir.Return{Value: &ir.NumberLit{Value: 1}}}}},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "a", Type: ir.Number, Init: &ir.NumberLit{Value: 1}, Mutable: true},
			&ir.VarDecl{Name: "k", Type: ir.Number, Init: &ir.NumberLit{Value: 2}},
			logNum(sum(sum(written, stable), f)),
			logNum(written),
		}}
	got := Emit(ir.ProgramOf(m))
	for _, want := range []string{
		"cakebearrt.LogNumber(((ts_read(a) + k) + f(ts_domain)))",
		"cakebearrt.LogNumber(a)\n",
		"func ts_read[T any](v T) T { return v }",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted Go lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ts_read(k)") {
		t.Errorf("a stable read was ordered:\n%s", got)
	}
}
