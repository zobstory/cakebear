package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// funcNamed is the module's top-level function name.
func funcNamed(t *testing.T, m *ir.Module, name string) *ir.Func {
	t.Helper()
	for _, f := range m.Funcs {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no function %s", name)
	return nil
}

// nodesOf collects the nodes of type T in stmts, in Inspect's order.
func nodesOf[T any](stmts []ir.Stmt) []T {
	var out []T
	ir.Inspect(&ir.Module{Main: stmts}, func(node any) {
		if n, ok := node.(T); ok {
			out = append(out, n)
		}
	})
	return out
}

// TestMaybeValues: a T into a maybe's slot is wrapped, null and undefined are
// its empty states, and a maybe declared with no value is undefined. A union
// of literals of one type is that type (D10).
func TestMaybeValues(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `let a: string | undefined = "x";
let b: number | null = null;
let c: boolean | null | undefined;
const d: "get" | "post" = "get";
a = undefined;
b = 3;
const xs: (number | undefined)[] = [1, undefined];
xs.push(2, undefined);
interface Link { v: number; next: Link | undefined }
const list: Link = { v: 1, next: { v: 2, next: undefined } };
const ys: number[] | null = []; // typed by its slot alone
`)
	m := p.EntryModule()
	maybe := func(e ir.Type) ir.Type { return ir.MaybeOf(e) }
	if w, ok := initOf(t, m, 0).(*ir.Wrap); !ok || !w.Typ.Equal(maybe(ir.String)) {
		t.Errorf("a's value is %#v, want a string wrapped", initOf(t, m, 0))
	}
	if e, ok := initOf(t, m, 1).(*ir.Empty); !ok || !e.Null || !e.Typ.Equal(maybe(ir.Number)) {
		t.Errorf("b's value is %#v, want null", initOf(t, m, 1))
	}
	if e, ok := initOf(t, m, 2).(*ir.Empty); !ok || e.Null || !e.Typ.Equal(maybe(ir.Boolean)) {
		t.Errorf("c's value is %#v, want undefined, a declaration with none", initOf(t, m, 2))
	}
	if got := initOf(t, m, 3).ExprType(); got != ir.String {
		t.Errorf("a union of string literals is %s, want string", got)
	}
	assigns := nodesOf[*ir.Assign](m.Main)
	if len(assigns) != 2 {
		t.Fatalf("%d assignments, want 2", len(assigns))
	}
	if e, ok := assigns[0].Value.(*ir.Empty); !ok || e.Null {
		t.Errorf("a = undefined assigns %#v, want undefined", assigns[0].Value)
	}
	if _, ok := assigns[1].Value.(*ir.Wrap); !ok {
		t.Errorf("b = 3 assigns %#v, want 3 wrapped", assigns[1].Value)
	}
	// A literal in a maybe's slot is of what the maybe holds, wrapped.
	if lit, ok := initOf(t, m, 8).(*ir.ObjectLit); !ok || !isWrap(lit.Fields[1].Value) {
		t.Errorf("list is %#v, want its next a Link wrapped", initOf(t, m, 8))
	}
	if !isWrap(initOf(t, m, 9)) {
		t.Errorf("ys is %#v, want an array wrapped", initOf(t, m, 9))
	}
	push := nodesOf[*ir.ArrayOp](m.Main)
	if len(push) != 1 || len(push[0].Args) != 2 {
		t.Fatalf("push lowered as %v", push)
	}
	if _, ok := push[0].Args[0].(*ir.Wrap); !ok {
		t.Errorf("push's 2 is %#v, want it wrapped", push[0].Args[0])
	}
	if _, ok := push[0].Args[1].(*ir.Empty); !ok {
		t.Errorf("push's undefined is %#v, want undefined", push[0].Args[1])
	}
}

// TestMaybeNarrowing: a read the checker has narrowed to the element is
// unwrapped, and one it hasn't stays a maybe, field reads included (D4).
func TestMaybeNarrowing(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `interface Box { n: number | undefined }
function inc(x: number | undefined): number {
  if (x === undefined) {
    return 0;
  }
  return x + 1;
}
function field(b: Box): number {
  if (b.n !== undefined) {
    return b.n;
  }
  return -1;
}
function pass(x: number | undefined): number | undefined {
  return x;
}
`)
	m := p.EntryModule()
	inc := funcNamed(t, m, "inc")
	if us := nodesOf[*ir.Unwrap](inc.Body); len(us) != 1 || us[0].Typ != ir.Number {
		t.Errorf("inc unwraps %v, want x once, in x + 1", us)
	}
	field := funcNamed(t, m, "field")
	us := nodesOf[*ir.Unwrap](field.Body)
	if len(us) != 1 {
		t.Fatalf("field unwraps %v, want b.n once", us)
	}
	if _, ok := us[0].Value.(*ir.FieldGet); !ok {
		t.Errorf("field unwraps %#v, want the field read", us[0].Value)
	}
	if us := nodesOf[*ir.Unwrap](funcNamed(t, m, "pass").Body); len(us) != 0 {
		t.Errorf("pass unwraps %v, want x passed as the maybe it is", us)
	}
}

// TestMaybeComparisons: === and !== with undefined or null test the state
// they name, == and != with either test both, and a maybe compared with a T
// compares with the T wrapped (D12).
func TestMaybeComparisons(t *testing.T) {
	t.Parallel()

	// m comes from a call: TypeScript narrows a const by its initialiser.
	p := lowerProgramClean(t, `function get(): number | null | undefined {
  return 5;
}
const m: number | null | undefined = get();
const n: number = 5;
const isU: boolean = m === undefined;
const isNotN: boolean = m !== null;
const loose: boolean = m == null;
const flipped: boolean = undefined === m;
const five: boolean = m === 5;
const never: boolean = n === undefined;
`)
	m := p.EntryModule()
	cases := []struct {
		decl            int
		negated, u, nul bool
	}{
		{2, false, true, false},
		{3, true, false, true},
		{4, false, true, true},
		{5, false, true, false},
		{7, false, true, false},
	}
	for _, tt := range cases {
		x := initOf(t, m, tt.decl)
		if u, ok := x.(*ir.Unary); ok && tt.negated {
			x = u.Operand
		} else if tt.negated {
			t.Errorf("decl %d is %#v, want a negation", tt.decl, x)
			continue
		}
		e, ok := x.(*ir.IsEmpty)
		if !ok || e.Undefined != tt.u || e.Null != tt.nul {
			t.Errorf("decl %d is %#v, want IsEmpty{Undefined: %v, Null: %v}", tt.decl, x, tt.u, tt.nul)
		}
	}
	if e := initOf(t, m, 7).(*ir.IsEmpty); !isWrap(e.Value) {
		t.Errorf("n === undefined tests %#v, want n wrapped: still evaluated, never empty", e.Value)
	}
	b, ok := initOf(t, m, 6).(*ir.Binary)
	if !ok || !isWrap(b.Right) {
		t.Errorf("m === 5 is %#v, want m compared with 5 wrapped", initOf(t, m, 6))
	}
}

func isWrap(x ir.Expr) bool {
	_, ok := x.(*ir.Wrap)
	return ok
}

// TestMaybeReturns: a function returning a maybe returns undefined where it
// says `return;` and where it ends; one returning a T doesn't change.
func TestMaybeReturns(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `function f(n: number): number | undefined {
  if (n > 0) {
    return n;
  }
  while (n < 0) {
    return;
  }
}
const g = (n: number): string | undefined => {
  if (n > 0) {
    return "yes";
  }
};
async function h(): Promise<number | undefined> {
  return;
}
`)
	m := p.EntryModule()
	f := funcNamed(t, m, "f")
	rets := nodesOf[*ir.Return](f.Body)
	if len(rets) != 3 {
		t.Fatalf("f has %d returns, want n, the while's, and the end's", len(rets))
	}
	if !isWrap(rets[0].Value) {
		t.Errorf("return n is %#v, want n wrapped", rets[0].Value)
	}
	for _, r := range rets[1:] {
		if e, ok := r.Value.(*ir.Empty); !ok || e.Null {
			t.Errorf("a return gives %#v, want undefined", r.Value)
		}
	}
	lit := nodesOf[*ir.FuncLit](m.Main)
	if len(lit) != 1 {
		t.Fatalf("%d function literals, want g", len(lit))
	}
	if rets := nodesOf[*ir.Return](lit[0].Body); len(rets) != 2 {
		t.Errorf("g has %d returns, want its own and the end's", len(rets))
	}
	h := funcNamed(t, m, "h")
	if r := nodesOf[*ir.Return](h.Body); len(r) != 1 || !r[0].Value.ExprType().Equal(ir.MaybeOf(ir.Number)) {
		t.Errorf("h returns %v, want undefined as its promise's maybe", r)
	}
}

// TestMaybeUses: a maybe of a primitive in a template is ToString's
// "undefined" or "null", and console.log prints one.
func TestMaybeUses(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, "function get(): number | undefined {\n  return 1;\n}\n"+
		"const m: number | undefined = get();\nconst s: string = `m is ${m}`;\nconsole.log(m, s);\n")
	m := p.EntryModule()
	calls := nodesOf[*ir.BuiltinCall](m.Main)
	if len(calls) != 1 || calls[0].Fn != ir.ToString || calls[0].Args[0].ExprType().Kind != ir.KindMaybe {
		t.Errorf("the template converts %v, want m, a maybe, by ToString", calls)
	}
	logs := nodesOf[*ir.ConsoleLog](m.Main)
	if len(logs) != 1 || logs[0].Args[0].ExprType().Kind != ir.KindMaybe {
		t.Errorf("console.log is %v, want m printed as the maybe it is", logs)
	}
}

func TestMaybeRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"a number or a string", "const x: number | string = 1;\n", "a union of a string and a number is not supported yet"},
		{"objects", "interface A { a: number }\ninterface B { b: number }\nconst x: A | B = { a: 1 };\n", "unions of object types are not supported yet"},
		{"only null", "const x: null = null;\n", "a type that is only null is not supported yet"},
		{"only undefined", "let x: undefined = undefined;\n", "a type that is only undefined is not supported yet"},
		{"only null or undefined", "let x: null | undefined = null;\n", "a type that is only null or undefined is not supported yet"},
		{"void", "function f(): void | undefined {\n  return;\n}\n", "a union with void is not supported yet"},
		{"null with undefined", "const b: boolean = null === undefined;\n", "comparing null or undefined with null or undefined is not supported yet"},
		{"maybe functions", "const f: (() => void) | undefined = undefined;\nconst g: (() => void) | undefined = undefined;\nconst b: boolean = f === g;\n", "on a"},
		{"join", "const xs: (number | undefined)[] = [];\nconst s: string = xs.join();\n", "join of an array of values that may be null or undefined is not supported yet"},
		{"sort", "const xs: (number | undefined)[] = [];\nxs.sort((a, b) => 0);\n", "Node moves undefined to the end without calling the comparator"},
		{"calling a maybe", "function get(): (() => number) | undefined {\n  return undefined;\n}\nconst f: (() => number) | undefined = get();\nif (f !== undefined) {\n  const n: number = f();\n}\n",
			"calling a function that may be null or undefined is not supported yet"},
		{"filter narrowing", "const xs: (number | undefined)[] = [];\nconst ys: number[] = xs.filter((x) => x !== undefined);\n",
			"filter narrowing a (number | null | undefined)[] to a number[] is not supported yet"},
		{"a maybe string as a format", "function get(): string | undefined {\n  return \"%d\";\n}\nconsole.log(get(), 1);\n",
			"a string before console.log's other arguments is a format to Node"},
		{"printing a maybe function", "function get(): (() => void) | undefined {\n  return undefined;\n}\nconsole.log(get());\n", "console.log cannot print"},
		{"a maybe space", "function get(): number | undefined {\n  return 2;\n}\nconst s: string = JSON.stringify([1], null, get());\n",
			"a space that may be null or undefined is not supported yet"},
		{"readonly into mutable, as maybes", "interface R { readonly path: string }\ninterface M { path: string }\n" +
			"function get(): R | undefined {\n  return { path: \"/\" };\n}\nconst r: R | undefined = get();\nconst m: M | undefined = r;\n",
			"cakebear refuses a readonly value where a mutable one is wanted"},
		{"JSON of undefined", "function get(): number | undefined {\n  return 1;\n}\nconst s: string = JSON.stringify(get());\n", "JSON.stringify of a value that may be undefined is not supported"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}

// TestMaybeArguments: an argument into a maybe parameter is wrapped, or
// empty, whether the call is to a function, a function value or resolve,
// whose parameter the checker types `T | PromiseLike<T>`, no maybe.
func TestMaybeArguments(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `async function main(): Promise<void> {
  const p: Promise<number | undefined> = new Promise((resolve) => {
    resolve(5);
  });
  const q: Promise<number | undefined> = new Promise((resolve) => {
    resolve(undefined);
  });
  const a: number | undefined = await p;
  const b: number | undefined = await q;
}
main();
const f = (x: number | undefined): boolean => x === undefined;
const a: boolean = f(1);
const b: boolean = f(undefined);
function g(h: (x: string | null) => void): void {
  h("s");
  h(null);
}
`)
	var calls int
	ir.Inspect(p.EntryModule(), func(node any) {
		c, ok := node.(*ir.Call)
		if !ok || len(c.Args) != 1 {
			return
		}
		calls++
		switch c.Args[0].(type) {
		case *ir.Wrap, *ir.Empty:
		default:
			t.Errorf("%s's argument is %#v, want it wrapped or empty", c.Callee, c.Args[0])
		}
	})
	if calls != 6 {
		t.Errorf("%d calls of one argument, want 6", calls)
	}
}

// TestMaybeRaceRule: a request handler may read a const maybe of a primitive
// (D11), and not a let one, nor a const maybe of a function, which the rule
// can't see into.
func TestMaybeRaceRule(t *testing.T) {
	t.Parallel()

	const get = "function get(): number | undefined {\n  return 1;\n}\n"
	handler := func(top, body string) string {
		return raceImport + get + top + "const server = createServer((req, res) => {\n" + body + "  res.end(\"ok\");\n});\nserver.listen(3000);\n"
	}
	lowerClean(t, handler("const m: number | undefined = get();\n", "  const n: boolean = m === undefined;\n"))
	for _, tt := range []struct{ name, src, want string }{
		{"a let maybe", handler("let m: number | undefined = get();\n", "  const n: boolean = m === undefined;\n"), "can't read `m`, which can change while it runs"},
		{"a const maybe of an object", handler("interface P { x: number }\nfunction make(): P | undefined {\n  return { x: 1 };\n}\nconst p: P | undefined = make();\n",
			"  const n: boolean = p === undefined;\n"), "`p`, an object created outside it"},
		{"a const maybe of a function", handler("function make(): (() => void) | undefined {\n  return undefined;\n}\nconst f: (() => void) | undefined = make();\n",
			"  const n: boolean = f === undefined;\n"), "`f`, which may hold a function; one that may be null or undefined can't be shared between requests yet"},
	} {
		_, errs := lowerSource(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) || !strings.Contains(msgs, "handler") {
			t.Errorf("%s: errors %q, want the handler refused for %s", tt.name, msgs, tt.want)
		}
	}
}

// TestMaybeFrozenConsts: a const readonly all the way down may hold maybes,
// and a request handler may read it (D11).
func TestMaybeFrozenConsts(t *testing.T) {
	t.Parallel()

	lowerClean(t, raceImport+`interface Route { readonly path: string; readonly title: string | undefined }
const routes: readonly Route[] = [{ path: "/", title: undefined }, { path: "/a", title: "A" }];
const home: Route | undefined = { path: "/h", title: "H" };
const server = createServer((req, res) => {
  const untitled: boolean = routes[0].title === undefined && home !== undefined;
  res.end("ok");
});
server.listen(3000);
`)
}
