package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// getters declares a maybe of a number and of an object, from calls, since
// TypeScript narrows a const by its initialiser.
const getters = `interface B { c: number; s: string | null }
interface A { b: B; xs: number[] }
function num(): number | undefined {
  return 1;
}
function obj(): A | undefined {
  return undefined;
}
const m: number | undefined = num();
const a: A | undefined = obj();
`

// condOf is a Cond's parts, with any Let around it removed and returned.
func condOf(t *testing.T, x ir.Expr) (*ir.Cond, *ir.Let) {
	t.Helper()
	let, _ := x.(*ir.Let)
	if let != nil {
		x = let.Body
	}
	c, ok := x.(*ir.Cond)
	if !ok {
		t.Fatalf("%#v is no Cond", x)
	}
	return c, let
}

// TestConditionalLowering: `c ? a : b` is a Cond of the branches' type, the
// slot's when they fill one, or else the type the checker gives the whole.
func TestConditionalLowering(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `const c: boolean = true;
const s: string = c ? "a" : "b";
const u: number | undefined = c ? 1 : undefined;
console.log(c ? null : 2);
`)
	m := p.EntryModule()
	if cond, _ := condOf(t, initOf(t, m, 1)); cond.Typ != ir.String {
		t.Errorf("a ?: of two strings is a %s", cond.Typ)
	}
	cond, _ := condOf(t, initOf(t, m, 2))
	if _, ok := cond.Else.(*ir.Empty); !ok || !isWrap(cond.Then) || !cond.Typ.Equal(ir.MaybeOf(ir.Number)) {
		t.Errorf("c ? 1 : undefined is %#v, want a maybe of 1 wrapped or undefined", cond)
	}
	logged, _ := condOf(t, nodesOf[*ir.ConsoleLog](m.Main)[0].Args[0])
	if e, ok := logged.Then.(*ir.Empty); !ok || !e.Null || !isWrap(logged.Else) {
		t.Errorf("c ? null : 2 with no slot is %#v, want null or 2 wrapped, the checker's 2 | null", logged)
	}
}

// TestCoalesceLowering: `x ?? d` is a Cond on x being null or undefined, x
// held in a Let only when reading it again could run code, unwrapped where
// the result is its element.
func TestCoalesceLowering(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, getters+`const n: number = m ?? 0;
const k: number = num() ?? 0;
const j: number | undefined = m ?? num();
const plain: number = 5;
const same: number = plain ?? 1;
`)
	m := p.EntryModule()
	cond, let := condOf(t, initOf(t, m, 2))
	if let != nil {
		t.Errorf("m ?? 0 holds m in %q; a binding is read again", let.Name)
	}
	if e, ok := cond.Test.(*ir.IsEmpty); !ok || !e.Undefined || !e.Null {
		t.Errorf("m ?? 0 tests %#v, want for null and undefined both", cond.Test)
	}
	if _, ok := cond.Else.(*ir.Unwrap); !ok || cond.Typ != ir.Number {
		t.Errorf("m ?? 0 chooses %#v, want m unwrapped, a number", cond.Else)
	}
	if cond.Test.(*ir.IsEmpty).Value == cond.Else.(*ir.Unwrap).Value {
		t.Error("the test and the branch share one node; each read must be its own")
	}
	if _, let := condOf(t, initOf(t, m, 3)); let == nil {
		t.Error("num() ?? 0 calls num again for its value; it must be held in a Let")
	}
	if cond, _ := condOf(t, initOf(t, m, 4)); !cond.Typ.Equal(ir.MaybeOf(ir.Number)) {
		t.Errorf("m ?? num() is a %s, want a maybe, as num() may be undefined", cond.Typ)
	}
	if _, ok := initOf(t, m, 6).(*ir.Ident); !ok {
		t.Errorf("plain ?? 1 is %#v, want plain alone: it can't be empty", initOf(t, m, 6))
	}
}

// TestNonNullLowering: `x!` is an Unwrap, Asserted; on a value that can't be
// empty it is the value.
func TestNonNullLowering(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, getters+"const n: number = m!;\nconst c: number = a!.b.c;\nconst five: number = 5;\nconst same: number = five!;\n")
	m := p.EntryModule()
	if u, ok := initOf(t, m, 2).(*ir.Unwrap); !ok || !u.Asserted {
		t.Errorf("m! is %#v, want an asserted unwrap", initOf(t, m, 2))
	}
	us := nodesOf[*ir.Unwrap](m.Main[3:4])
	if len(us) != 1 || !us[0].Asserted {
		t.Errorf("a!.b.c unwraps %v, want a, asserted", us)
	}
	if _, ok := initOf(t, m, 5).(*ir.Ident); !ok {
		t.Errorf("five! is %#v, want five", initOf(t, m, 5))
	}
}

// TestOptionalChainLowering: `a?.b.c` is undefined when a is empty and the
// chain read from a's value otherwise, through fields, elements and members;
// on a value that can't be empty, `?.` is `.`.
func TestOptionalChainLowering(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, getters+`const c: number | undefined = a?.b.c;
const fromCall: number | undefined = obj()?.b.c;
const nested: number | undefined = a?.b.s?.length;
const at: number | undefined = a?.xs[1];
const upper: string | null | undefined = a?.b.s?.toUpperCase();
const o: B = { c: 1, s: null };
const plain: number = o?.c;
`)
	m := p.EntryModule()
	cond, let := condOf(t, initOf(t, m, 2))
	if let != nil {
		t.Errorf("a?.b.c holds a in %q; a binding is read again", let.Name)
	}
	// undefined even when a is null: `null?.b` is undefined.
	if e, ok := cond.Then.(*ir.Empty); !ok || e.Null || !cond.Typ.Equal(ir.MaybeOf(ir.Number)) {
		t.Errorf("a?.b.c is %#v when a is empty, want undefined of a maybe", cond.Then)
	}
	w, ok := cond.Else.(*ir.Wrap)
	if !ok {
		t.Fatalf("a?.b.c is %#v otherwise, want the chain wrapped", cond.Else)
	}
	get, ok := w.Value.(*ir.FieldGet)
	inner, ok2 := get.Object.(*ir.FieldGet)
	if !ok || !ok2 {
		t.Fatalf("a?.b.c reads %#v, want .b then .c", w.Value)
	}
	if _, ok := inner.Object.(*ir.Unwrap); !ok {
		t.Errorf("a?.b reads b from %#v, want a's value unwrapped", inner.Object)
	}
	if _, let := condOf(t, initOf(t, m, 3)); let == nil {
		t.Error("obj()?.b.c calls obj again for the chain; it must be held in a Let")
	}
	for _, i := range []int{4, 5, 6} {
		if outer, _ := condOf(t, initOf(t, m, i)); outer.Typ.Kind != ir.KindMaybe {
			t.Errorf("chain %d is a %s, want a maybe", i, outer.Typ)
		}
	}
	if _, ok := initOf(t, m, 8).(*ir.FieldGet); !ok {
		t.Errorf("o?.c on an object that can't be empty is %#v, want o.c", initOf(t, m, 8))
	}
}

// TestCoalesceAssignLowering: `x ??= d` is an if assigning d, wrapped, when x
// is empty, for a name and a field; on a value that can't be empty it is
// nothing.
func TestCoalesceAssignLowering(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, getters+`let z: number | undefined = num();
z ??= 10;
const b: B = { c: 1, s: null };
b.s ??= "set";
let plain: number = 1;
plain ??= 2;
`)
	m := p.EntryModule()
	ifs := nodesOf[*ir.If](m.Main)
	if len(ifs) != 2 {
		t.Fatalf("%d ifs, want z's and b.s's, and none for plain", len(ifs))
	}
	if as, ok := ifs[0].Then[0].(*ir.Assign); !ok || !isWrap(as.Value) {
		t.Errorf("z ??= 10 is %#v, want 10 wrapped, assigned", ifs[0].Then)
	}
	if _, ok := ifs[1].Then[0].(*ir.FieldSet); !ok {
		t.Errorf("b.s ??= is %#v, want a field set", ifs[1].Then)
	}
	for _, f := range ifs {
		if e, ok := f.Cond.(*ir.IsEmpty); !ok || !e.Undefined || !e.Null {
			t.Errorf("??= tests %#v, want null and undefined both", f.Cond)
		}
	}
}

func TestCondRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"branches of two types", "const c: boolean = true;\nconsole.log(c ? 1 : \"a\");\n", "a union of a"},
		{"??= as a value", getters + "let z: number | undefined = num();\nconst y: number = (z ??= 1);\n", "??= used as a value is not supported yet"},
		{"?? of two types", getters + "console.log(m ?? \"none\");\n", "a union of a"},
		{"??= on a call's field", getters + "function bee(): B {\n  return { c: 1, s: null };\n}\nbee().s ??= \"x\";\n", "??= on anything but a variable's name"},
		{"an optional call", "function get(): (() => number) | undefined {\n  return undefined;\n}\nconst f: (() => number) | undefined = get();\nconst n: number | undefined = f?.();\n",
			"calling a function that may be null or undefined is not supported yet"},
		{"||=", getters + "let z: number | undefined = num();\nz ||= 1;\n", "not supported yet"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}
