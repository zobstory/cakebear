package backend

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

func TestBuildRefusesWhatItCannotEmit(t *testing.T) {
	t.Parallel()

	intoNowhere := ir.ProgramOf(&ir.Module{File: "/p/main.ts", Main: []ir.Stmt{
		&ir.ExprStmt{Expr: &ir.Call{Callee: "helper", Module: "/p/lib.ts", Typ: ir.Void}},
	}})
	awaitsOutsideTheEntry := &ir.Program{Modules: []*ir.Module{{File: "/p/a.ts", Async: true}, {File: "/p/main.ts"}}, Entry: 1}

	for _, tt := range []struct {
		name string
		p    *ir.Program
		want string
	}{
		{"a reference into a module not in the program", intoNowhere, "invalid IR, a bug in lowering: "},
		{"a top-level await outside the entry", awaitsOutsideTheEntry, "/p/a.ts has a top-level await, which only the entry module may have so far"},
	} {
		_, err := Build(tt.p, Options{Output: filepath.Join(t.TempDir(), "out")})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Build = %v, want an error containing %q", tt.name, err, tt.want)
		}
		if got := Emit(tt.p); !strings.HasPrefix(got, "#error ") || !strings.Contains(got, tt.want) {
			t.Errorf("%s: Emit = %q, want an #error line containing %q", tt.name, got, tt.want)
		}
	}
}

// TestQualifiersLeaveOneModuleUnchanged: references qualified with the
// module's own file emit exactly as the unqualified ones did, so a program of
// one module compiles to the same Go as before references named modules.
func TestQualifiersLeaveOneModuleUnchanged(t *testing.T) {
	t.Parallel()

	module := func(file string) *ir.Module {
		return &ir.Module{
			Name: "main",
			File: "/p/main.ts",
			Funcs: []*ir.Func{{
				Name: "bump", Result: ir.Number,
				Body: []ir.Stmt{
					&ir.Assign{Name: "count", Module: file, Value: &ir.Binary{
						Op: ir.OpAdd, Left: &ir.Ident{Name: "count", Module: file, Typ: ir.Number}, Right: &ir.NumberLit{Value: 1}, Typ: ir.Number,
					}},
					&ir.Return{Value: &ir.Ident{Name: "count", Module: file, Typ: ir.Number}},
				},
			}},
			Main: []ir.Stmt{
				&ir.VarDecl{Name: "count", Type: ir.Number, Init: &ir.NumberLit{Value: 0}, Mutable: true},
				&ir.ExprStmt{Expr: &ir.ConsoleLog{Arg: &ir.Call{Callee: "bump", Module: file, Typ: ir.Number}}},
			},
		}
	}

	bare, qualified := Emit(ir.ProgramOf(module(""))), Emit(ir.ProgramOf(module("/p/main.ts")))
	if strings.Contains(qualified, "#error") || !strings.Contains(qualified, "func bump(") {
		t.Fatalf("the qualified program doesn't emit:\n%s", qualified)
	}
	if qualified != bare {
		t.Errorf("qualifying references changed the Go:\n--- bare\n%s\n--- qualified\n%s", bare, qualified)
	}
}

// buildAndRun builds p and returns what the binary prints.
func buildAndRun(t *testing.T, p *ir.Program) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "prog")
	src, err := Build(p, Options{Output: out})
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, src)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running the binary: %v", err)
	}
	return string(got)
}

// call calls name, declared at the top level of module.
func call(name, module string) *ir.Call {
	return &ir.Call{Callee: name, Module: module, Typ: ir.Number}
}

func returns(e ir.Expr) []ir.Stmt { return []ir.Stmt{&ir.Return{Value: e}} }

// TestModulesEvaluateInESOrder (M3 check 5): side effects at every level of a
// graph with a cycle, main → a → b → a and main → c → b, print in the order
// Node 24.18 printed them for the same graph: b, a, c, main, then 3.
func TestModulesEvaluateInESOrder(t *testing.T) {
	t.Parallel()

	const a, b, c, main = "/p/a.ts", "/p/b.ts", "/p/c.ts", "/p/main.ts"
	p := &ir.Program{Entry: 3, Modules: []*ir.Module{
		{File: a, Imports: []string{b}, Main: []ir.Stmt{logStr("a")},
			Funcs: []*ir.Func{{Name: "a", Result: ir.Number, Body: returns(call("b", b))}}},
		{File: b, Imports: []string{a}, Main: []ir.Stmt{logStr("b")}, Funcs: []*ir.Func{
			{Name: "b", Result: ir.Number, Body: returns(num(1))},
			{Name: "callsA", Result: ir.Number, Body: returns(call("a", a))},
		}},
		{File: c, Imports: []string{b}, Main: []ir.Stmt{logStr("c")},
			Funcs: []*ir.Func{{Name: "c", Result: ir.Number, Body: returns(&ir.Binary{Op: ir.OpAdd, Left: call("b", b), Right: num(1), Typ: ir.Number})}}},
		{Name: "main", File: main, Imports: []string{a, c}, Main: []ir.Stmt{
			logStr("main"),
			logNum(&ir.Binary{Op: ir.OpAdd, Left: call("a", a), Right: call("c", c), Typ: ir.Number}),
		}},
	}}
	if got, want := buildAndRun(t, p), "b\na\nc\nmain\n3\n"; got != want {
		t.Errorf("the program printed %q, want Node's %q", got, want)
	}
}

// TestTwoModulesEachWithHelper (M3 check 6): two modules each declare a
// top-level `helper` and `n`, and a function of main has a parameter named
// helper too. All of it compiles, and each name means its own binding.
func TestTwoModulesEachWithHelper(t *testing.T) {
	t.Parallel()

	const x, y, main = "/p/x.ts", "/p/y.ts", "/p/main.ts"
	ident := func(name, module string) *ir.Ident { return &ir.Ident{Name: name, Module: module, Typ: ir.Number} }
	p := &ir.Program{Entry: 0, Modules: []*ir.Module{
		{Name: "main", File: main, Imports: []string{x, y},
			Funcs: []*ir.Func{{Name: "f", Params: []ir.Param{{Name: "helper", Type: ir.Number}}, Result: ir.Number,
				Body: returns(&ir.Binary{Op: ir.OpAdd, Left: ident("helper", ""), Right: call("helper", x), Typ: ir.Number})}},
			Main: []ir.Stmt{logNum(call("helper", x)), logNum(call("helper", y)), logNum(&ir.Call{Callee: "f", Module: main, Args: []ir.Expr{num(100)}, Typ: ir.Number})},
		},
		{File: x, Main: []ir.Stmt{&ir.VarDecl{Name: "n", Type: ir.Number, Init: num(1), Mutable: true}},
			Funcs: []*ir.Func{{Name: "helper", Result: ir.Number, Body: []ir.Stmt{
				&ir.Assign{Name: "n", Module: x, Value: &ir.Binary{Op: ir.OpAdd, Left: ident("n", x), Right: num(1), Typ: ir.Number}},
				&ir.Return{Value: ident("n", x)},
			}}}},
		{File: y, Main: []ir.Stmt{&ir.VarDecl{Name: "n", Type: ir.Number, Init: num(10), Mutable: true}},
			Funcs: []*ir.Func{{Name: "helper", Result: ir.Number, Body: returns(ident("n", y))}}},
	}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, want := buildAndRun(t, p), "2\n10\n103\n"; got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestAFunctionNamedInit: Go runs a package-level func init itself and
// allows it no parameters, and each module's top-level code is a function
// named ts_m<i>_init, so a user's own `init` must be renamed, alone or in a
// module of several.
func TestAFunctionNamedInit(t *testing.T) {
	t.Parallel()

	initFn := func() *ir.Func { return &ir.Func{Name: "init", Result: ir.Number, Body: returns(num(7))} }
	alone := ir.ProgramOf(&ir.Module{Name: "main", Funcs: []*ir.Func{initFn()}, Main: []ir.Stmt{logNum(call("init", ""))}})
	if got := buildAndRun(t, alone); got != "7\n" {
		t.Errorf("one module: printed %q, want %q", got, "7\n")
	}

	const lib, main = "/p/lib.ts", "/p/main.ts"
	several := &ir.Program{Entry: 1, Modules: []*ir.Module{
		{File: lib, Funcs: []*ir.Func{initFn()}, Main: []ir.Stmt{logStr("lib")}},
		{Name: "main", File: main, Imports: []string{lib}, Main: []ir.Stmt{logNum(call("init", lib))}},
	}}
	if got := buildAndRun(t, several); got != "lib\n7\n" {
		t.Errorf("two modules: printed %q, want %q", got, "lib\n7\n")
	}
}
