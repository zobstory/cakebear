package ir

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// twoModules is a program in which main.ts refers into lib.ts: a call to its
// function, a read of its const and a write to its let.
func twoModules() *Program {
	lib := &Module{
		File:  "/p/lib.ts",
		Funcs: []*Func{{Name: "helper", Result: Number, Body: []Stmt{&Return{Value: &NumberLit{Value: 1}}}}},
		Main: []Stmt{
			&VarDecl{Name: "limit", Type: Number, Init: &NumberLit{Value: 3}},
			&VarDecl{Name: "count", Type: Number, Init: &NumberLit{Value: 0}, Mutable: true},
		},
	}
	main := &Module{
		File: "/p/main.ts",
		Main: []Stmt{
			&Assign{Name: "count", Module: "/p/lib.ts", Value: &Call{Callee: "helper", Module: "/p/lib.ts", Typ: Number}},
			&ExprStmt{Expr: &ConsoleLog{Arg: &Ident{Name: "limit", Module: "/p/lib.ts", Typ: Number}}},
		},
	}
	return &Program{Modules: []*Module{lib, main}, Entry: 1}
}

func TestProgramOf(t *testing.T) {
	t.Parallel()

	m := &Module{File: "/p/main.ts"}
	p := ProgramOf(m)
	if p.EntryModule() != m || p.Module("/p/main.ts") != m || p.Module("/p/other.ts") != nil {
		t.Errorf("ProgramOf(m): entry %v, Module(main) %v", p.EntryModule(), p.Module("/p/main.ts"))
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	// A module alone needs no file.
	if err := ProgramOf(&Module{}).Validate(); err != nil {
		t.Errorf("Validate of a module with no file: %v", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	if err := twoModules().Validate(); err != nil {
		t.Fatalf("Validate of a valid program: %v", err)
	}

	tests := []struct {
		name   string
		change func(p *Program)
		want   string
	}{
		{"no modules", func(p *Program) { p.Modules = nil }, "no modules"},
		{"entry out of range", func(p *Program) { p.Entry = 2 }, "the entry is module 2 of 2"},
		{"out of order", func(p *Program) { p.Modules[0], p.Modules[1] = p.Modules[1], p.Modules[0] }, "out of order"},
		{"the same file twice", func(p *Program) { p.Modules[1].File = "/p/lib.ts" }, "out of order, or the same file twice"},
		{"a module without a file among several", func(p *Program) { p.Modules[0].File = "" }, "module 0 of 2 has no file"},
		{"into a module not in the program", func(p *Program) {
			p.Modules[1].Main[1].(*ExprStmt).Expr.(*ConsoleLog).Arg.(*Ident).Module = "/p/gone.ts"
		}, "limit refers into /p/gone.ts, which is not one of the program's modules"},
		{"a name the module doesn't declare", func(p *Program) {
			p.Modules[1].Main[0].(*Assign).Value.(*Call).Callee = "missing"
		}, "declares no missing"},
		{"assignment to a const", func(p *Program) { p.Modules[1].Main[0].(*Assign).Name = "limit" }, "assignment to limit, a const of /p/lib.ts"},
		{"assignment to a function", func(p *Program) { p.Modules[1].Main[0].(*Assign).Name = "helper" }, "assignment to helper, a function of /p/lib.ts"},
		{"a block's variable isn't top level", func(p *Program) {
			lib := p.Modules[0]
			lib.Main = []Stmt{&If{Cond: &BoolLit{Value: true}, Then: lib.Main}}
		}, "declares no count"},
		{"a capture, inside a literal inside a call", func(p *Program) {
			lit := &FuncLit{Captures: []Capture{{Name: "nothing", Module: "/p/lib.ts", Type: Number}}}
			p.Modules[1].Main = append(p.Modules[1].Main, &ExprStmt{Expr: &Call{Callee: "run", Args: []Expr{lit}}})
		}, "declares no nothing"},
	}
	for _, tt := range tests {
		p := twoModules()
		tt.change(p)
		err := p.Validate()
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want an error containing %q", tt.name, err, tt.want)
		}
	}
}

// everyNode is a module holding every statement and expression type, each
// nested inside another where it can be.
func everyNode() *Module {
	lit := &FuncLit{Body: []Stmt{
		&If{
			Cond: &Unary{Op: OpNot, Operand: &BoolLit{Value: true}},
			Then: []Stmt{&While{Cond: &BoolLit{}, Body: []Stmt{&Return{}}}},
			Else: []Stmt{&ExprStmt{Expr: &Await{Value: &NewPromise{Executor: &FuncLit{}}}}},
		},
	}}
	return &Module{
		Funcs: []*Func{{Name: "f", Body: []Stmt{&Return{Value: &Convert{Value: &NumberLit{Value: 1}}}}}},
		Main: []Stmt{
			&VarDecl{Name: "s", Init: &Binary{Op: OpAdd, Left: &StringLit{}, Right: &Ident{Name: "x"}}},
			&Assign{Name: "s", Value: &Call{Callee: "g", Args: []Expr{lit, &NullLit{}, &UndefinedLit{}}}},
			&ExprStmt{Expr: &ConsoleLog{Arg: &HostProp{Prop: HostURL, Recv: &HostCall{Op: HostCreateServer}}}},
		},
	}
}

func TestInspectVisitsEveryNode(t *testing.T) {
	t.Parallel()

	var seen []string
	Inspect(everyNode(), func(node any) { seen = append(seen, fmt.Sprintf("%T", node)) })

	for _, want := range []string{
		"*ir.VarDecl", "*ir.Assign", "*ir.If", "*ir.While", "*ir.Return", "*ir.ExprStmt",
		"*ir.NumberLit", "*ir.StringLit", "*ir.BoolLit", "*ir.NullLit", "*ir.UndefinedLit",
		"*ir.Ident", "*ir.Binary", "*ir.Unary", "*ir.Call", "*ir.FuncLit", "*ir.Await",
		"*ir.NewPromise", "*ir.ConsoleLog", "*ir.Convert", "*ir.HostCall", "*ir.HostProp",
	} {
		if !slices.Contains(seen, want) {
			t.Errorf("Inspect never visited a %s", want)
		}
	}
	// Each before its children, functions first: f's Return, then its Convert.
	if len(seen) < 3 || seen[0] != "*ir.Return" || seen[1] != "*ir.Convert" || seen[2] != "*ir.NumberLit" {
		t.Errorf("visit order starts %q, want the function's Return, Convert, NumberLit", seen[:min(3, len(seen))])
	}
}

// strayStmt is a statement type Inspect has no case for.
type strayStmt struct{}

func (strayStmt) isStmt()        {}
func (strayStmt) StmtSpan() Span { return Span{} }

func TestInspectPanicsOnAnUnknownNode(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "no case for statement ir.strayStmt") {
			t.Errorf("recovered %v, want Inspect's panic naming the type", r)
		}
	}()
	Inspect(&Module{Main: []Stmt{strayStmt{}}}, func(any) {})
}

// TestEvaluationOrder: ES order on the graph TestModulesEvaluateInESOrder
// (backend) runs, whose order was measured on Node 24.18: b, a, c, main. A
// module no evaluating import reaches is not in it.
func TestEvaluationOrder(t *testing.T) {
	t.Parallel()

	p := &Program{Entry: 3, Modules: []*Module{
		{File: "/p/a.ts", Imports: []string{"/p/b.ts"}},
		{File: "/p/b.ts", Imports: []string{"/p/a.ts"}},
		{File: "/p/c.ts", Imports: []string{"/p/b.ts"}},
		{File: "/p/main.ts", Imports: []string{"/p/a.ts", "/p/c.ts"}},
		{File: "/p/types.ts"},
	}}
	got := fmt.Sprint(p.EvaluationOrder())
	if want := "[1 0 2 3]"; got != want {
		t.Errorf("EvaluationOrder = %s, want %s (b, a, c, main)", got, want)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	p.Modules[0].Imports = append(p.Modules[0].Imports, "/p/gone.ts")
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "/p/a.ts imports /p/gone.ts, which is not one of the program's modules") {
		t.Errorf("Validate with an import outside the program = %v", err)
	}
}
