package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestThrowAndTryLowering: throw, try/catch/finally and the catch binding,
// an unknown, narrowed by instanceof into an Error whose message is read.
func TestThrowAndTryLowering(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `function fail(m: string): void {
  throw new Error(m);
}
try {
  fail("x");
} catch (e) {
  if (e instanceof TypeError) {
    console.log(e.message);
  }
} finally {
  console.log("done");
}
try {
  fail("y");
} catch {
  console.log("caught");
}
`)
	m := p.EntryModule()
	throw, ok := m.Funcs[0].Body[0].(*ir.Throw)
	if !ok {
		t.Fatalf("fail's body is %#v, want a Throw", m.Funcs[0].Body)
	}
	if ne, ok := throw.Value.(*ir.NewError); !ok || ne.Kind != ir.ErrorBase || ne.Message == nil || ne.Message.ExprType() != ir.String {
		t.Errorf("throw new Error(m) throws %#v", throw.Value)
	}

	tries := nodesOf[*ir.Try](m.Main)
	if len(tries) != 2 {
		t.Fatalf("%d trys, want 2", len(tries))
	}
	first := tries[0]
	if first.Catch == nil || first.Catch.Name != "e" || first.Finally == nil || len(first.Body) != 1 {
		t.Errorf("try/catch (e)/finally is %#v", first)
	}
	if second := tries[1]; second.Catch == nil || second.Catch.Name != "" || second.Finally != nil {
		t.Errorf("try/catch {} is %#v, want a catch binding nothing and no finally", second)
	}
	is := nodesOf[*ir.InstanceOf](m.Main)
	if len(is) != 1 || is[0].Kind != ir.ErrorType || is[0].Value.ExprType() != ir.Unknown {
		t.Errorf("e instanceof TypeError is %#v, want an InstanceOf of the unknown e", is)
	}
	gets := nodesOf[*ir.ErrorGet](m.Main)
	if len(gets) != 1 || gets[0].Prop != ir.ErrorMessage {
		t.Fatalf("e.message is %#v, want an ErrorGet of message", gets)
	}
	if u, ok := gets[0].Error.(*ir.Unbox); !ok || u.Typ != ir.Error || u.Value.ExprType() != ir.Unknown {
		t.Errorf("the e that e.message reads is %#v, want e unboxed to the Error instanceof narrowed it to", gets[0].Error)
	}
}

// TestErrorValues: each built-in kind, with new or without, a message or
// none, and Error as a type: a maybe of one, a field, a property read.
func TestErrorValues(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `interface Result {
  error: Error | null;
}
const a: Error = new TypeError("t");
const b: Error = RangeError("r");
const c: Error = new Error();
const d: Error = new URIError(undefined);
const r: Result = { error: a };
const s: string | undefined = c.stack;
const n: string = a.name;
const t: string = a.toString();
if (r.error !== null) {
  console.log(r.error.message);
}
`)
	m := p.EntryModule()
	for i, want := range []struct {
		kind    ir.ErrorKind
		message bool
	}{{ir.ErrorType, true}, {ir.ErrorRange, true}, {ir.ErrorBase, false}, {ir.ErrorURI, false}} {
		ne, ok := initOf(t, m, i).(*ir.NewError)
		if !ok || ne.Kind != want.kind || (ne.Message != nil) != want.message {
			t.Errorf("declaration %d is %#v, want a new %s with a message %v", i, initOf(t, m, i), want.kind, want.message)
		}
	}
	if s, ok := initOf(t, m, 5).(*ir.ErrorGet); !ok || s.Prop != ir.ErrorStack || !s.ExprType().Equal(ir.MaybeOf(ir.String)) {
		t.Errorf("c.stack is %#v, want an ErrorGet of stack, a string | undefined", initOf(t, m, 5))
	}
	if g, ok := initOf(t, m, 6).(*ir.ErrorGet); !ok || g.Prop != ir.ErrorName {
		t.Errorf("a.name is %#v", initOf(t, m, 6))
	}
	if c, ok := initOf(t, m, 7).(*ir.BuiltinCall); !ok || c.Fn != ir.ToStringMethod || c.Args[0].ExprType() != ir.Error {
		t.Errorf("a.toString() is %#v, want the toString method of the Error, whose receiver is read", initOf(t, m, 7))
	}
	field := p.Shapes[0].Fields[0]
	if field.Name != "error" || !field.Type.Equal(ir.MaybeOf(ir.Error)) {
		t.Errorf("Result's field is %s: %s, want error: Error | null | undefined", field.Name, field.Type)
	}
	gets := nodesOf[*ir.ErrorGet](m.Main)
	if last := gets[len(gets)-1]; last.Prop != ir.ErrorMessage {
		t.Errorf("r.error.message is %#v", last)
	} else if _, ok := last.Error.(*ir.Unwrap); !ok {
		t.Errorf("the r.error that .message reads is %#v, want it unwrapped where !== null narrowed it", last.Error)
	}
}

// TestUnknownFlows: a value into an unknown's slot is boxed, a read the
// checker has narrowed is unboxed, and typeof, === and the tests against
// null work on an unknown as it is.
func TestUnknownFlows(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `function show(x: unknown): string {
  if (typeof x === "string") {
    return x;
  }
  return String(x);
}
const u: unknown = 5;
let v: unknown = "s";
v = 3;
console.log(0, show(u), show(true), v === 3, v !== null, typeof v !== "number", `+"`${v}`"+`);
`)
	m := p.EntryModule()
	show := m.Funcs[0].Body
	if tests := nodesOf[*ir.TypeOf](show); len(tests) != 1 || tests[0].Tag != ir.TagString {
		t.Errorf("typeof x === \"string\" is %#v", tests)
	}
	ret := nodesOf[*ir.Return](show)
	if u, ok := ret[0].Value.(*ir.Unbox); !ok || u.Typ != ir.String {
		t.Errorf("return x after typeof narrowed it is %#v, want x unboxed to a string", ret[0].Value)
	}
	if c, ok := ret[1].Value.(*ir.BuiltinCall); !ok || c.Fn != ir.ToString || c.Args[0].ExprType() != ir.Unknown {
		t.Errorf("String(x) is %#v, want ToString of the unknown", ret[1].Value)
	}
	if b, ok := initOf(t, m, 0).(*ir.Box); !ok || b.Value.ExprType() != ir.Number {
		t.Errorf("const u: unknown = 5 is %#v, want 5 boxed", initOf(t, m, 0))
	}
	if a, ok := m.Main[2].(*ir.Assign); !ok || !isBox(a.Value) {
		t.Errorf("v = 3 is %#v, want 3 boxed", m.Main[2])
	}
	args := nodesOf[*ir.ConsoleLog](m.Main)[0].Args
	if call, ok := args[2].(*ir.Call); !ok || !isBox(call.Args[0]) {
		t.Errorf("show(true) is %#v, want true boxed into the parameter", args[2])
	}
	if eq, ok := args[3].(*ir.Binary); !ok || eq.Op != ir.OpEqual || eq.Left.ExprType() != ir.Unknown || !isBox(eq.Right) {
		t.Errorf("v === 3 is %#v, want v === 3 boxed", args[3])
	}
	if not, ok := args[4].(*ir.Unary); !ok || not.Op != ir.OpNot {
		t.Errorf("v !== null is %#v, want the negation of a test", args[4])
	} else if e, ok := not.Operand.(*ir.IsEmpty); !ok || !e.Null || e.Undefined || e.Value.ExprType() != ir.Unknown {
		t.Errorf("v !== null tests %#v, want the unknown itself tested for null, not wrapped in a maybe", not.Operand)
	}
	if not, ok := args[5].(*ir.Unary); !ok || not.Op != ir.OpNot {
		t.Errorf("typeof v !== \"number\" is %#v", args[5])
	} else if test, ok := not.Operand.(*ir.TypeOf); !ok || test.Tag != ir.TagNumber {
		t.Errorf("typeof v !== \"number\" tests %#v", not.Operand)
	}
	if c, ok := args[6].(*ir.Concat); !ok || len(c.Parts) != 1 {
		t.Errorf("a template of v is %#v", args[6])
	} else if b, ok := c.Parts[0].(*ir.BuiltinCall); !ok || b.Fn != ir.ToString || b.Args[0].ExprType() != ir.Unknown {
		t.Errorf("${v} is %#v, want ToString of the unknown", c.Parts[0])
	}
}

// TestErrorEdges: an extension type is thrown and boxed as the number it is,
// a try with an empty finally and no catch is still a try, a narrowed stack
// is unwrapped, and a program's own function named like a kind is an
// ordinary call.
func TestErrorEdges(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `export {};
function RangeError(m: string): string {
  return m + "!";
}
const n: i32 = i32(5);
const u: unknown = n;
const e: Error = new Error("m");
try {
  console.log(RangeError("x"));
} finally {
}
if (e.stack !== undefined) {
  const st: string = e.stack;
  console.log(st);
}
throw n;
`)
	m := p.EntryModule()
	if b, ok := initOf(t, m, 1).(*ir.Box); !ok || b.Value.ExprType() != ir.Number {
		t.Errorf("an i32 into an unknown is %#v, want it widened to a number, then boxed", initOf(t, m, 1))
	}
	if calls := nodesOf[*ir.Call](m.Main); len(calls) != 1 || calls[0].Callee != "RangeError" {
		t.Errorf("the program's own RangeError(\"x\") is %#v, want an ordinary call", calls)
	}
	if news := nodesOf[*ir.NewError](m.Main); len(news) != 1 {
		t.Errorf("%d NewErrors, want only new Error(\"m\")", len(news))
	}
	if tries := nodesOf[*ir.Try](m.Main); len(tries) != 1 || tries[0].Finally == nil || tries[0].Catch != nil {
		t.Errorf("try {} finally {} is %#v, want a try with an empty finally", tries)
	}
	if unwraps := nodesOf[*ir.Unwrap](m.Main); len(unwraps) != 1 {
		t.Errorf("e.stack narrowed by !== undefined: %d unwraps, want 1", len(unwraps))
	} else if _, ok := unwraps[0].Value.(*ir.ErrorGet); !ok {
		t.Errorf("the unwrap is of %#v, want e.stack", unwraps[0].Value)
	}
	throw, ok := m.Main[len(m.Main)-1].(*ir.Throw)
	if !ok || throw.Value.ExprType() != ir.Number {
		t.Errorf("throw n, an i32, is %#v, want the number it is thrown", m.Main[len(m.Main)-1])
	}
}

func isBox(x ir.Expr) bool {
	_, ok := x.(*ir.Box)
	return ok
}

// TestUnknownSlots: an unknown field takes its value boxed, and so does a
// program's own callback slot, but nothing is boxed where a library's
// declaration says unknown for what cakebear types itself: Number.isInteger's
// argument, `some`'s and `find`'s predicates, a named predicate's adaptation.
func TestUnknownSlots(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `interface Outcome {
  detail: unknown;
}
function even(x: number): boolean {
  return x % 2 === 0;
}
function apply(f: () => unknown): void {
  console.log(String(f()));
}
const xs: number[] = [1, 2];
const r: Outcome = { detail: 5 };
const a: boolean = xs.some((x: number): boolean => x > 1);
const b: number | undefined = xs.find((x: number) => x > 1);
const c: boolean = xs.some(even);
const d: boolean = Number.isInteger(xs[0]);
const f: boolean = xs.some((x: number) => {
  return x > 1;
});
apply(() => 1);
console.log(JSON.stringify(r));
`)
	m := p.EntryModule()
	lit := nodesOf[*ir.ObjectLit](m.Main)
	if len(lit) != 1 || !isBox(lit[0].Fields[0].Value) {
		t.Errorf("{ detail: 5 } is %#v, want 5 boxed into the unknown field", lit)
	}
	for i := 2; i <= 6; i++ {
		if boxes := nodesOf[*ir.Box]([]ir.Stmt{m.Main[i]}); len(boxes) != 0 {
			t.Errorf("statement %d boxes %#v, where a library types the slot", i, boxes[0].Value)
		}
	}
	returns := nodesOf[*ir.Return](m.Main[7:8])
	if len(returns) != 1 || !isBox(returns[0].Value) {
		t.Errorf("a callback for the program's own () => unknown returns %#v, want 1 boxed", returns)
	}
	if _, errs := lowerSource(t, "const u: unknown = 1;\nconsole.log(JSON.stringify(u));\n"); !strings.Contains(joinMsgs(errs), "JSON.stringify of a value that may be undefined is not supported") {
		t.Errorf("JSON.stringify of an unknown, which may be undefined: errors = %q", joinMsgs(errs))
	}
}

// TestReturnsInATry: a function returning a maybe returns undefined from a
// bare return in any block of a try, as from one anywhere else.
func TestReturnsInATry(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `function g(c: boolean): string | undefined {
  try {
    if (c) {
      return;
    }
  } catch {
    return;
  } finally {
    console.log("g");
  }
}
console.log(g(true));
`)
	rets := nodesOf[*ir.Return](p.EntryModule().Funcs[0].Body)
	if len(rets) < 2 {
		t.Fatalf("g has %d returns", len(rets))
	}
	for i, r := range rets {
		if _, ok := r.Value.(*ir.Empty); !ok {
			t.Errorf("return %d is %#v, want undefined", i, r.Value)
		}
	}
}

// TestErrorRefusals: each is refused by name, as a limit of the phase.
func TestErrorRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"throwing a function", "throw (): number => 1;\n", "throwing a () => number is not supported"},
		{"a destructured catch", "try {\n} catch ({ message }: any) {\n  console.log(message);\n}\n",
			"destructuring what a catch binds is not supported yet"},
		{"a catch typed any", "try {\n} catch (e: any) {\n}\n", "a catch binding of type any is not supported"},
		{"a cause", "const e: Error = new Error(\"m\", { cause: 1 });\n", "Error's options, which carry a cause, are not supported yet"},
		{"a message that may be undefined", "function f(m: string | undefined): Error {\n  return new Error(m);\n}\n",
			"a message of type string | null | undefined is not supported yet"},
		{"instanceof a class", "const xs: number[] = [];\nconsole.log(xs instanceof Array);\n",
			"instanceof is supported only for Error and its built-in kinds yet"},
		{"instanceof on an array", "const xs: number[] = [];\nconsole.log(xs instanceof Error);\n",
			"instanceof on a number[] is not supported yet"},
		{"typeof of a number", "const n: number = 1;\nconsole.log(typeof n === \"number\");\n", "typeof of a number is not supported yet"},
		{"typeof against a variable", "const u: unknown = 1;\nconst s: string = \"number\";\nconsole.log(typeof u === s);\n",
			"comparing typeof with anything but a string literal is not supported yet"},
		{"a function as an unknown", "const f: unknown = (): number => 1;\n", "a () => number as an unknown is not supported"},
		{"any", "const a: any = 1;\n", "any is not supported yet"},
		{"an unknown first among console.log's arguments", "const u: unknown = \"%d\";\nconsole.log(u, 1);\n",
			"a string before console.log's other arguments is a format to Node"},
		{"nothing returned for an unknown", "function apply(f: () => unknown): void {\n}\napply(() => {\n  console.log(1);\n});\n",
			"this function returns nothing where unknown is expected"},
		// What agent m6-sweep found refused in words that misled, or not at all.
		{"an array into an unknown[]", "const a: number[] = [1];\nconst b: unknown[] = a;\n",
			"a number[] can't be used as an unknown[] yet: an array holds values of one type in cakebear"},
		{"a map's numbers into an unknown[]", "const a: number[] = [1];\nconst b: unknown[] = a.map((n) => n * 2);\n",
			"a number[] can't be used as an unknown[] yet"},
		{"never as ??'s default", "function fail(): never {\n  throw new Error(\"x\");\n}\nfunction f(m: string | undefined): string {\n  return m ?? fail();\n}\n",
			"a call that never returns, as the default of ??, is not supported yet"},
		{"never as a branch", "function fail(): never {\n  throw new Error(\"x\");\n}\nfunction f(c: boolean): number {\n  return c ? 1 : fail();\n}\n",
			"a call that never returns, as a branch of ?:, is not supported yet"},
		{"writing an Error's name", "const e: Error = new Error(\"m\");\ne.name = \"X\";\n",
			"assigning an Error's name is not supported yet: cakebear's Errors don't change once made"},
		{"|| on an unknown", "function get(): unknown {\n  return 0;\n}\nconst u: unknown = get() || \"d\";\n",
			"|| on an unknown as a value is not supported yet: narrow it first"},
		{"&& on an unknown", "function get(): unknown {\n  return 0;\n}\nconst u: unknown = get() && \"d\";\n",
			"&& on an unknown as a value is not supported yet"},
		{"typeof as a value", "const u: unknown = 1;\nconst s: string = typeof u;\n",
			"typeof as a value, not compared with a string literal, is not supported yet"},
		{"a handler reading a const unknown", "import { createServer } from \"node:http\";\nfunction get(): unknown {\n  return 1;\n}\n" +
			"const shared: unknown = get();\ncreateServer((req, res) => {\n  res.end(String(shared));\n});\n",
			"can't use `shared`, an unknown created outside it; one can't be shared between requests yet"},
		{"a spawn reading a const unknown", "function get(): unknown {\n  return 1;\n}\nconst shared: unknown = get();\n" +
			"const p: Promise<string> = spawn((): string => String(shared));\n",
			"can't use `shared`, an unknown created outside it; one can't be shared with a spawned function yet"},
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
