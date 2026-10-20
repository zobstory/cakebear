package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// callsTo is every call of name in stmts.
func callsTo(stmts []ir.Stmt, name string) []*ir.Call {
	var out []*ir.Call
	for _, c := range nodesOf[*ir.Call](stmts) {
		if c.Callee == name {
			out = append(out, c)
		}
	}
	return out
}

// TestOptionalParams: `b?: T` is a maybe in the signature, and a call
// leaving it out passes undefined; one passing it passes it wrapped.
func TestOptionalParams(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, "function f(a: number, b?: string): number {\n  return a;\n}\nf(1);\nf(1, \"x\");\n")
	m := p.EntryModule()
	f := funcNamed(t, m, "f")
	if len(f.Params) != 2 || f.Params[1].Name != "b" || !f.Params[1].Type.Equal(ir.MaybeOf(ir.String)) {
		t.Fatalf("f's parameters are %+v, want b a maybe of a string", f.Params)
	}
	calls := callsTo(m.Main, "f")
	if len(calls) != 2 || len(calls[0].Args) != 2 || len(calls[1].Args) != 2 {
		t.Fatalf("the calls of f are %v, want two of two arguments each", calls)
	}
	if e, ok := calls[0].Args[1].(*ir.Empty); !ok || e.Null {
		t.Errorf("f(1) passes b as %#v, want undefined", calls[0].Args[1])
	}
	if !isWrap(calls[1].Args[1]) {
		t.Errorf("f(1, \"x\") passes b as %#v, want x wrapped", calls[1].Args[1])
	}
}

// TestDefaultParams: `b = d` is a maybe in the signature, and the body
// opens with b as the argument, or d where the argument is undefined, null
// a value; d may read the parameters before it.
func TestDefaultParams(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `function g(a: number, b: number = a * 2): number {
  return a + b;
}
function h(x: number | null = 7): number | null {
  return x;
}
g(1);
g(1, 3);
h(null);
`)
	m := p.EntryModule()
	g := funcNamed(t, m, "g")
	if !g.Params[1].Type.Equal(ir.MaybeOf(ir.Number)) || g.Params[1].Name == "b" {
		t.Fatalf("g's b is %+v, want a maybe under a name of lowering's own", g.Params[1])
	}
	decl, ok := g.Body[0].(*ir.VarDecl)
	if !ok || decl.Name != "b" || decl.Type != ir.Number {
		t.Fatalf("g's body opens with %#v, want b, a number", g.Body[0])
	}
	cond := decl.Init.(*ir.Cond)
	if test := cond.Test.(*ir.IsEmpty); !test.Undefined || test.Null {
		t.Errorf("b's default is chosen by %#v, want undefined alone", test)
	}
	if _, ok := cond.Then.(*ir.Binary); !ok {
		t.Errorf("b's default is %#v, want a * 2", cond.Then)
	}
	if _, ok := cond.Else.(*ir.Unwrap); !ok {
		t.Errorf("b given is %#v, want the argument unwrapped", cond.Else)
	}
	calls := callsTo(m.Main, "g")
	if len(calls) != 2 || len(calls[0].Args) != 2 || !isEmptyValue(calls[0].Args[1]) {
		t.Fatalf("g(1) is %v, want b passed as undefined", calls)
	}
	if !isWrap(calls[1].Args[1]) {
		t.Errorf("g(1, 3) passes b as %#v, want 3 wrapped", calls[1].Args[1])
	}
	hx := funcNamed(t, m, "h").Body[0].(*ir.VarDecl)
	if test := hx.Init.(*ir.Cond).Test.(*ir.IsEmpty); test.Null || !hx.Type.Equal(ir.MaybeOf(ir.Number)) {
		t.Errorf("h's x is %#v, want a maybe whose null is kept", hx)
	}
}

// TestClosureDefaults: an arrow's optional and default parameters are
// lowered as a function's are.
func TestClosureDefaults(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, "const area = (w: number, h: number = w): number => w * h;\nconst a: number = area(3);\nconst opt = (n?: number): boolean => n === undefined;\nconst b: boolean = opt();\n")
	m := p.EntryModule()
	lits := nodesOf[*ir.FuncLit](m.Main)
	if len(lits) != 2 {
		t.Fatalf("%d function literals, want area and opt", len(lits))
	}
	if !lits[0].Params[1].Type.Equal(ir.MaybeOf(ir.Number)) {
		t.Errorf("area's h is %+v, want a maybe", lits[0].Params[1])
	}
	if d, ok := lits[0].Body[0].(*ir.VarDecl); !ok || d.Name != "h" {
		t.Errorf("area's body opens with %#v, want h", lits[0].Body[0])
	}
	for _, name := range []string{"area", "opt"} {
		calls := callsTo(m.Main, name)
		if len(calls) != 1 {
			t.Fatalf("%d calls of %s", len(calls), name)
		}
		if last := calls[0].Args[len(calls[0].Args)-1]; !isEmptyValue(last) {
			t.Errorf("%s's call passes %#v last, want undefined", name, last)
		}
	}
}

func isEmptyValue(x ir.Expr) bool {
	_, ok := x.(*ir.Empty)
	return ok
}

// TestDestructuringDefaults: `{ a = d }` is the field, or d where it is
// undefined, in a declaration, a for…of and a parameter's pattern; a
// parameter's whole pattern may have a default too.
func TestDestructuringDefaults(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `interface Opts { size?: number; label?: string | null }
const o: Opts = {};
const { size = 10 } = o;
for (const { label = "-" } of [o]) {
  console.log(label);
}
function whole({ size }: Opts = { size: 3 }): number {
  return size ?? -1;
}
`)
	m := p.EntryModule()
	var size *ir.VarDecl
	for _, s := range m.Main {
		if d, ok := s.(*ir.VarDecl); ok && d.Name == "size" {
			size = d
		}
	}
	if size == nil || size.Type != ir.Number {
		t.Fatalf("size is %#v, want a number", size)
	}
	if c, ok := size.Init.(*ir.Cond); !ok || !c.Test.(*ir.IsEmpty).Undefined {
		t.Errorf("size is given %#v, want 10 where the field is undefined", size.Init)
	}
	var label *ir.VarDecl
	for _, f := range nodesOf[*ir.ForOf](m.Main) {
		for _, s := range f.Body {
			if d, ok := s.(*ir.VarDecl); ok && d.Name == "label" {
				label = d
			}
		}
	}
	if label == nil || !label.Type.Equal(ir.MaybeOf(ir.String)) {
		t.Errorf("for…of's label is %#v, want a maybe: its null is kept", label)
	}
	whole := funcNamed(t, m, "whole")
	if pt := whole.Params[0].Type; pt.Kind != ir.KindMaybe || pt.Elem.Kind != ir.KindObject {
		t.Errorf("whole's pattern parameter is %s, want a maybe of Opts", whole.Params[0].Type)
	}
}

// TestNestedPatternDefault: a nested pattern's default is held, so the
// names it binds read one value, made once.
func TestNestedPatternDefault(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `interface In { v: number; w: number }
interface Out { inner?: In }
function make(): In {
  return { v: 1, w: 2 };
}
const o: Out = {};
const { inner: { v, w } = make() } = o;
`)
	for _, s := range p.EntryModule().Main {
		d, ok := s.(*ir.VarDecl)
		if !ok || (d.Name != "v" && d.Name != "w") {
			continue
		}
		if get, ok := d.Init.(*ir.FieldGet); !ok {
			t.Errorf("%s is %#v, want a field read", d.Name, d.Init)
		} else if _, ok := get.Object.(*ir.Ident); !ok {
			t.Errorf("%s reads a field of %#v, want the held value", d.Name, get.Object)
		}
	}
}

// TestDefaultedAppends: a defaulted string parameter the body appends to
// keeps its accumulator, on the declaration that is its binding.
func TestDefaultedAppends(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, "function rep(s: string = \"\"): string {\n  s += \"ab\";\n  return s;\n}\n")
	if d, ok := funcNamed(t, p.EntryModule(), "rep").Body[0].(*ir.VarDecl); !ok || !d.Appended {
		t.Errorf("rep's s is %#v, want a declaration marked appended", d)
	}
}

func TestParamRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"a rest parameter", "const r = (...xs: number[]): number => 0;\n", "rest parameters are not supported yet"},
		{"an optional parameter where a required one is passed", "const xs: number[] = [1];\nconst ys: string[] = xs.map((x: number, i?: number): string => \"\");\n", "parameter i is"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}
