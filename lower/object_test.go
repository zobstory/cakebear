package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// lowerProgram type-checks a snippet and lowers it as a whole program, the
// way cakec does, with its shapes.
func lowerProgram(t *testing.T, src string) (*ir.Program, []*Error) {
	t.Helper()
	program, file := checkSource(t, src)
	c, done := program.GetTypeChecker(t.Context())
	defer done()
	return Program(file, c)
}

func lowerProgramClean(t *testing.T, src string) *ir.Program {
	t.Helper()
	p, errs := lowerProgram(t, src)
	if len(errs) > 0 {
		t.Fatalf("unexpected lowering errors: %v", errs)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return p
}

// TestObjectShapes: an interface and a type alias with the same fields are
// one shape, a literal keeps its keys in source order, and Program.Shapes is
// the shapes the program uses.
func TestObjectShapes(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `interface Point { x: number; y: number }
type Vec = { y: number; x: number };
interface Unused { z: boolean }
const p: Point = { y: 2, x: 1 };
const v: Vec = p;
const tagged: { name: string; at: Point } = { name: "a", at: p };
`)
	if len(p.Shapes) != 2 {
		t.Fatalf("%d shapes, want 2 (Point and the tagged one): %v", len(p.Shapes), p.Shapes)
	}
	main := p.EntryModule().Main
	pt := main[0].(*ir.VarDecl)
	lit := pt.Init.(*ir.ObjectLit)
	if got := []int{lit.Fields[0].Field, lit.Fields[1].Field}; got[0] != 1 || got[1] != 0 {
		t.Errorf("literal fields %v, want [1 0]: y then x, by index in the sorted shape", got)
	}
	if v := main[1].(*ir.VarDecl); !v.Type.Equal(pt.Type) {
		t.Errorf("Vec is %s and Point %s, want one shape", v.Type, pt.Type)
	}
	if got := strings.Join(pt.Type.Shape.Names, ","); got != "Point,Vec" {
		t.Errorf("Names = %q, want Point,Vec", got)
	}
}

// TestFieldAccess: reads, writes and updates of fields.
func TestFieldAccess(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `interface Counter { n: number; label: string }
const c: Counter = { n: 0, label: "c" };
c.n = c.n + 1;
c.n++;
c.label += "!";
const m: number = c.n;
`)
	var sets, gets int
	for _, m := range p.Modules {
		ir.Inspect(m, func(node any) {
			switch node.(type) {
			case *ir.FieldSet:
				sets++
			case *ir.FieldGet:
				gets++
			}
		})
	}
	if sets != 3 || gets != 4 {
		t.Errorf("%d field sets and %d gets, want 3 and 4", sets, gets)
	}
}

// TestRecursiveObjectType: a type that refers to itself is one shape, and
// the program validates.
func TestRecursiveObjectType(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `interface Link { name: string; next: Link }
function second(l: Link): string {
  return l.next.name;
}
`)
	if len(p.Shapes) != 1 {
		t.Fatalf("%d shapes, want 1", len(p.Shapes))
	}
	s := p.Shapes[0]
	if next := s.Fields[s.Index("next")].Type; next.Shape != s {
		t.Errorf("next is a %s, want the shape itself", next)
	}
}

// TestObjectRefusals: what M4 can't hold exactly is refused by name (D5,
// D6), and nothing reaches the backend half-made.
func TestObjectRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src, want string
	}{
		{"an optional property", "interface P { x?: number }\nconst p: P = { x: 1 };\n", "optional property `x` is not supported yet"},
		{"Record", "const r: Record<string, number> = { a: 1 };\n", "index signatures, and Record, are not supported yet"},
		{"a method in an interface", "interface M { f(): number }\nconst m: M = { f: (): number => 1 };\n", "method `f` is not supported yet"},
		{"a method in a literal", "interface F { f: () => number }\nconst o: F = { f(): number { return 1; } };\n", "a method in an object literal is not supported yet"},
		{"a spread", "const a: { x: number } = { x: 1 };\nconst b: { x: number } = { ...a };\n", "spreading into an object literal is not supported yet"},
		{"a computed key", "const o: { x: number } = { [\"x\"]: 1 };\n", "a computed property name is not supported yet"},
		{"an integer-like key", "const o: { 1: number } = { 1: 2 };\n", "an integer-like property name (`1`) is not supported yet"},
		{"a wider object in a variable", "interface P { x: number }\ninterface P3 { x: number; z: number }\nconst a: P3 = { x: 1, z: 2 };\nconst b: P = a;\n",
			"a P3 can't be used as a P: an object has exactly the fields of its type in cakebear, and it has `z` too"},
		{"a wider object as an argument", "interface P { x: number }\ninterface P3 { x: number; z: number }\nfunction f(p: P): number {\n  return p.x;\n}\nconst a: P3 = { x: 1, z: 2 };\nconst n: number = f(a);\n",
			"a P3 can't be used as a P"},
		{"a wider object returned", "interface P { x: number }\ninterface P3 { x: number; z: number }\nfunction f(a: P3): P {\n  return a;\n}\n", "a P3 can't be used as a P"},
		{"a wider object in a field", "interface P { x: number }\ninterface P3 { x: number; z: number }\ninterface Box { p: P }\nconst a: P3 = { x: 1, z: 2 };\nconst b: Box = { p: a };\n", "a P3 can't be used as a P"},
		{"a union", "const u: { a: number } | { b: string } = { a: 1 };\n", "unions of object types are not supported yet"},
		{"a tuple property", "interface Pair { p: [number, string] }\nfunction size(b: Pair): number {\n  return 1;\n}\n", "property `p`: tuples are not supported yet"},
		{"logging an object holding a function", "const o: { f: () => number } = { f: (): number => 1 };\nconsole.log(o);\n", "console.log cannot print a { f: () => number } yet: it holds a function"},
		{"=== between two shapes", "const a: { x: number } = { x: 1 };\nconst b: { y: number } = { y: 1 };\nconst t: boolean = (a as unknown) === (b as unknown);\n", "not supported yet"},
		{"a recursive type failing partway", "interface A { d: D; bad?: number }\ninterface D { a: A }\nfunction f(d: D): number {\n  return 1;\n}\n", "optional property `bad` is not supported yet"},
		// X fails after Y was classified against X's reserved shape: Y must
		// fail too when used later, not pass holding a shape never defined.
		{"a type whose recursive part failed", "interface X { y: Y; bad?: number }\ninterface Y { x: X }\nfunction f(x: X): number {\n  return 1;\n}\nfunction g(y: Y): number {\n  return 2;\n}\n",
			"parameter y has a type the backend cannot represent yet: property `x`"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors = %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}

// TestAggregateSharedStateRefused (D10): a concurrent function may use only
// the objects it creates. A const object's fields can still change, so one
// declared outside a handler or a spawned function is shared state, whether
// read directly or through a function it calls; one it makes itself is its
// own.
func TestAggregateSharedStateRefused(t *testing.T) {
	t.Parallel()

	const server = "import { createServer } from \"node:http\";\ninterface Route { path: string; body: string }\n"
	for _, tt := range []struct {
		name, src, want string
	}{
		{"a const object read in a handler", server + "const home: Route = { path: \"/\", body: \"hi\" };\ncreateServer((req, res) => {\n  res.end(home.body);\n}).listen(3000);\n",
			"this handler runs on many requests at once, so it can't use `home`, an object created outside it; an object can be shared between requests only if it is frozen (a const, readonly all the way down, built from literals), and this one isn't: its type isn't readonly all the way down: property `path` isn't readonly. Freeze it, or create it inside the handler, or in a function that returns a new one"},
		{"through a function the handler calls", server + "const home: Route = { path: \"/\", body: \"hi\" };\nfunction body(): string {\n  return home.body;\n}\ncreateServer((req, res) => {\n  res.end(body());\n}).listen(3000);\n",
			"can't call `body()`, which uses `home`, an object created outside the handler, which isn't frozen: its type isn't readonly all the way down"},
		{"a let object", server + "let current: Route = { path: \"/\", body: \"hi\" };\ncreateServer((req, res) => {\n  res.end(current.path);\n}).listen(3000);\n",
			"can't use `current`, an object created outside it; an object can be shared between requests only if it is frozen (a const, readonly all the way down, built from literals), and this one isn't: it is a let"},
		{"captured by a spawned function", "export {};\nconst cfg: { n: number } = { n: 1 };\nconst r: number = await spawn((): number => cfg.n);\n",
			"this spawned function runs in parallel with its caller, so it can't use `cfg`, an object created outside it; an object can be shared with a spawned function only if it is frozen (a const, readonly all the way down, built from literals), and this one isn't: its type isn't readonly all the way down: property `n` isn't readonly. Freeze it, or create it inside the spawned function"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors = %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}

	// A handler's own objects, and a function returning a new one, are fine.
	lowerProgramClean(t, server+`function route(path: string): Route {
  return { path: path, body: "page " + path };
}
createServer((req, res) => {
  const mine: Route = { path: req.url, body: "ok" };
  mine.body = mine.body + "!";
  res.end(route(mine.path).body);
}).listen(3000);
`)
}

// TestUsedShapesDropsUnused: a shape interned for a type nothing in the
// program holds, such as one reserved for a recursive type that then
// failed, stays out of Program.Shapes, and the rest are numbered again.
func TestUsedShapesDropsUnused(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	unused := shapes.Reserve("Broken")
	inner := shapes.Intern("Inner", []ir.Field{{Name: "a", Type: ir.Number}})
	outer := shapes.Intern("Outer", []ir.Field{{Name: "in", Type: ir.ObjectOf(inner)}})
	p := &ir.Program{Modules: []*ir.Module{{Name: "t", Main: []ir.Stmt{
		&ir.VarDecl{Name: "o", Type: ir.ObjectOf(outer), Init: &ir.Ident{Name: "x", Typ: ir.ObjectOf(outer), Stable: true}},
	}}}}
	got := usedShapes(p, shapes.All())
	if len(got) != 2 || got[0] != inner || got[1] != outer {
		t.Fatalf("usedShapes = %v, want Inner then Outer, without %s", got, unused)
	}
	if inner.ID != 0 || outer.ID != 1 {
		t.Errorf("IDs %d, %d; want 0, 1", inner.ID, outer.ID)
	}
	p.Shapes = got
	if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
