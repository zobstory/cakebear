package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestJSONStringify (D15): JSON.stringify lowers to its own node, with the
// space when there is one, a null or undefined replacer accepted only as the
// way to reach it, and a string result.
func TestJSONStringify(t *testing.T) {
	t.Parallel()

	m := lowerClean(t, `interface Point { x: number; y: number }
const p: Point = { x: 1, y: 2 };
const a: string = JSON.stringify(p);
const b: string = JSON.stringify([p, p], null, 2);
const c: string = JSON.stringify(p, undefined, "\t");
const width: i32 = i32(4);
const d: string = JSON.stringify("text", null, width);
console.log(JSON.stringify(true));
`)
	for i, want := range []struct {
		value string // the value's IR type
		space ir.Type
	}{
		{"Point", ir.Invalid},
		{"Point[]", ir.Number},
		{"Point", ir.String},
		{"string", ir.Int32},
	} {
		d := m.Main[i+1]
		if i == 3 {
			d = m.Main[5]
		}
		j, ok := d.(*ir.VarDecl).Init.(*ir.JSONStringify)
		if !ok {
			t.Fatalf("declaration %d is initialised by %T, want a JSONStringify", i, d.(*ir.VarDecl).Init)
		}
		if got := j.Value.ExprType().String(); !strings.Contains(got, want.value) {
			t.Errorf("JSON.stringify %d serialises a %s, want a %s", i, got, want.value)
		}
		switch {
		case want.space == ir.Invalid && j.Space != nil:
			t.Errorf("JSON.stringify %d has a space, %v, where the call gives none", i, j.Space)
		case want.space != ir.Invalid && (j.Space == nil || !j.Space.ExprType().Equal(want.space)):
			t.Errorf("JSON.stringify %d's space is %v, want a %s", i, j.Space, want.space)
		}
		if !j.ExprType().Equal(ir.String) {
			t.Errorf("JSON.stringify %d is a %s, want a string", i, j.ExprType())
		}
	}
	logged := m.Main[6].(*ir.ExprStmt).Expr.(*ir.ConsoleLog).Args[0]
	if _, ok := logged.(*ir.JSONStringify); !ok {
		t.Errorf("console.log's argument is %T, want a JSONStringify", logged)
	}
}

func TestJSONRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"a replacer function", "const o: { a: number } = { a: 1 };\nconst s: string = JSON.stringify(o, (k: string, v: number): number => v);\n",
			"a replacer is not supported yet; pass null to give a space"},
		{"a replacer array", "const o: { a: number } = { a: 1 };\nconst s: string = JSON.stringify(o, [\"a\"]);\n", "a replacer is not supported yet"},
		{"a function", "const f = (): number => 1;\nconst s: string = JSON.stringify(f);\n",
			"JSON.stringify of a function is not supported yet: Node gives undefined"},
		{"an object holding a function", "const o: { f: () => number } = { f: (): number => 1 };\nconst s: string = JSON.stringify(o);\n",
			"JSON.stringify cannot serialise a { f: () => number } yet: it holds a function"},
		{"an array of functions", "const fs: (() => number)[] = [(): number => 1];\nconst s: string = JSON.stringify(fs);\n", "it holds a function"},
		{"a promise", "const p: Promise<number> = new Promise((resolve) => {\n  resolve(1);\n});\nconst s: string = JSON.stringify(p);\n",
			"JSON.stringify cannot serialise a promise yet"},
		{"a request", "import { createServer } from \"node:http\";\ncreateServer((req, res) => {\n  res.end(JSON.stringify(req));\n});\n",
			"JSON.stringify cannot serialise an IncomingMessage yet"},
		{"JSON.parse", "const n: number = JSON.parse(\"1\");\n", "JSON.parse is not supported yet: it returns any, which cakebear doesn't lower"},
		{"another member", "console.log(JSON.rawJSON(\"1\"));\n", "JSON.rawJSON is not supported yet; only JSON.stringify is"},
		{"a member added to JSON", "export {};\ndeclare global {\n  interface JSON {\n    extra(): string;\n  }\n}\nconsole.log(JSON.extra());\n",
			"JSON.extra is added to JSON outside TypeScript's libs, which is not supported"},
		{"a shared object in a handler", "import { createServer } from \"node:http\";\nconst page: { title: string } = { title: \"home\" };\ncreateServer((req, res) => {\n  res.end(JSON.stringify(page));\n});\n",
			"can't use `page`, an object created outside it"},
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

// TestJSONInAHandler: the live test's pattern, a handler serialising an
// object it builds itself, is fine, and a program's own JSON is no longer
// JSON: calling a member of it is an ordinary (here unsupported) call.
func TestJSONInAHandler(t *testing.T) {
	t.Parallel()

	lowerClean(t, raceImport+`interface Page { title: string; hits: number[] }
function page(title: string): Page {
  return { title: title, hits: [1, 2] };
}
createServer((req, res) => {
  res.setHeader("content-type", "application/json");
  res.end(JSON.stringify(page(req.url), null, 2));
}).listen(3000);
`)
	_, errs := lowerSource(t, "export {};\nconst JSON: { stringify: (n: number) => string } = { stringify: (n: number): string => \"x\" };\nconst s: string = JSON.stringify(1);\n")
	if msgs := joinMsgs(errs); !strings.Contains(msgs, "only calls to a function by name are supported yet") {
		t.Errorf("a program's own JSON: errors = %q, want it treated as the program's", msgs)
	}
}
