package backend

import (
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
	twoModules := &ir.Program{Modules: []*ir.Module{{File: "/p/a.ts"}, {File: "/p/b.ts"}}}

	for _, tt := range []struct {
		name string
		p    *ir.Program
		want string
	}{
		{"a reference into a module not in the program", intoNowhere, "invalid IR, a bug in lowering: "},
		{"two modules", twoModules, "this program has 2"},
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
