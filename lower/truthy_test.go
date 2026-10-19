package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

const truthySrc = `interface User { name: string; age: number | null }
function count(n: number): number {
  return n;
}
function user(): User | undefined {
  return undefined;
}
function maybe(): number | undefined {
  return 2;
}
const n: number = count(1);
const s: string = "x";
const flag: boolean = n > 0;
const m: number | undefined = maybe();
const u: User | undefined = user();
`

// TestTruthyConditions: a condition that isn't a boolean is its truthiness,
// and `&&`, `||` and `!` in one are tested part by part, with no value of
// either operand (D7); a boolean is itself.
func TestTruthyConditions(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, truthySrc+`if (n) {
  console.log(1);
}
while (s) {
  break;
}
const c: string = m ? "a" : "b";
const not: boolean = !u;
if (u && u.name) {
  console.log(2);
}
if (flag) {
  console.log(3);
}
`)
	ifs := nodesOf[*ir.If](p.EntryModule().Main)
	if len(ifs) != 3 {
		t.Fatalf("%d ifs, want 3", len(ifs))
	}
	if _, ok := ifs[0].Cond.(*ir.Truthy); !ok {
		t.Errorf("if (n) tests %#v, want n's truthiness", ifs[0].Cond)
	}
	b, ok := ifs[1].Cond.(*ir.Binary)
	if !ok || b.Op != ir.OpAnd {
		t.Fatalf("if (u && u.name) tests %#v, want an && of truthinesses", ifs[1].Cond)
	}
	for _, side := range []ir.Expr{b.Left, b.Right} {
		if _, ok := side.(*ir.Truthy); !ok {
			t.Errorf("a side of u && u.name is %#v, want its truthiness", side)
		}
	}
	if len(nodesOf[*ir.Cond](p.EntryModule().Main[9:10])) != 0 {
		t.Error("if (u && u.name) builds a value of the && to test it")
	}
	if _, ok := ifs[2].Cond.(*ir.Ident); !ok {
		t.Errorf("if (flag) tests %#v, want flag itself", ifs[2].Cond)
	}
	if w := nodesOf[*ir.While](p.EntryModule().Main); len(w) != 1 || !isTruthy(w[0].Cond) {
		t.Errorf("while (s) is %v, want s's truthiness", w)
	}
	if cond, _ := condOf(t, initOf(t, p.EntryModule(), 7)); !isTruthy(cond.Test) {
		t.Errorf("m ? … tests %#v, want m's truthiness", cond.Test)
	}
	if not, ok := initOf(t, p.EntryModule(), 8).(*ir.Unary); !ok || !isTruthy(not.Operand) {
		t.Errorf("!u is %#v, want the negation of u's truthiness", initOf(t, p.EntryModule(), 8))
	}
}

func isTruthy(x ir.Expr) bool {
	_, ok := x.(*ir.Truthy)
	return ok
}

// TestLogicalValues: `x || y` and `x && y` as values, where an operand isn't
// a boolean, are one of the operands, chosen by x's truthiness, x held where
// it is a call; between booleans they stay a boolean operator.
func TestLogicalValues(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, truthySrc+`const or: number = n || 5;
const called: number = count(0) || 5;
const def: number = m || 0;
const name: string | undefined = u && u.name;
const both: boolean = flag || n > 1;
`)
	m := p.EntryModule()
	cond, let := condOf(t, initOf(t, m, 5))
	if let != nil || !isTruthy(cond.Test) {
		t.Errorf("n || 5 is %#v, want a Cond on n's truthiness, n read again", cond)
	}
	if _, let := condOf(t, initOf(t, m, 6)); let == nil {
		t.Error("count(0) || 5 calls count again for its value; it must be held in a Let")
	}
	if cond, _ := condOf(t, initOf(t, m, 7)); cond.Typ != ir.Number {
		t.Errorf("m || 0 is a %s, want a number", cond.Typ)
	} else if _, ok := cond.Then.(*ir.Unwrap); !ok {
		t.Errorf("m || 0 keeps m as %#v, want it unwrapped: a truthy m has a value", cond.Then)
	}
	and, _ := condOf(t, initOf(t, m, 8))
	if !isWrap(and.Then) {
		t.Errorf("u && u.name gives the name as %#v, want it wrapped", and.Then)
	}
	if kept, ok := and.Else.(*ir.Cond); !ok || !isEmptyTest(kept.Test) {
		t.Errorf("u && u.name keeps an empty u as %#v, want its null or undefined", and.Else)
	} else if null, ok := kept.Then.(*ir.Empty); !ok || !null.Null {
		t.Errorf("u && u.name gives a null u as %#v, want null", kept.Then)
	}
	if b, ok := initOf(t, m, 9).(*ir.Binary); !ok || b.Op != ir.OpOr {
		t.Errorf("flag || n > 1 is %#v, want a boolean ||", initOf(t, m, 9))
	}
}

func isEmptyTest(x ir.Expr) bool {
	_, ok := x.(*ir.IsEmpty)
	return ok
}

// TestBooleanCallback: Boolean passed as a callback is a function giving its
// first argument's truthiness, of the slot's signature.
func TestBooleanCallback(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, "const xs: (number | undefined)[] = [0, 1];\nconst ys: (number | undefined)[] = xs.filter(Boolean);\nconst any: boolean = xs.some(Boolean);\n")
	ops := nodesOf[*ir.ArrayOp](p.EntryModule().Main)
	if len(ops) != 2 {
		t.Fatalf("%d array ops, want filter and some", len(ops))
	}
	for _, op := range ops {
		lit, ok := op.Args[0].(*ir.FuncLit)
		if !ok || len(lit.Params) != 3 || lit.Result != ir.Boolean {
			t.Fatalf("%s's callback is %#v, want a (value, index, array) => boolean", op.Method, op.Args[0])
		}
		ret, ok := lit.Body[0].(*ir.Return)
		if !ok || !isTruthy(ret.Value) {
			t.Fatalf("%s's Boolean returns %#v, want the value's truthiness", op.Method, lit.Body)
		}
		if id, ok := ret.Value.(*ir.Truthy).Value.(*ir.Ident); !ok || id.Name != lit.Params[0].Name || id.Name == "" {
			t.Errorf("%s's Boolean tests %#v, want its first parameter, %q", op.Method, ret.Value, lit.Params[0].Name)
		}
	}
}

const spreadSrc = `interface Opts { a: number; label?: string; tags?: string[] }
function count(n: number): number {
  return n;
}
const c: boolean = count(1) > 0;
`

// TestConditionalSpread: a literal with conditional spreads is a Cond on each
// test, between literals keeping their keys in source order; a value before
// a later test is held, where it is a call.
func TestConditionalSpread(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, spreadSrc+`const o: Opts = { a: 1, ...(c && { label: "yes" }) };
const p: Opts = { a: count(2), ...(c ? { tags: ["t"] } : {}), label: "z" };
`)
	m := p.EntryModule()
	cond, let := condOf(t, initOf(t, m, 1))
	if let != nil {
		t.Errorf("{ a: 1, … } holds 1 in %q; a literal is read again", let.Name)
	}
	then, ok1 := cond.Then.(*ir.ObjectLit)
	els, ok2 := cond.Else.(*ir.ObjectLit)
	if !ok1 || !ok2 || len(then.Fields) != 2 || len(els.Fields) != 1 {
		t.Fatalf("o is %#v, want { a, label } or { a }", cond)
	}
	if then.Typ.Shape.Fields[then.Fields[1].Field].Name != "label" {
		t.Errorf("o's spread sets %s, want label after a", then.Typ.Shape.Fields[then.Fields[1].Field].Name)
	}
	cond, let = condOf(t, initOf(t, m, 2))
	if let == nil {
		t.Error("count(2) before the spread isn't held; it must run before c is tested")
	}
	for _, branch := range []ir.Expr{cond.Then, cond.Else} {
		lit := branch.(*ir.ObjectLit)
		if last := lit.Fields[len(lit.Fields)-1]; lit.Typ.Shape.Fields[last.Field].Name != "label" {
			t.Errorf("a branch of p ends with %s, want label, written last", lit.Typ.Shape.Fields[last.Field].Name)
		}
	}
}

// TestSpreadWithoutAType: an unannotated literal's spread key is optional in
// the type TypeScript gives the binding, when the test may not hold.
func TestSpreadWithoutAType(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, spreadSrc+"const q = { a: 1, ...(c && { k: \"x\" }) };\n")
	if f := initOf(t, p.EntryModule(), 1).ExprType().Shape.Fields; len(f) != 2 || !f[1].Optional {
		t.Errorf("q's shape is %v, want k optional", initOf(t, p.EntryModule(), 1).ExprType())
	}
}

func TestSpreadRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"a plain spread", spreadSrc + "const o: Opts = { a: 1 };\nconst p: Opts = { ...o };\n", "but for a conditional spread of literals"},
		{"a spread of a variable", spreadSrc + "const l: Opts = { a: 1 };\nconst p: Opts = { a: 2, ...(c && l) };\n", "but for a conditional spread of literals"},
		{"a key set twice", spreadSrc + "const p: Opts = { a: 1, label: \"a\", ...(c && { label: \"b\" }) };\n", "a property set by a spread and elsewhere in the same literal, label"},
		{"a spread whose test always holds", spreadSrc + "const q = { a: 1, ...(true && { k: \"x\" }) };\n", "a spread whose test always holds, setting k"},
		{"a spread in a spread", spreadSrc + "const p: Opts = { a: 1, ...(c && { ...(c && { label: \"x\" }) }) };\n", "a spread inside a spread literal"},
		{"too many spreads", spreadSrc + "const p: Opts = { a: 1, ...(c && {}), ...(c && {}), ...(c && {}), ...(c && {}), ...(c && {}) };\n", "more than 4 conditional spreads"},
		{"a truthiness callback", "const xs: number[] = [];\nconst ys: number[] = xs.filter((x) => x);\n", "return !!x, or pass Boolean"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}
