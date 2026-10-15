package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestInferredResults: a function declared without a result type has the one
// TypeScript infers, a number, an object of the shape its literal makes, a
// string or nothing, where it used to be void whatever it returned.
func TestInferredResults(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `function one() {
  return 1;
}
function point(x: number) {
  return { x: x, y: x * 2 };
}
function label(n: number) {
  return `+"`n=${n}`"+`;
}
function nothing() {
  console.log("x");
}
async function later() {
  return 2;
}
`)
	want := map[string]string{"one": "number", "point": "{ x: number; y: number }", "label": "string", "nothing": "void", "later": "Promise<number>"}
	for _, fn := range p.EntryModule().Funcs {
		if got := fn.Result.String(); got != want[fn.Name] {
			t.Errorf("%s returns %s, want %s", fn.Name, got, want[fn.Name])
		}
	}

	// One TypeScript infers as a union is refused, pointing at the annotation.
	_, errs := lowerSource(t, "function either(b: boolean) {\n  if (b) {\n    return 1;\n  }\n  return \"a\";\n}\n")
	if msgs := joinMsgs(errs); !strings.Contains(msgs, "function either returns a type the backend cannot represent yet") || !strings.Contains(msgs, "annotating its result may help") {
		t.Errorf("errors = %q, want either's result refused, with the hint", msgs)
	}
}

// TestExtensionWidensToNumber: one of cakebear's numeric types filling a
// number's slot, a variable, an argument or a result, is converted to a
// number, where it used to reach go build as an int32 in a float64's place.
// A base64 in a string's slot needs nothing: both are Go strings.
func TestExtensionWidensToNumber(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `const i: i32 = i32(3);
const n: number = i;
function twice(x: number): number {
  return x * 2;
}
function f(x: u64): number {
  return x;
}
const m: number = twice(i);
const b: base64 = base64("aGk=");
const s: string = b;
`)
	widened := func(what string, e ir.Expr, from ir.Type) {
		t.Helper()
		c, ok := e.(*ir.Convert)
		if !ok || c.Typ != ir.Number || c.Value.ExprType() != from {
			t.Errorf("%s is %#v, want a %s converted to a number", what, e, from)
		}
	}
	widened("n's initialiser", m.Main[1].(*ir.VarDecl).Init, ir.Int32)
	widened("twice(i)'s argument", m.Main[2].(*ir.VarDecl).Init.(*ir.Call).Args[0], ir.Int32)
	widened("f's result", m.Funcs[1].Body[0].(*ir.Return).Value, ir.Uint64)
	if s := m.Main[4].(*ir.VarDecl).Init; s.ExprType() != ir.Base64 {
		t.Errorf("s's initialiser is %#v, want the base64 as it is", s)
	}
}
