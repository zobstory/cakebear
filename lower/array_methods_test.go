package lower

import (
	"slices"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// TestArrayMethodLowering: each method lowers to its ArrayOp with the full
// callback signature, whatever the literal declares, and a named function
// with fewer parameters is adapted to it.
func TestArrayMethodLowering(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `const xs: number[] = [3, 1, 2];
function double(x: number): number {
  return x * 2;
}
const flags: boolean[] = xs.map((x) => x > 1);
const twice: number[] = xs.map(double);
const big: number[] = xs.filter((x, i) => x > i);
const any: boolean = xs.some((x) => x > 2);
const all: boolean = xs.every((x) => x > 0);
const at: number = xs.findIndex((x) => x === 1);
xs.forEach((x) => x * 2);
const sorted: number[] = xs.sort((a, b) => a - b);
const ok: boolean = xs.reduce((acc, x) => acc && x > 0, true);
const has: boolean = xs.includes(2);
const where: number = xs.indexOf(2);
const joined: string = xs.join("-");
const part: number[] = xs.slice(1);
`)
	ops := map[ir.ArrayMethod]*ir.ArrayOp{}
	ir.Inspect(p.EntryModule(), func(node any) {
		if op, ok := node.(*ir.ArrayOp); ok {
			ops[op.Method] = op
		}
	})
	if got := len(ops); got != 12 {
		t.Fatalf("%d methods lowered, want all 12: %v", got, ops)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := ops[ir.ArrayMap].Typ.String(); got != "number[]" {
		// The last map, of double: its adapter forwards one of three parameters.
		t.Errorf("map's type is %s, want number[]", got)
	}
	if cb := ops[ir.ArrayMap].Args[0].(*ir.FuncLit); len(cb.Params) != 3 || cb.Params[1].Name != "" {
		t.Errorf("double's adapter takes %v, want (value, index, array) using only value", cb.Params)
	}
	if got := ops[ir.ArrayReduce].Typ; got != ir.Boolean {
		t.Errorf("reduce's type is %s, want boolean, its initial value's", got)
	}
}

func TestArrayMethodRefusals(t *testing.T) {
	t.Parallel()

	const xs = "const xs: number[] = [1, 2];\n"
	for _, tt := range []struct {
		name, src, want string
	}{
		{"a member refused plainly", xs + "const r: number[] = xs.reverse();\n", "the array method reverse is not supported yet"},
		{"sort without a comparator", xs + "const s: number[] = xs.sort();\n", "sort without a comparator compares as strings"},
		{"reduce without an initial value", xs + "const s: number = xs.reduce((a, b) => a + b);\n", "reduce without an initial value is not supported yet"},
		{"filter testing truthiness", xs + "const f: number[] = xs.filter((x) => x);\n", "filter's callback must return a boolean; testing a number for truthiness is not supported yet"},
		{"map returning nothing", xs + "xs.map((x) => {\n  console.log(x);\n});\n", "map's callback must return a value; forEach is for its effects"},
		{"a thisArg", xs + "xs.forEach((x) => x, {});\n", "forEach's thisArg is not supported"},
		{"includes with fromIndex", xs + "const h: boolean = xs.includes(1, 1);\n", "includes's fromIndex is not supported yet"},
		{"indexOf on functions", "const fs: (() => void)[] = [];\nconst f = (): void => {};\nconst i: number = fs.indexOf(f);\n", "indexOf on an array of functions is not supported"},
		{"join of functions", "const fs: (() => void)[] = [];\nconst s: string = fs.join();\n", "join of an array of functions is not supported"},
		{"join of nested functions", "const fs: (() => void)[][] = [];\nconst s: string = fs.join();\n", "join of an array of functions is not supported"},
		{"join of objects with their own toString", "interface T { toString: () => string }\nconst ts: T[] = [{ toString: (): string => \"own\" }];\nconst s: string = ts.join();\n",
			"join of an array of objects with their own toString or valueOf is not supported: Node calls it"},
		{"a function value that isn't a name", "interface Box { f: (x: number) => number }\nconst b: Box = { f: (x: number): number => x };\n" + xs + "const ys: number[] = xs.map(b.f);\n",
			"can't be used as a (number, number, number[]) => number yet: write an arrow function in its place that calls it"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors = %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}

// TestFunctionValueAdaptedToItsSlot: a named function taking fewer
// parameters than the function type it fills, which TypeScript allows, is
// adapted wherever it flows, not only into a method: an argument, here.
func TestFunctionValueAdaptedToItsSlot(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `function apply(g: (a: number, b: number) => number): number {
  return g(1, 2);
}
function one(a: number): number {
  return a * 10;
}
const r: number = apply(one);
`)
	call := p.EntryModule().Main[0].(*ir.VarDecl).Init.(*ir.Call)
	lit, ok := call.Args[0].(*ir.FuncLit)
	if !ok || len(lit.Params) != 2 || !lit.Typ.Equal(ir.FuncType([]ir.Type{ir.Number, ir.Number}, ir.Number)) {
		t.Fatalf("apply's argument is %#v, want an adapter of apply's parameter type", call.Args[0])
	}
	// It passes one its first parameter, and ignores the second.
	inner := lit.Body[0].(*ir.Return).Value.(*ir.Call)
	if arg, ok := inner.Args[0].(*ir.Ident); inner.Callee != "one" || len(inner.Args) != 1 || !ok || arg.Name != lit.Params[0].Name || lit.Params[1].Name != "" {
		t.Errorf("the adapter calls %s with %v, want one with its first parameter alone", inner.Callee, inner.Args)
	}
}

// TestArrayMembersAreAccountedFor (the lib-drift test of D16): every member
// TypeScript's libs give Array and ReadonlyArray is lowered or refused by
// name, so a member a new lib adds is noticed, and every name the refusal
// table lists is still a member.
func TestArrayMembersAreAccountedFor(t *testing.T) {
	t.Parallel()

	program, file := checkSource(t, "const xs: number[] = [];\nconst ro: readonly number[] = xs;\n")
	c, done := program.GetTypeChecker(t.Context())
	defer done()
	var members []string
	for _, stmt := range file.Statements.Nodes {
		decl := stmt.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0]
		for _, prop := range c.GetPropertiesOfType(c.GetTypeAtLocation(decl.Name())) {
			if name := prop.Name(); !strings.HasPrefix(name, ast.InternalSymbolNamePrefix) && !slices.Contains(members, name) {
				members = append(members, name)
			}
		}
	}
	for _, name := range members {
		_, lowered := arrayMethods[name]
		_, refused := refusedArrayMembers[name]
		if !lowered && !refused && name != "length" && name != "push" && name != "pop" && name != "shift" {
			t.Errorf("Array.%s is neither lowered nor refused by name (lower/array_methods.go)", name)
		}
	}
	for name := range refusedArrayMembers {
		if !slices.Contains(members, name) {
			t.Errorf("refusedArrayMembers lists %s, which the libs no longer declare", name)
		}
	}
	if len(members) < 30 {
		t.Errorf("found %d members, too few to be the libs' Array: %v", len(members), members)
	}
}
