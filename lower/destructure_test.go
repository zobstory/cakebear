package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestShorthandProperty (D14): `{ x }` reads the variable x into the field x,
// as `{ x: x }` does, qualified with its module when it is top-level and
// marked unstable when something assigns it.
func TestShorthandProperty(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `const x: number = 1;
let y: string = "a";
y = "b";
const o: { x: number; y: string } = { y, x };
function wrap(x: number): { x: number } {
  return { x };
}
const later = (): { y: string } => ({ y });
function first(a: number): number {
  return a;
}
const pair: { first: (a: number, b: number) => number } = { first };
`)
	lit := m.Main[3].(*ir.VarDecl).Init.(*ir.ObjectLit)
	if len(lit.Fields) != 2 {
		t.Fatalf("%d fields, want 2", len(lit.Fields))
	}
	for i, want := range []struct {
		name   string
		field  int
		stable bool
	}{{"y", 1, false}, {"x", 0, true}} {
		f := lit.Fields[i]
		id, ok := f.Value.(*ir.Ident)
		if !ok {
			t.Fatalf("field %d is %T, want the variable %s", i, f.Value, want.name)
		}
		if id.Name != want.name || f.Field != want.field || id.Stable != want.stable || id.Module != m.File {
			t.Errorf("field %d = %s (field %d, stable %t, module %q), want %s (field %d, stable %t, module %q)",
				i, id.Name, f.Field, id.Stable, id.Module, want.name, want.field, want.stable, m.File)
		}
	}
	ret := m.Funcs[0].Body[0].(*ir.Return).Value.(*ir.ObjectLit)
	if id := ret.Fields[0].Value.(*ir.Ident); id.Name != "x" || id.Module != "" {
		t.Errorf("wrap's { x } reads %s in module %q, want its own parameter x", id.Name, id.Module)
	}
	if caps := m.Main[4].(*ir.VarDecl).Init.(*ir.FuncLit).Captures; len(caps) != 1 || caps[0].Name != "y" || !caps[0].Mutable {
		t.Errorf("`() => ({ y })` captures %v, want the let y", caps)
	}
	// first takes one parameter where its field's type takes two: it is
	// adapted to the field, as a name in `{ first: first }` would be.
	if v := m.Main[5].(*ir.VarDecl).Init.(*ir.ObjectLit).Fields[0].Value; !v.ExprType().Equal(m.Main[5].(*ir.VarDecl).Type.Shape.Fields[0].Type) {
		t.Errorf("{ first } holds a %s, want its field's %s", v.ExprType(), m.Main[5].(*ir.VarDecl).Type.Shape.Fields[0].Type)
	}
}

// TestDestructuring (D14): the value destructured is evaluated once, into a
// binding of its own, and each name the pattern gives reads its field from
// that, through a nested pattern's fields too. const and let give const and
// let bindings.
func TestDestructuring(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `interface Point { x: number; y: number }
interface Item { name: string; at: Point }
function f(i: Item): number {
  const { name, at: { x, y: down } } = i;
  let { at } = i;
  return x + down;
}
`)
	body := m.Funcs[0].Body
	if len(body) != 7 {
		t.Fatalf("%d statements, want 7: a held value and three names, then a held value and one name, then the return", len(body))
	}
	held := body[0].(*ir.VarDecl)
	if !strings.HasPrefix(held.Name, "ts destructured") || held.Mutable {
		t.Errorf("first statement declares %q (mutable %t), want a const of lowering's own", held.Name, held.Mutable)
	}
	if id, ok := held.Init.(*ir.Ident); !ok || id.Name != "i" {
		t.Errorf("the held value is %v, want the parameter i", held.Init)
	}
	reads := func(e ir.Expr) string { // the source of a chain of field reads, and its depth
		depth := 0
		for {
			g, ok := e.(*ir.FieldGet)
			if !ok {
				id, _ := e.(*ir.Ident)
				if id == nil {
					return "?"
				}
				return id.Name + strings.Repeat(".f", depth)
			}
			e, depth = g.Object, depth+1
		}
	}
	for i, want := range []struct {
		name, from string
		mutable    bool
	}{
		{"name", held.Name + ".f", false},
		{"x", held.Name + ".f.f", false},
		{"down", held.Name + ".f.f", false},
	} {
		d := body[i+1].(*ir.VarDecl)
		if d.Name != want.name || reads(d.Init) != want.from || d.Mutable != want.mutable {
			t.Errorf("statement %d declares %s from %s (mutable %t), want %s from %s (mutable %t)",
				i+1, d.Name, reads(d.Init), d.Mutable, want.name, want.from, want.mutable)
		}
	}
	second := body[4].(*ir.VarDecl)
	if second.Name == held.Name {
		t.Errorf("both patterns hold their value in %q", held.Name)
	}
	if at := body[5].(*ir.VarDecl); at.Name != "at" || !at.Mutable || reads(at.Init) != second.Name+".f" {
		t.Errorf("let { at } declares %s from %s (mutable %t), want a let read from %s", at.Name, reads(at.Init), at.Mutable, second.Name)
	}
}

// TestDestructuredParameters: a parameter written as a pattern gets a name of
// its own, and the body opens by binding the pattern's names from it, in a
// function, a closure and a for…of loop alike.
func TestDestructuredParameters(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `interface Point { x: number; y: number }
function sum({ x, y }: Point): number {
  return x + y;
}
const pts: Point[] = [{ x: 1, y: 2 }];
const ys: number[] = pts.map(({ y }) => y);
for (const { x } of pts) {
  console.log(x);
}
`)
	opens := func(what, param string, body []ir.Stmt, names ...string) {
		t.Helper()
		if !strings.HasPrefix(param, "ts ") {
			t.Errorf("%s binds the pattern to %q, want a name of lowering's own", what, param)
		}
		if len(body) < len(names) {
			t.Fatalf("%s's body has %d statements, want at least %d", what, len(body), len(names))
		}
		for i, name := range names {
			d, ok := body[i].(*ir.VarDecl)
			if !ok || d.Name != name {
				t.Errorf("%s's statement %d is %v, want the declaration of %s", what, i, body[i], name)
				continue
			}
			if g, ok := d.Init.(*ir.FieldGet); !ok || g.Object.(*ir.Ident).Name != param {
				t.Errorf("%s's %s is %v, want a field of %s", what, name, d.Init, param)
			}
		}
	}
	fn := m.Funcs[0]
	opens("sum", fn.Params[0].Name, fn.Body, "x", "y")

	var lit *ir.FuncLit
	var loop *ir.ForOf
	ir.Inspect(m, func(node any) {
		switch n := node.(type) {
		case *ir.FuncLit:
			lit = n
		case *ir.ForOf:
			loop = n
		}
	})
	if lit == nil || loop == nil {
		t.Fatalf("no closure (%v) or no for…of (%v)", lit, loop)
	}
	opens("the closure", lit.Params[0].Name, lit.Body, "y")
	opens("the loop", loop.Name, loop.Body, "x")
}

func TestDestructuringRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"an array pattern", "const xs: number[] = [1];\nconst [a] = xs;\n", "array destructuring is not supported yet"},
		{"an array pattern parameter", "function f([a]: number[]): number {\n  return a;\n}\n", "array destructuring is not supported yet"},
		{"an array pattern nested in an object's", "const o: { xs: number[] } = { xs: [1] };\nconst { xs: [a] } = o;\n", "array destructuring is not supported yet"},
		{"a rest element", "const o: { a: number; b: number } = { a: 1, b: 2 };\nconst { a, ...rest } = o;\n", "a rest element in destructuring is not supported yet"},
		{"a default", "const o: { a: number } = { a: 1 };\nconst { a = 2 } = o;\n", "a default in destructuring is not supported yet"},
		{"a computed key", "const o: { a: number } = { a: 1 };\nconst { [\"a\"]: z } = o;\n", "a computed property name is not supported yet"},
		{"a string", "const s: string = \"abc\";\nconst { length } = s;\n", "destructuring a string is not supported yet; only an object"},
		{"an array", "const xs: number[] = [1];\nconst { length } = xs;\n", "destructuring a number[] is not supported yet; only an object"},
		{"a nested string", "const o: { s: string } = { s: \"ab\" };\nconst { s: { length } } = o;\n", "destructuring a string is not supported yet; only an object"},
		{"a request", "import { createServer } from \"node:http\";\ncreateServer(({ url }, res) => {\n  res.end(url);\n});\n",
			"destructuring IncomingMessage, a node:http value, is not supported yet; read its members instead"},
		{"a library global, shorthand", "const o: { origin: string } = { origin };\n", "origin is a library global cakebear does not implement yet"},
		{"an exported let", "const o: { a: number } = { a: 1 };\nexport let { a } = o;\n", "exporting a let is not supported yet"},
		{"a wider object, shorthand", "interface P { x: number }\ninterface P3 { x: number; z: number }\nconst p: P3 = { x: 1, z: 2 };\nconst b: { p: P } = { p };\n",
			"a P3 can't be used as a P: an object has exactly the fields of its type in cakebear, and it has `z` too"},
		{"destructuring assignment", "const o: { a: number } = { a: 1 };\nlet a: number = 0;\n({ a } = o);\n", "destructuring assignment is not supported yet; declare the names with const or let"},
		{"a let exported by name", "const o: { a: number } = { a: 1 };\nlet { a } = o;\nexport { a };\n", "exporting a, a let, is not supported yet"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, tt.src)
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
			for _, e := range errs {
				if e.Kind != Unsupported {
					t.Errorf("%q is reported as an error; it is a limit of this phase", e.Msg)
				}
			}
		})
	}
}

// TestDestructuringAcrossModules: a name a top-level pattern declares is a
// top-level binding like any other, qualified with its module wherever it is
// read, imported or not, and so is the value the pattern holds.
func TestDestructuringAcrossModules(t *testing.T) {
	t.Parallel()

	p, errs := lowerFiles(t, map[string]string{
		"conf.ts": "export interface Conf { port: number; host: string }\nconst conf: Conf = { port: 8080, host: \"h\" };\n" +
			"export const { port, host: hostname } = conf;\nexport function next(): number {\n  return port + 1;\n}\n",
		"main.ts": "import type { Conf } from \"./conf\";\nimport { hostname, next } from \"./conf\";\n" +
			"const mine: Conf = { port: 1, host: \"m\" };\nconst { port } = mine;\nconsole.log(port, hostname, next());\n",
	}, "main.ts")
	if len(errs) > 0 {
		t.Fatalf("unexpected lowering errors: %v", errs)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	files := map[string]string{}
	for _, m := range p.Modules {
		files[m.File[strings.LastIndex(m.File, "/")+1:]] = m.File
	}
	for _, m := range p.Modules {
		ir.Inspect(m, func(node any) {
			id, ok := node.(*ir.Ident)
			if !ok {
				return
			}
			want := m.File
			if id.Name == "hostname" || (id.Name == "port" && strings.HasSuffix(m.File, "conf.ts")) {
				want = files["conf.ts"]
			}
			if id.Module != want {
				t.Errorf("in %s, %s names module %q, want %q", m.Name, id.Name, id.Module, want)
			}
		})
	}
}

// TestDestructuredBindingsAndTheRaceRule: a name a const pattern declares is a
// const, which a request handler may read when it holds a primitive; a let's
// is shared state, and so is an object read out of a const. A shorthand reads
// its variable as the name alone does, directly or in a function the handler
// calls, though the checker resolves the shorthand's name to the property.
func TestDestructuredBindingsAndTheRaceRule(t *testing.T) {
	t.Parallel()

	const conf = "interface Conf { greeting: string; inner: { s: string } }\nconst conf: Conf = { greeting: \"hi\", inner: { s: \"x\" } };\n"
	if _, errs := lowerSource(t, raceImport+conf+"const { greeting } = conf;\ncreateServer((req, res) => {\n  res.end(greeting);\n}).listen(3000);\n"); len(errs) > 0 {
		t.Errorf("a handler reading a destructured const string: %v", errs)
	}
	for _, tt := range []struct{ name, src, want string }{
		{"a let", "let { greeting } = conf;\ncreateServer((req, res) => {\n  res.end(greeting);\n}).listen(3000);\n",
			"can't read `greeting`, which can change while it runs"},
		{"an object", "const { inner } = conf;\ncreateServer((req, res) => {\n  res.end(inner.s);\n}).listen(3000);\n",
			"can't use `inner`, an object created outside it"},
		{"an enclosing function's pattern parameter", "function start({ greeting }: Conf): void {\n  createServer((req, res) => {\n    res.end(greeting);\n  }).listen(3000);\n}\nstart(conf);\n",
			"can't read `greeting`, which can change while it runs"},
		{"a shorthand of a shared let", "let hits: number = 0;\nhits = 1;\ncreateServer((req, res) => {\n  const o: { hits: number } = { hits };\n  console.log(o);\n  res.end(\"ok\");\n}).listen(3000);\n",
			"can't read `hits`, which can change while it runs"},
		{"a shorthand of an outer object", "createServer((req, res) => {\n  const o: { conf: Conf } = { conf };\n  res.end(o.conf.greeting);\n}).listen(3000);\n",
			"can't use `conf`, an object created outside it"},
		{"a function reading a let through a shorthand", "let hits: number = 0;\nfunction snap(): { hits: number } {\n  return { hits };\n}\nhits = 2;\ncreateServer((req, res) => {\n  console.log(snap());\n  res.end(\"ok\");\n}).listen(3000);\n",
			"can't call `snap()`, which reads `hits`, which can change while it runs"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, raceImport+conf+tt.src)
			if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
				t.Errorf("errors = %q, want one containing %q", msgs, tt.want)
			}
		})
	}
}
