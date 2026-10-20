package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// The bugs M5's re-measurement of wx-next found (check 9), each a lowering
// that refused valid code, or let invalid IR through to Validate.

// TestLetWithNoValue: a let of any type may be declared with no value, a
// maybe's undefined, anything else Go's zero; a for loop's first clause
// still needs one.
func TestLetWithNoValue(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, "let n: number;\nn = 1;\nlet m: number | undefined;\nconsole.log(n, m);\n")
	m := p.EntryModule()
	if d := m.Main[0].(*ir.VarDecl); d.Init != nil || d.Type != ir.Number {
		t.Errorf("let n: number; is %#v, want a number with no value", d)
	}
	if d := m.Main[2].(*ir.VarDecl); !isEmptyValue(d.Init) {
		t.Errorf("let m: number | undefined; is %#v, want undefined", d)
	}
	_, errs := lowerProgram(t, "for (let i: number; ; ) {\n  break;\n}\n")
	if msgs := joinMsgs(errs); !strings.Contains(msgs, "a for loop's let with no value is not supported yet") {
		t.Errorf("for (let i: number;;) gave %q", msgs)
	}
}

// TestShorthandIsNarrowed: `{ x }` reads x as narrowed there, as `{ x: x }`
// does.
func TestShorthandIsNarrowed(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, "interface Box { label: string }\nfunction make(label: string | undefined): Box | undefined {\n  if (label === undefined) {\n    return undefined;\n  }\n  return { label };\n}\n")
	lits := nodesOf[*ir.ObjectLit](funcNamed(t, p.EntryModule(), "make").Body)
	if len(lits) != 1 {
		t.Fatalf("%d literals, want 1", len(lits))
	}
	if _, ok := lits[0].Fields[0].Value.(*ir.Unwrap); !ok {
		t.Errorf("{ label } is %#v, want label unwrapped", lits[0].Fields[0].Value)
	}
}

// TestAsyncReturnSlots: an async function's returns fill its promise's
// element, which the checker types `T | PromiseLike<T>`: a value is wrapped
// into a maybe there, and an empty array literal typed by it.
func TestAsyncReturnSlots(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `interface Ids { ids: string[] }
async function count(n: number): Promise<number | undefined> {
  return n;
}
async function none(): Promise<Ids> {
  return { ids: [] };
}
`)
	m := p.EntryModule()
	if r := nodesOf[*ir.Return](funcNamed(t, m, "count").Body); len(r) != 1 || !isWrap(r[0].Value) {
		t.Errorf("count returns %v, want n wrapped", r)
	}
	if lits := nodesOf[*ir.ArrayLit](funcNamed(t, m, "none").Body); len(lits) != 1 || lits[0].Typ.String() != "string[]" {
		t.Errorf("none's [] is %v, want a string[]", lits)
	}
}

func TestMeasuredRefusals(t *testing.T) {
	t.Parallel()

	_, errs := lowerProgram(t, "export async function f() {\n  return null;\n}\n")
	if msgs := joinMsgs(errs); !strings.Contains(msgs, "async function f returns a type the backend cannot represent yet") {
		t.Errorf("an async function inferred to give null: %q", msgs)
	}
	_, errs = lowerProgram(t, "interface T { a: number }\nfunction get(): T | undefined {\n  return undefined;\n}\nconst t: T | undefined = get();\nconst { a } = t ?? {};\n")
	if len(errs) == 0 || errs[0].Kind != Unsupported || !strings.Contains(errs[0].Msg, "is not supported yet") {
		t.Errorf("const { a } = t ?? {} gave %v, want cannot compile yet: TypeScript allows it", errs)
	}
}
