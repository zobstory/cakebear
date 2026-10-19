package backend

import (
	"errors"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// The members that may give undefined (M5 step 6, plan D9), built from IR
// by hand. What each program prints is what Node 24.18 printed for the
// TypeScript in its comment, the same over three runs.

// buildAndPanic builds p and runs it, wanting it to stop as a Go panic
// does, exit 2, with want on stderr. It returns what it printed first.
func buildAndPanic(t *testing.T, p *ir.Program, want string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "prog")
	if src, err := Build(p, Options{Output: out}); err != nil {
		t.Fatalf("Build: %v\n%s", err, src)
	}
	var stderr strings.Builder
	cmd := exec.Command(out)
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(stderr.String(), want) {
		t.Errorf("%v, stderr\n%s\nwant exit 2 and %q", err, stderr.String(), want)
	}
	return string(stdout)
}

// TestArrayMembersMayBeUndefinedMatchNode: at, find and findLast on arrays
// of numbers, strings, objects and maybes, and xs[i] where the read may be
// undefined (noUncheckedIndexedAccess, or an array of maybes): past the
// end, negative, fractional, and -0, which is the property "0". On an
// array of maybes, the element's own undefined is the read's.
//
//	interface Point { x: number; y: number }
//	const xs: number[] = [1, 2, 3];
//	console.log(xs.at(-1), xs.at(5), xs.at(1.7), xs.at(-4), xs.at(-1.5), xs.at(-0.5));
//	console.log(xs.find((x: number): boolean => x > 1), xs.findLast((x: number): boolean => x < 3), xs.find((x: number): boolean => x > 9));
//	const ss: string[] = ["a", "b"];
//	console.log(ss.at(0), ss.at(-3), ss.find((s: string): boolean => s === "b"), ss.findLast((s: string): boolean => s === "z"));
//	const ps: Point[] = [{ x: 1, y: 2 }, { x: 3, y: 4 }];
//	console.log(ps.at(-1), ps.find((p: Point): boolean => p.x > 5), ps.findLast((p: Point): boolean => p.x < 5) === ps[1]);
//	const ms: (number | undefined)[] = [1, undefined, 3];
//	console.log(ms.at(1), ms.at(2), ms.at(3), ms.find((m: number | undefined): boolean => m === undefined), ms.findLast((m: number | undefined): boolean => m !== undefined));
//	console.log(xs[0], xs[3], xs[-1], xs[1.5], xs[-0], ms[1], ms[2], ms[5]);
func TestArrayMembersMayBeUndefinedMatchNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	nums, numM := ir.ArrayOf(ir.Number), ir.MaybeOf(ir.Number)
	ptT := ir.ObjectOf(pt)
	xs, ss, ps, ms := stable("xs", nums), stable("ss", ir.ArrayOf(ir.String)), stable("ps", ir.ArrayOf(ptT)), stable("ms", ir.ArrayOf(numM))
	test := func(elem ir.Type, name string, body ir.Expr) *ir.FuncLit {
		return arrow(visit(elem, name, "", ""), ir.Boolean, ret(body))
	}
	x, sv, p, mv := stable("x", ir.Number), stable("s", ir.String), stable("p", ptT), stable("m", numM)
	at := func(arr ir.Expr, t ir.Type, i float64) ir.Expr { return method(ir.ArrayAt, arr, t, num(i)) }
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("xs", nums, arrayOf(ir.Number, num(1), num(2), num(3))),
		logAll(at(xs, numM, -1), at(xs, numM, 5), at(xs, numM, 1.7), at(xs, numM, -4), at(xs, numM, -1.5), at(xs, numM, -0.5)),
		logAll(method(ir.ArrayFind, xs, numM, test(ir.Number, "x", binary(ir.OpGreater, x, num(1), ir.Boolean))),
			method(ir.ArrayFindLast, xs, numM, test(ir.Number, "x", binary(ir.OpLess, x, num(3), ir.Boolean))),
			method(ir.ArrayFind, xs, numM, test(ir.Number, "x", binary(ir.OpGreater, x, num(9), ir.Boolean)))),
		decl("ss", ss.Typ, arrayOf(ir.String, str("a"), str("b"))),
		logAll(at(ss, ir.MaybeOf(ir.String), 0), at(ss, ir.MaybeOf(ir.String), -3),
			method(ir.ArrayFind, ss, ir.MaybeOf(ir.String), test(ir.String, "s", binary(ir.OpEqual, sv, str("b"), ir.Boolean))),
			method(ir.ArrayFindLast, ss, ir.MaybeOf(ir.String), test(ir.String, "s", binary(ir.OpEqual, sv, str("z"), ir.Boolean)))),
		decl("ps", ps.Typ, arrayOf(ptT, lit(pt, "x", num(1), "y", num(2)), lit(pt, "x", num(3), "y", num(4)))),
		logAll(at(ps, ir.MaybeOf(ptT), -1),
			method(ir.ArrayFind, ps, ir.MaybeOf(ptT), test(ptT, "p", binary(ir.OpGreater, get(p, "x"), num(5), ir.Boolean))),
			binary(ir.OpEqual,
				method(ir.ArrayFindLast, ps, ir.MaybeOf(ptT), test(ptT, "p", binary(ir.OpLess, get(p, "x"), num(5), ir.Boolean))),
				some(&ir.IndexGet{Array: ps, Index: num(1), Typ: ptT, Span: span}), ir.Boolean)),
		decl("ms", ms.Typ, arrayOf(numM, some(num(1)), undef(ir.Number), some(num(3)))),
		logAll(at(ms, numM, 1), at(ms, numM, 2), at(ms, numM, 3),
			method(ir.ArrayFind, ms, numM, test(numM, "m", isEmpty(mv, true, false))),
			method(ir.ArrayFindLast, ms, numM, test(numM, "m", &ir.Unary{Op: ir.OpNot, Operand: isEmpty(mv, true, false), Typ: ir.Boolean, Span: span}))),
		logAll(unchecked(xs, num(0), numM), unchecked(xs, num(3), numM), unchecked(xs, num(-1), numM), unchecked(xs, num(1.5), numM),
			unchecked(xs, num(math.Copysign(0, -1)), numM), unchecked(ms, num(1), numM), unchecked(ms, num(2), numM), unchecked(ms, num(5), numM)),
	}}
	want := "3 undefined 2 undefined 3 1\n2 2 undefined\na undefined b undefined\n{ x: 3, y: 4 } undefined true\n" +
		"undefined 3 undefined undefined 3\n1 undefined undefined undefined 1 undefined 3 undefined\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestPopAndShiftMatchNode: pop and shift as values and as statements, on
// arrays with elements and empty ones, seen through an alias, which is the
// same array; and on an array of maybes, whose null is the element's.
//
//	const xs: number[] = [1, 2, 3, 4];
//	const alias: number[] = xs;
//	console.log(xs.pop(), xs.shift(), alias);
//	xs.pop();
//	xs.shift();
//	console.log(xs, alias.length);
//	console.log(xs.pop(), xs.shift(), xs);
//	xs.shift();
//	xs.pop();
//	alias.push(5, 6);
//	console.log(xs.shift(), xs, alias);
//	const ms: (string | null)[] = [null, "b"];
//	console.log(ms.shift(), ms.pop(), ms.pop(), ms);
func TestPopAndShiftMatchNode(t *testing.T) {
	t.Parallel()

	nums, numM, strM := ir.ArrayOf(ir.Number), ir.MaybeOf(ir.Number), ir.MaybeOf(ir.String)
	xs, alias, ms := stable("xs", nums), stable("alias", nums), stable("ms", ir.ArrayOf(strM))
	pop := func(arr ir.Expr, t ir.Type) ir.Expr { return method(ir.ArrayPop, arr, t) }
	shift := func(arr ir.Expr, t ir.Type) ir.Expr { return method(ir.ArrayShift, arr, t) }
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("xs", nums, arrayOf(ir.Number, num(1), num(2), num(3), num(4))),
		decl("alias", nums, xs),
		logAll(pop(xs, numM), shift(xs, numM), alias),
		do(pop(xs, ir.Void)),
		do(shift(xs, ir.Void)),
		logAll(xs, method(ir.ArrayLength, alias, ir.Number)),
		logAll(pop(xs, numM), shift(xs, numM), xs),
		do(shift(xs, ir.Void)),
		do(pop(xs, ir.Void)),
		do(method(ir.ArrayPush, alias, ir.Number, num(5), num(6))),
		logAll(shift(xs, numM), xs, alias),
		decl("ms", ms.Typ, arrayOf(strM, null(ir.String), some(str("b")))),
		logAll(shift(ms, strM), pop(ms, strM), pop(ms, strM), ms),
	}}
	want := "4 1 [ 2, 3 ]\n[] 0\nundefined undefined []\n5 [ 6 ] [ 6 ]\nnull b undefined []\n"
	if got := buildAndRun(t, ir.ProgramOf(m)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestStringMembersMayBeUndefinedMatchNode: at, codePointAt and s[i] in
// code units: a pair's halves, read alone, print as U+FFFD, as Node writes
// a lone surrogate; codePointAt gives the pair's code point at its lead
// and the trail's own value at the trail. s[i] where the read may be
// undefined is undefined past the end, negative or fractional.
//
//	const s: string = "a😀b";
//	console.log(s.at(1), s.at(-1), s.codePointAt(1), s.codePointAt(2), s.codePointAt(9), s.at(9), s.codePointAt(-1));
//	console.log(s[0], s[3]);
//	console.log(s[1], s[9], s[-1], s[1.5]);   // under noUncheckedIndexedAccess
//	console.log(`${s.at(-2)}|${s.codePointAt(0)}`);
func TestStringMembersMayBeUndefinedMatchNode(t *testing.T) {
	t.Parallel()

	strM, numM := ir.MaybeOf(ir.String), ir.MaybeOf(ir.Number)
	s := stable("s", ir.String)
	at := func(i float64) ir.Expr { return strCall(ir.StrAt, strM, s, num(i)) }
	cp := func(i float64) ir.Expr { return strCall(ir.StrCodePointAt, numM, s, num(i)) }
	index := func(i float64) ir.Expr { return strCall(ir.StrIndex, ir.String, s, num(i)) }
	maybeIndex := func(i float64) ir.Expr { return strCall(ir.StrIndexOrUndefined, strM, s, num(i)) }
	toStr := func(x ir.Expr) ir.Expr {
		return &ir.BuiltinCall{Fn: ir.ToString, Args: []ir.Expr{x}, Typ: ir.String, Span: span}
	}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("s", ir.String, str("a😀b")),
		logAll(at(1), at(-1), cp(1), cp(2), cp(9), at(9), cp(-1)),
		logAll(index(0), index(3)),
		logAll(maybeIndex(1), maybeIndex(9), maybeIndex(-1), maybeIndex(1.5)),
		logNum(&ir.Concat{Parts: []ir.Expr{toStr(at(-2)), str("|"), toStr(cp(0))}, Span: span}),
	}}
	want := "� b 128512 56832 undefined undefined undefined\na b\n� undefined undefined undefined\n�|97\n"
	if got := buildAndRun(t, ir.ProgramOf(m)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestStringIndexPastTheEndPanics: s[i] typed string, which TypeScript
// reads unchecked, stops where Node reads undefined, as xs[i] does (M4 plan
// D8), saying so. Node prints undefined for each.
//
//	const s: string = "abc";
//	console.log(s[5]);   // and s[-1], s[1.5]
func TestStringIndexPastTheEndPanics(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		i    float64
		want string
	}{
		{5, "cakebear: index 5 is out of range for a string of length 3 (Node reads undefined)"},
		{-1, "cakebear: -1 is not a string index (Node reads undefined)"},
		{1.5, "cakebear: 1.5 is not a string index (Node reads undefined)"},
	} {
		m := &ir.Module{Name: "t", Main: []ir.Stmt{
			decl("s", ir.String, str("abc")),
			logNum(strCall(ir.StrIndex, ir.String, stable("s", ir.String), num(tt.i))),
		}}
		if out := buildAndPanic(t, ir.ProgramOf(m), tt.want); out != "" {
			t.Errorf("s[%v] printed %q before the panic", tt.i, out)
		}
	}
}

// TestFindCallbacksMatchNode: find's and findLast's callbacks get each
// element, its index and the array itself, findLast's from the end; and a
// find is a call, so a read of a binding its callback writes, beside it,
// is ordered as JavaScript orders it: n is read before find runs.
//
//	const xs: number[] = [5, 6, 7];
//	console.log(xs.find((x: number, i: number, a: number[]): boolean => { console.log(x, i, a === xs); return i === 1; }));
//	console.log(xs.findLast((x: number, i: number, a: number[]): boolean => { console.log(x, i, a.length); return false; }));
//	let n: number = 1;
//	console.log(n + xs.find((x: number): boolean => { n = 10; return true; })!);
//	console.log(n);
func TestFindCallbacksMatchNode(t *testing.T) {
	t.Parallel()

	nums, numM := ir.ArrayOf(ir.Number), ir.MaybeOf(ir.Number)
	xs, x, i, a := stable("xs", nums), stable("x", ir.Number), stable("i", ir.Number), stable("a", nums)
	n := ident("n", ir.Number)
	first := method(ir.ArrayFind, xs, numM, arrow(visit(ir.Number, "", "", ""), ir.Boolean, assign("n", num(10)), ret(boolLit(true))))
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("xs", nums, arrayOf(ir.Number, num(5), num(6), num(7))),
		logNum(method(ir.ArrayFind, xs, numM, arrow(visit(ir.Number, "x", "i", "a"), ir.Boolean,
			logAll(x, i, binary(ir.OpEqual, a, xs, ir.Boolean)), ret(binary(ir.OpEqual, i, num(1), ir.Boolean))))),
		logNum(method(ir.ArrayFindLast, xs, numM, arrow(visit(ir.Number, "x", "i", "a"), ir.Boolean,
			logAll(x, i, method(ir.ArrayLength, a, ir.Number)), ret(boolLit(false))))),
		&ir.VarDecl{Name: "n", Type: ir.Number, Init: num(1), Mutable: true, Span: span},
		logNum(binary(ir.OpAdd, n, &ir.Unwrap{Value: first, Typ: ir.Number, Asserted: true, Span: span}, ir.Number)),
		logNum(n),
	}}
	p := ir.ProgramOf(m)
	want := "5 0 true\n6 1 true\n6\n7 2 3\n6 1 3\n5 0 3\nundefined\n6\n10\n"
	if got := buildAndRun(t, p); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
	if src := Emit(p); !strings.Contains(src, "ts_read(n) + ") {
		t.Errorf("n beside find isn't an ordered read:\n%s", src)
	}
}

// TestFindShrinkingPanics (D9): where Node reads undefined for the
// elements find's callback removed, cakebear stops, saying so. Node prints
// undefined. The pops are statements, which drop what they remove.
//
//	const xs: number[] = [1, 2, 3, 4];
//	console.log(xs.find((x: number, i: number, a: number[]): boolean => { if (i === 1) { a.pop(); a.pop(); } return false; }));
func TestFindShrinkingPanics(t *testing.T) {
	t.Parallel()

	nums := ir.ArrayOf(ir.Number)
	a := stable("a", nums)
	cut := &ir.If{Cond: binary(ir.OpEqual, stable("i", ir.Number), num(1), ir.Boolean), Span: span,
		Then: []ir.Stmt{do(method(ir.ArrayPop, a, ir.Void)), do(method(ir.ArrayPop, a, ir.Void))}}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("xs", nums, arrayOf(ir.Number, num(1), num(2), num(3), num(4))),
		logNum(method(ir.ArrayFind, stable("xs", nums), ir.MaybeOf(ir.Number),
			arrow(visit(ir.Number, "", "i", "a"), ir.Boolean, cut, ret(boolLit(false))))),
	}}
	const want = "cakebear: the array shrank to 2 elements while find ran over 4 (Node reads undefined)"
	if out := buildAndPanic(t, ir.ProgramOf(m), want); out != "" {
		t.Errorf("the program printed %q before the panic", out)
	}
}
