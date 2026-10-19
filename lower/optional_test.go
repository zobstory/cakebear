package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

const opts = "interface O { a: number; b?: string }\n"

// TestOptionalShapes: `b?: string` is an optional field of a maybe, a shape
// apart from `b: string | undefined`, which prints differently; Partial's
// properties are optional too.
func TestOptionalShapes(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, opts+`interface R { a: number; b: string | undefined }
const o: O = { a: 1 };
const r: R = { a: 1, b: undefined };
const q: Partial<R> = {};
`)
	m := p.EntryModule()
	o, r := initOf(t, m, 0).ExprType().Shape, initOf(t, m, 1).ExprType().Shape
	if o == r {
		t.Fatal("{ b?: string } and { b: string | undefined } are one shape")
	}
	if f := o.Fields[o.Index("b")]; !f.Optional || !f.Type.Equal(ir.MaybeOf(ir.String)) {
		t.Errorf("b?: string is %+v, want an optional maybe of a string", f)
	}
	if f := r.Fields[r.Index("b")]; f.Optional {
		t.Error("b: string | undefined is optional; it is required, and may be undefined")
	}
	q := initOf(t, m, 2).ExprType().Shape
	for _, f := range q.Fields {
		if !f.Optional {
			t.Errorf("Partial's %s isn't optional", f.Name)
		}
	}
}

// TestOptionalLiterals: a literal may leave an optional field out, and sets
// one it lists, undefined included, which makes it present.
func TestOptionalLiterals(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, opts+`const left: O = { a: 1 };
const set: O = { a: 1, b: "x" };
const undef: O = { b: undefined, a: 1 };
const b: string = "y";
const short: O = { a: 1, b };
`)
	m := p.EntryModule()
	if lit := initOf(t, m, 0).(*ir.ObjectLit); len(lit.Fields) != 1 {
		t.Errorf("{ a: 1 } sets %d fields, want a alone", len(lit.Fields))
	}
	if lit := initOf(t, m, 1).(*ir.ObjectLit); !isWrap(lit.Fields[1].Value) {
		t.Errorf("b: \"x\" is %#v, want it wrapped", lit.Fields[1].Value)
	}
	if lit := initOf(t, m, 2).(*ir.ObjectLit); len(lit.Fields) != 2 {
		t.Errorf("{ b: undefined, a: 1 } sets %d fields, want both: b is present, as undefined", len(lit.Fields))
	} else if _, ok := lit.Fields[0].Value.(*ir.Empty); !ok {
		t.Errorf("b: undefined is %#v, want undefined", lit.Fields[0].Value)
	}
	if lit := initOf(t, m, 4).(*ir.ObjectLit); !isWrap(lit.Fields[1].Value) {
		t.Errorf("the shorthand { b } is %#v, want b wrapped", lit.Fields[1].Value)
	}
}

// TestShorthandIntoAMaybe: a shorthand property fills a field of a maybe, or
// a number's with an extension type, as a written-out one does. It was
// refused for both since M5 step 3: a shorthand has no slot of its own.
func TestShorthandIntoAMaybe(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, "interface M { v: number | undefined; n: number }\nconst v: number = 1;\nconst n: i32 = i32(2);\nconst m: M = { v, n };\n")
	lit := initOf(t, p.EntryModule(), 2).(*ir.ObjectLit)
	if !isWrap(lit.Fields[0].Value) {
		t.Errorf("{ v } is %#v, want v wrapped", lit.Fields[0].Value)
	}
	if _, ok := lit.Fields[1].Value.(*ir.Convert); !ok {
		t.Errorf("{ n } is %#v, want the i32 widened", lit.Fields[1].Value)
	}
}

// TestOptionalPresence: writes are field sets, `delete o.b` a FieldDelete
// and `"b" in o` a HasField, of required and optional fields alike.
func TestOptionalPresence(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, opts+`const o: O = { a: 1 };
o.b = "x";
delete o.b;
const has: boolean = "b" in o;
const always: boolean = "a" in o;
o.b ??= "y";
const { b } = o;
`)
	m := p.EntryModule()
	sets := nodesOf[*ir.FieldSet](m.Main)
	if len(sets) != 2 || !isWrap(sets[0].Value) {
		t.Errorf("o.b = \"x\" and ??= set %v, want two field sets, the first of x wrapped", sets)
	}
	if dels := nodesOf[*ir.FieldDelete](m.Main); len(dels) != 1 || dels[0].Field != 1 {
		t.Errorf("delete o.b is %v, want one FieldDelete of b", dels)
	}
	if h, ok := initOf(t, m, 3).(*ir.HasField); !ok || h.Field != 1 {
		t.Errorf("\"b\" in o is %#v, want a HasField of b", initOf(t, m, 3))
	}
	if h, ok := initOf(t, m, 4).(*ir.HasField); !ok || h.Field != 0 {
		t.Errorf("\"a\" in o is %#v, want a HasField of a", initOf(t, m, 4))
	}
	var bound ir.Type
	for _, s := range m.Main {
		if d, ok := s.(*ir.VarDecl); ok && d.Name == "b" {
			bound = d.Type
		}
	}
	if !bound.Equal(ir.MaybeOf(ir.String)) {
		t.Errorf("const { b } = o binds a %s, want a maybe of a string", bound)
	}
}

func TestOptionalRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"delete as a value", opts + "const o: O = { a: 1 };\nconst d: boolean = delete o.b;\n", "delete used as a value is not supported yet"},
		{"delete of an element", "const xs: (number | undefined)[] = [1];\ndelete xs[0];\n", "delete of anything but an object's property"},
		{"in with a computed key", opts + "const o: O = { a: 1 };\nconst k: string = \"b\";\nconst h: boolean = k in o;\n", "in with a key that isn't written as a string"},
		{"in with another key", opts + "const o: O = { a: 1 };\nconst h: boolean = \"c\" in o;\n", "in with \"c\", which isn't a property of"},
		{"in on an array", "const xs: number[] = [1];\nconst h: boolean = \"length\" in xs;\n", "in on a number[] is not supported yet"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}
