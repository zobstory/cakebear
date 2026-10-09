package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

func lowerClean(t *testing.T, src string) *ir.Module {
	t.Helper()
	m, errs := lowerSource(t, src)
	if len(errs) > 0 {
		t.Fatalf("unexpected lowering errors: %v", errs)
	}
	return m
}

// argLit returns the function literal passed as a call's first argument, where
// the call is the expression of the i'th top-level statement.
func argLit(t *testing.T, m *ir.Module, i int) *ir.FuncLit {
	t.Helper()
	stmt, ok := m.Main[i].(*ir.ExprStmt)
	if !ok {
		t.Fatalf("statement %d is %T, want *ir.ExprStmt", i, m.Main[i])
	}
	call, ok := stmt.Expr.(*ir.Call)
	if !ok || len(call.Args) == 0 {
		t.Fatalf("statement %d is not a call with arguments: %#v", i, stmt.Expr)
	}
	lit, ok := call.Args[0].(*ir.FuncLit)
	if !ok {
		t.Fatalf("first argument is %T, want *ir.FuncLit", call.Args[0])
	}
	return lit
}

func TestLowerArrowFunctionValue(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, "const add = (a: number, b: number): number => a + b;\n")
	decl := m.Main[0].(*ir.VarDecl)
	want := ir.FuncType([]ir.Type{ir.Number, ir.Number}, ir.Number)
	if !decl.Type.Equal(want) {
		t.Errorf("add has type %s, want %s", decl.Type, want)
	}
	lit, ok := decl.Init.(*ir.FuncLit)
	if !ok {
		t.Fatalf("initialiser is %T, want *ir.FuncLit", decl.Init)
	}
	if len(lit.Params) != 2 || lit.Params[0].Name != "a" || lit.Params[1].Name != "b" || lit.Result != ir.Number {
		t.Errorf("literal = %+v, want (a, b) => number", lit)
	}
	if len(lit.Body) != 1 {
		t.Fatalf("concise body lowered to %d statements, want 1", len(lit.Body))
	}
	if _, ok := lit.Body[0].(*ir.Return); !ok {
		t.Errorf("concise body is %T, want *ir.Return", lit.Body[0])
	}
}

// An unannotated parameter takes its type from the callback it is passed to,
// and a function-typed parameter can be called by name.
func TestClosureParamTypesFromContext(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `
function apply(f: (x: number) => number, v: number): number {
  return f(v);
}
apply((x) => x * 2, 3);
`)
	if got := m.Funcs[0].Params[0].Type; !got.Equal(ir.FuncType([]ir.Type{ir.Number}, ir.Number)) {
		t.Errorf("apply's f has type %s, want (number) => number", got)
	}
	lit := argLit(t, m, 0)
	if len(lit.Params) != 1 || lit.Params[0].Name != "x" || lit.Params[0].Type != ir.Number {
		t.Errorf("literal params = %+v, want x: number from context", lit.Params)
	}
}

func TestClosureCapturesWithMutability(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `
const k: number = 2;
let total: number = 0;
function run(f: (x: number) => void): void {
  f(1);
}
run((x) => {
  const local: number = x * k;
  total = total + local;
  run((y) => {
    total = total + y + k;
  });
});
`)
	outer := argLit(t, m, 2)
	wantOuter := []ir.Capture{{Name: "k", Type: ir.Number}, {Name: "total", Type: ir.Number, Mutable: true}}
	if !equalCaptures(outer.Captures, wantOuter) {
		t.Errorf("outer captures = %+v, want %+v (not x, local, y or the function run)", outer.Captures, wantOuter)
	}

	nested := outer.Body[2].(*ir.ExprStmt).Expr.(*ir.Call).Args[0].(*ir.FuncLit)
	wantNested := []ir.Capture{{Name: "total", Type: ir.Number, Mutable: true}, {Name: "k", Type: ir.Number}}
	if !equalCaptures(nested.Captures, wantNested) {
		t.Errorf("nested captures = %+v, want %+v", nested.Captures, wantNested)
	}
}

// A parameter of the enclosing function is captured as mutable, since
// TypeScript lets a parameter be reassigned; a shadowing parameter is not a
// capture at all.
func TestClosureCapturesParametersNotShadows(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `
function adder(n: number): (x: number) => number {
  return (x) => x + n;
}
const v: number = 1;
const inc = (v: number): number => v + 1;
`)
	lit := m.Funcs[0].Body[0].(*ir.Return).Value.(*ir.FuncLit)
	if want := []ir.Capture{{Name: "n", Type: ir.Number, Mutable: true}}; !equalCaptures(lit.Captures, want) {
		t.Errorf("adder's literal captures = %+v, want %+v", lit.Captures, want)
	}
	if c := m.Main[1].(*ir.VarDecl).Init.(*ir.FuncLit).Captures; len(c) != 0 {
		t.Errorf("a parameter shadowing v was recorded as capturing it: %+v", c)
	}
}

// TypeScript lets a callback declare fewer parameters than its type passes.
// Go needs the exact signature, so the missing ones are emitted unnamed.
func TestClosureFitsSlotArity(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `
function each(f: (x: number, i: number) => void): void {
  f(1, 0);
}
each(() => console.log("tick"));
each((x) => console.log(x));
`)
	none := argLit(t, m, 0)
	if len(none.Params) != 2 || none.Params[0].Name != "" || none.Params[1].Name != "" {
		t.Errorf("() => … params = %+v, want two unnamed number params", none.Params)
	}
	one := argLit(t, m, 1)
	if len(one.Params) != 2 || one.Params[0].Name != "x" || one.Params[1].Name != "" {
		t.Errorf("(x) => … params = %+v, want x then one unnamed", one.Params)
	}
	if !one.Typ.Equal(ir.FuncType([]ir.Type{ir.Number, ir.Number}, ir.Void)) {
		t.Errorf("literal type = %s, want the slot's (number, number) => void", one.Typ)
	}
}

// A literal returning a value where the slot expects void has the value
// evaluated and dropped, in both a concise and a block body.
func TestClosureDiscardsResultForVoidSlot(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `
function each(f: (x: number) => void): void {
  f(1);
}
each((x) => x * 2);
each((x) => {
  return x * 3;
});
`)
	concise := argLit(t, m, 0)
	if concise.Result != ir.Void || len(concise.Body) != 1 {
		t.Fatalf("concise literal = %+v, want a void result and one statement", concise)
	}
	if _, ok := concise.Body[0].(*ir.ExprStmt); !ok {
		t.Errorf("concise body is %T, want *ir.ExprStmt", concise.Body[0])
	}

	block := argLit(t, m, 1)
	if len(block.Body) != 2 {
		t.Fatalf("block body has %d statements, want ExprStmt then bare Return", len(block.Body))
	}
	if _, ok := block.Body[0].(*ir.ExprStmt); !ok {
		t.Errorf("first statement is %T, want *ir.ExprStmt", block.Body[0])
	}
	if r, ok := block.Body[1].(*ir.Return); !ok || r.Value != nil {
		t.Errorf("second statement = %#v, want a bare return", block.Body[1])
	}
}

// Each refusal names the construct, and reads as a limit of this phase rather
// than a mistake in the source.
func TestClosureRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"async", "const f = async (): Promise<void> => {};\n", "async functions are not supported yet"},
		{"generator", "const g = function* (): Generator<number> { yield 1; };\n", "generator functions are not supported yet"},
		{"named", "const h = function named(): number { return 1; };\n", "named function expressions are not supported yet"},
		{"generic", "const id = <T,>(x: T): T => x;\n", "generic functions are not supported yet"},
		{"rest", "const r = (...xs: number[]): number => 1;\n", "rest parameters are not supported yet"},
		{"optional", "const o = (x?: number): number => 1;\n", "optional and default parameters are not supported yet"},
		{"default", "const d = (x: number = 1): number => x;\n", "optional and default parameters are not supported yet"},
		{"destructured", "const s = ({ a }: { a: number }): number => a;\n", "destructured parameters are not supported yet"},
		// A recursive function type must be refused, not recursed into forever.
		{"recursive type", "type F = (next: F) => void;\nconst loop: F = (next) => {};\n", "loop has a type the backend cannot represent yet"},
		{"param mismatch", "function run(f: (x: i32) => void): void { f(i32(1)); }\nrun((x: number) => { console.log(x); });\n",
			"parameter x is number here but the caller passes i32"},
		{"result mismatch", "function run(f: () => number): number { return f(); }\nrun((): i32 => i32(1));\n",
			"this function returns i32 where number is expected"},
		{"unrepresentable result", "function take(f: () => void): void { f(); }\ntake(() => null);\n",
			"this function returns a type the backend cannot represent yet"},
		{"unrepresentable param", "function take(f: (x: null) => void): void {}\ntake((x) => {});\n",
			"parameter x has a type the backend cannot represent yet"},
		{"call an expression", "const one: number = ((x: number): number => x)(1);\n", "only calls to a function by name are supported yet"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
			var msgs []string
			for _, e := range errs {
				msgs = append(msgs, e.Msg)
				if strings.Contains(e.Msg, tt.want) && e.Kind != Unsupported {
					t.Errorf("%q is reported as an error, want a limit of this phase", e.Msg)
				}
			}
			if !strings.Contains(strings.Join(msgs, "\n"), tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}

func equalCaptures(got, want []ir.Capture) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].Name != want[i].Name || !got[i].Type.Equal(want[i].Type) || got[i].Mutable != want[i].Mutable {
			return false
		}
	}
	return true
}
