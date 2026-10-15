package backend

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

func binary(op ir.BinaryOp, left, right ir.Expr, t ir.Type) *ir.Binary {
	return &ir.Binary{Op: op, Left: left, Right: right, Typ: t, Span: span}
}

// TestEmitLoopsCompileAndRun: a for loop with two loop variables, continue
// and break, and functions that end in a loop TypeScript knows never exits,
// a `while (true)` that Go alone would call a missing return.
func TestEmitLoopsCompileAndRun(t *testing.T) {
	t.Parallel()

	i, j, n := ident("i", ir.Number), ident("j", ir.Number), ident("n", ir.Number)
	forever := &ir.BoolLit{Value: true, Span: span}
	// function first(n: number): number { let i = 0; while (true) { if (i * i > n) return i; i = i + 1; } }
	first := &ir.Func{
		Name:   "first",
		Params: []ir.Param{{Name: "n", Type: ir.Number, Span: span}},
		Result: ir.Number,
		Body: []ir.Stmt{
			&ir.VarDecl{Name: "i", Type: ir.Number, Init: num(0), Mutable: true, Span: span},
			&ir.While{Cond: forever, Body: []ir.Stmt{
				&ir.If{Cond: binary(ir.OpGreater, binary(ir.OpMul, i, i, ir.Number), n, ir.Boolean), Then: []ir.Stmt{&ir.Return{Value: i, Span: span}}, Span: span},
				&ir.Assign{Name: "i", Value: binary(ir.OpAdd, i, num(1), ir.Number), Span: span},
			}, Span: span},
		},
		Span: span,
	}
	// The same, as a literal: const second = (): number => { while (true) { return 7; } }
	secondType := ir.FuncType(nil, ir.Number)
	second := &ir.FuncLit{Result: ir.Number, Body: []ir.Stmt{
		&ir.While{Cond: forever, Body: []ir.Stmt{&ir.Return{Value: num(7), Span: span}}, Span: span},
	}, Span: span}

	m := &ir.Module{
		Name:  "t",
		Funcs: []*ir.Func{first},
		Main: []ir.Stmt{
			// for (let i = 0, j = 10; i < j; i = i + 1) { if (i === 1) continue; if (i === 4) break; console.log(i + j) }
			&ir.For{
				Init: []ir.Stmt{
					&ir.VarDecl{Name: "i", Type: ir.Number, Init: num(0), Mutable: true, Span: span},
					&ir.VarDecl{Name: "j", Type: ir.Number, Init: num(10), Mutable: true, Span: span},
				},
				Cond: binary(ir.OpLess, i, j, ir.Boolean),
				Post: []ir.Stmt{&ir.Assign{Name: "i", Value: binary(ir.OpAdd, i, num(1), ir.Number), Span: span}},
				Body: []ir.Stmt{
					&ir.If{Cond: binary(ir.OpEqual, i, num(1), ir.Boolean), Then: []ir.Stmt{&ir.Continue{Span: span}}, Span: span},
					&ir.If{Cond: binary(ir.OpEqual, i, num(4), ir.Boolean), Then: []ir.Stmt{&ir.Break{Span: span}}, Span: span},
					logNum(binary(ir.OpAdd, i, j, ir.Number)),
				},
				Span: span,
			},
			&ir.For{Body: []ir.Stmt{&ir.Break{Span: span}}, Span: span},
			logNum(&ir.Call{Callee: "first", Args: []ir.Expr{num(50)}, Typ: ir.Number, Span: span}),
			&ir.VarDecl{Name: "second", Type: secondType, Init: second, Span: span},
			logNum(&ir.Call{Callee: "second", Typ: ir.Number, Span: span}),
		},
	}

	out := filepath.Join(t.TempDir(), "prog")
	src, err := Build(ir.ProgramOf(m), Options{Output: out})
	if err != nil {
		t.Fatalf("emitted source did not compile: %v\n%s", err, src)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running built binary: %v", err)
	}
	if want := "10\n12\n13\n8\n7\n"; string(got) != want {
		t.Errorf("binary printed %q, want %q", got, want)
	}
}

// TestBodyEndsWithReturnNeedsNoPanic: the panic closes only a body Go can't
// see terminates.
func TestBodyEndsWithReturnNeedsNoPanic(t *testing.T) {
	t.Parallel()

	m := &ir.Module{Name: "t", Funcs: []*ir.Func{
		{Name: "one", Result: ir.Number, Body: []ir.Stmt{&ir.Return{Value: num(1), Span: span}}, Span: span},
		{Name: "quiet", Result: ir.Void, Body: []ir.Stmt{&ir.While{Cond: &ir.BoolLit{Span: span}, Span: span}}, Span: span},
	}}
	if got := Emit(ir.ProgramOf(m)); strings.Contains(got, "panic(") {
		t.Errorf("a body ending in a return, or with no result, got a panic:\n%s", got)
	}
}
