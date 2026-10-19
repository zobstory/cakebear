package backend

import (
	"bytes"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// Null and undefined (M5 step 3), built from IR by hand. What each program
// prints is what Node 24.18 printed for the TypeScript in its comment.

// TestMaybeStatesMatchNode: a maybe holding a value, undefined and null, at
// the top level and in a function's own scope, printed; an optional property
// left out, which reads undefined; functions whose end is reached, which
// return undefined; and a bare null and undefined printed.
//
//	interface Q { a: number; b?: string }
//	let top: number | undefined = undefined;
//	function pick(c: boolean): number | undefined { if (c) { return 1; } }
//	async function later(c: boolean): Promise<string | undefined> { if (c) { return "yes"; } }
//	let v: number | null | undefined = undefined;
//	console.log(v); v = 5; console.log(v); v = null; console.log(v);
//	let s: string | undefined = "hi";
//	console.log(s); s = undefined; console.log(s);
//	let b: boolean | null = true;
//	console.log(b); b = null; console.log(b);
//	console.log(top); top = -0; console.log(top);
//	const q: Q = { a: 1 };
//	console.log(q, q.b);
//	console.log(pick(true), pick(false));
//	console.log(await later(true), await later(false));
//	console.log(null); console.log(undefined); console.log(1, null, undefined);
func TestMaybeStatesMatchNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	q := shapes.Intern("Q", []ir.Field{{Name: "a", Type: ir.Number}, {Name: "b", Type: ir.MaybeOf(ir.String), Optional: true}})
	numM, strM, boolM := ir.MaybeOf(ir.Number), ir.MaybeOf(ir.String), ir.MaybeOf(ir.Boolean)
	c := []ir.Param{{Name: "c", Type: ir.Boolean, Span: span}}
	then := func(v ir.Expr) []ir.Stmt {
		return []ir.Stmt{&ir.If{Cond: stable("c", ir.Boolean), Then: []ir.Stmt{ret(v)}, Span: span}}
	}
	pick := func(b bool) ir.Expr {
		return &ir.Call{Callee: "pick", Args: []ir.Expr{boolLit(b)}, Typ: numM, Span: span}
	}
	later := func(b bool) ir.Expr {
		return &ir.Await{Value: &ir.Call{Callee: "later", Args: []ir.Expr{boolLit(b)}, Typ: ir.PromiseOf(strM), Span: span}, Typ: strM, Span: span}
	}
	let := func(name string, t ir.Type, init ir.Expr) ir.Stmt {
		return &ir.VarDecl{Name: name, Type: t, Init: init, Mutable: true, Span: span}
	}
	v, s, b, top := ident("v", numM), ident("s", strM), ident("b", boolM), ident("top", numM)
	m := &ir.Module{Name: "t", File: "/src/t.ts", Async: true,
		Funcs: []*ir.Func{
			{Name: "pick", Params: c, Result: numM, Span: span, Body: then(some(num(1)))},
			{Name: "later", Params: c, Result: ir.PromiseOf(strM), Async: true, Span: span, Body: then(some(str("yes")))},
		},
		Main: []ir.Stmt{
			let("top", numM, undef(ir.Number)),
			let("v", numM, undef(ir.Number)),
			logNum(v), assign("v", some(num(5))), logNum(v), assign("v", null(ir.Number)), logNum(v),
			let("s", strM, some(str("hi"))),
			logNum(s), assign("s", undef(ir.String)), logNum(s),
			let("b", boolM, some(boolLit(true))),
			logNum(b), assign("b", null(ir.Boolean)), logNum(b),
			logNum(top), assign("top", some(num(math.Copysign(0, -1)))), logNum(top),
			decl("q", ir.ObjectOf(q), lit(q, "a", num(1))),
			logAll(stable("q", ir.ObjectOf(q)), get(stable("q", ir.ObjectOf(q)), "b")),
			logAll(pick(true), pick(false)),
			logAll(later(true), later(false)),
			logAll(&ir.NullLit{Span: span}), logAll(&ir.UndefinedLit{Span: span}),
			logAll(num(1), &ir.NullLit{Span: span}, &ir.UndefinedLit{Span: span}),
		}}
	want := "undefined\n5\nnull\nhi\nundefined\ntrue\nnull\nundefined\n-0\n{ a: 1 } undefined\n1 undefined\nyes undefined\nnull\nundefined\n1 null undefined\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestMaybeEqualityMatchesNode: `===` and `!==` between two maybes compare
// states, then values, as a T's do: NaN unequal to itself, -0 equal to 0,
// undefined unequal to null and to 0, objects by identity. An undefined is a
// composite literal in Go, which an if's condition can start.
//
//	interface Point { x: number; y: number }
//	const a: number | undefined = NaN; const b: number | undefined = NaN;
//	console.log(a === b, a !== b);
//	const z: number | undefined = -0; const p: number | undefined = 0;
//	console.log(z === p, z !== p);
//	const u1: number | undefined = undefined; const u2: number | undefined = undefined;
//	console.log(u1 === u2, u1 === p);
//	const n1: number | null | undefined = null; const n2: number | null | undefined = null;
//	console.log(n1 === u1, n1 !== u1, n1 === n2, n1 === p);
//	const five: number | undefined = 5;
//	console.log(five === p, five === (5 as number | undefined));
//	const s1: string | undefined = "a"; const s2: string | undefined = "a"; const s3: string | undefined = undefined;
//	console.log(s1 === s2, s1 === s3);
//	const o1: Point | undefined = { x: 1, y: 2 }; const o2: Point | undefined = { x: 1, y: 2 }; const o3: Point | undefined = o1;
//	console.log(o1 === o2, o1 === o3);
//	if ((undefined as number | undefined) === u1) { console.log("undefined first"); }
func TestMaybeEqualityMatchesNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	numM, strM, ptM := ir.MaybeOf(ir.Number), ir.MaybeOf(ir.String), ir.MaybeOf(ir.ObjectOf(pt))
	eq := func(l, r ir.Expr) ir.Expr { return binary(ir.OpEqual, l, r, ir.Boolean) }
	ne := func(l, r ir.Expr) ir.Expr { return binary(ir.OpNotEqual, l, r, ir.Boolean) }
	n := func(name string) ir.Expr { return stable(name, numM) }
	nan := func() ir.Expr { return binary(ir.OpDiv, num(0), num(0), ir.Number) } // a literal NaN has no Go spelling
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("a", numM, some(nan())), decl("b", numM, some(nan())),
		logAll(eq(n("a"), n("b")), ne(n("a"), n("b"))),
		decl("z", numM, some(num(math.Copysign(0, -1)))), decl("p", numM, some(num(0))),
		logAll(eq(n("z"), n("p")), ne(n("z"), n("p"))),
		decl("u1", numM, undef(ir.Number)), decl("u2", numM, undef(ir.Number)),
		logAll(eq(n("u1"), n("u2")), eq(n("u1"), n("p"))),
		decl("n1", numM, null(ir.Number)), decl("n2", numM, null(ir.Number)),
		logAll(eq(n("n1"), n("u1")), ne(n("n1"), n("u1")), eq(n("n1"), n("n2")), eq(n("n1"), n("p"))),
		decl("five", numM, some(num(5))),
		logAll(eq(n("five"), n("p")), eq(n("five"), some(num(5)))),
		decl("s1", strM, some(str("a"))), decl("s2", strM, some(str("a"))), decl("s3", strM, undef(ir.String)),
		logAll(eq(stable("s1", strM), stable("s2", strM)), eq(stable("s1", strM), stable("s3", strM))),
		decl("o1", ptM, some(lit(pt, "x", num(1), "y", num(2)))), decl("o2", ptM, some(lit(pt, "x", num(1), "y", num(2)))),
		decl("o3", ptM, stable("o1", ptM)),
		logAll(eq(stable("o1", ptM), stable("o2", ptM)), eq(stable("o1", ptM), stable("o3", ptM))),
		&ir.If{Cond: eq(undef(ir.Number), n("u1")), Then: []ir.Stmt{logStr("undefined first")}, Span: span},
	}}
	want := "false true\ntrue false\ntrue false\nfalse true true false\nfalse true\ntrue false\nfalse true\nundefined first\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestIsEmptyMatchesNode: `=== undefined`, `=== null`, `== null` and
// `!= null` on each state, over an array of maybes, one of an undefined
// itself, which starts an if's condition, and a value unwrapped where
// TypeScript narrowed it.
//
//	const xs: (number | null | undefined)[] = [1, null, undefined, 0];
//	for (const x of xs) { console.log(x === undefined, x === null, x == null, x != null); }
//	if ((undefined as number | undefined) === undefined) { console.log("undefined is undefined"); }
//	const m: number | undefined = 7;
//	console.log(m! + 1);
func TestIsEmptyMatchesNode(t *testing.T) {
	t.Parallel()

	numM := ir.MaybeOf(ir.Number)
	x := stable("x", numM)
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		&ir.ForOf{Name: "x", Type: numM, Array: arrayOf(numM, some(num(1)), null(ir.Number), undef(ir.Number), some(num(0))), Span: span,
			Body: []ir.Stmt{logAll(isEmpty(x, true, false), isEmpty(x, false, true), isEmpty(x, true, true),
				&ir.Unary{Op: ir.OpNot, Operand: isEmpty(x, true, true), Typ: ir.Boolean, Span: span})}},
		&ir.If{Cond: isEmpty(undef(ir.Number), true, false), Then: []ir.Stmt{logStr("undefined is undefined")}, Span: span},
		decl("m", numM, some(num(7))),
		logNum(binary(ir.OpAdd, must(stable("m", numM)), num(1), ir.Number)),
	}}
	want := "false false false true\nfalse true true false\ntrue false true false\nfalse false false true\nundefined is undefined\n8\n"
	if got := buildAndRun(t, ir.ProgramOf(m)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestUnwrapOfAnEmptyPanics: where TypeScript's narrowing was wrong, Node
// carries on with the empty value and cakebear stops, exit 2, naming the
// value as the source does (M5 plan D4), an accepted difference.
//
//	const len: number | undefined = undefined;
//	console.log(len! + 1);          // Node prints NaN
//
//	interface Box { p: Point | null }
//	const boxes: Box[] = [{ p: null }];
//	console.log(boxes[0].p!);       // Node prints null
func TestUnwrapOfAnEmptyPanics(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	box := shapes.Intern("Box", []ir.Field{{Name: "p", Type: ir.MaybeOf(ir.ObjectOf(pt))}})
	boxes := stable("boxes", ir.ArrayOf(ir.ObjectOf(box)))
	first := &ir.IndexGet{Array: boxes, Index: num(0), Typ: ir.ObjectOf(box), Span: span}
	for _, tt := range []struct {
		main []ir.Stmt
		want string
	}{
		{[]ir.Stmt{
			decl("len", ir.MaybeOf(ir.Number), undef(ir.Number)),
			logNum(binary(ir.OpAdd, must(stable("len", ir.MaybeOf(ir.Number))), num(1), ir.Number)),
		}, "cakebear: len is undefined where TypeScript had narrowed it to a value (Node carries on with undefined)"},
		{[]ir.Stmt{
			decl("boxes", boxes.Typ, arrayOf(ir.ObjectOf(box), lit(box, "p", null(ir.ObjectOf(pt))))),
			logAll(must(get(first, "p"))),
		}, "cakebear: boxes[0].p is null where TypeScript had narrowed it to a value (Node carries on with null)"},
	} {
		out := filepath.Join(t.TempDir(), "prog")
		if src, err := Build(withShapes(&ir.Module{Name: "t", Main: tt.main}, &shapes), Options{Output: out}); err != nil {
			t.Fatalf("Build: %v\n%s", err, src)
		}
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(out)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), tt.want) {
			t.Errorf("%v, stdout %q, stderr\n%s\nwant exit 2, nothing printed, and %q", err, stdout.String(), stderr.String(), tt.want)
		}
	}
}

// TestMaybeToStringMatchesNode: a template's substitutions and String() of
// maybes of each primitive: "undefined", "null", or the value's own string.
//
//	const n1: number | undefined = 1.5; const n2: number | undefined = undefined; const n3: number | null = null;
//	console.log(`${n1} ${n2} ${n3}`);
//	const s1: string | undefined = "hi"; const s2: string | null = null;
//	console.log(`[${s1}] [${s2}]`);
//	const b1: boolean | undefined = false; const b2: boolean | undefined = undefined;
//	console.log(`${b1} ${b2}`);
//	console.log(String(n1));
//	const i: i32 | undefined = i32(-3); const u: u32 | null = u32(4294967295);
//	const f: f32 | undefined = f32(0.1); const big: i64 | undefined = i64(9007199254740992);
//	const e: base64 | undefined = base64("aGk="); const none: u64 | undefined = undefined;
//	console.log(`${i} ${u} ${f} ${big} ${e} ${none}`);
//
// Node ran it with i32 = x => x | 0, u32 = x => x >>> 0, f32 = Math.fround,
// and i64 and base64 the identity. ir.checkBuiltin takes only a primitive
// for ToString until lowering's half of M5 step 3 widens it, so this test
// builds the program without Validate.
func TestMaybeToStringMatchesNode(t *testing.T) {
	t.Parallel()

	toStr := func(x ir.Expr) ir.Expr {
		return &ir.BuiltinCall{Fn: ir.ToString, Args: []ir.Expr{x}, Typ: ir.String, Span: span}
	}
	tmpl := func(parts ...ir.Expr) ir.Stmt {
		for i, p := range parts {
			if p.ExprType().Kind == ir.KindMaybe {
				parts[i] = toStr(p)
			}
		}
		return logNum(&ir.Concat{Parts: parts, Span: span})
	}
	conv := func(t ir.Type, v ir.Expr) ir.Expr { return some(&ir.Convert{Value: v, Typ: t, Span: span}) }
	m := map[string]ir.Type{}
	declare := func(name string, init ir.Expr) ir.Stmt {
		m[name] = init.ExprType()
		return decl(name, init.ExprType(), init)
	}
	use := func(name string) ir.Expr { return stable(name, m[name]) }
	sp := str(" ")
	main := []ir.Stmt{
		declare("n1", some(num(1.5))), declare("n2", undef(ir.Number)), declare("n3", null(ir.Number)),
		declare("s1", some(str("hi"))), declare("s2", null(ir.String)),
		declare("b1", some(boolLit(false))), declare("b2", undef(ir.Boolean)),
		declare("i", conv(ir.Int32, num(-3))), declare("u", conv(ir.Uint32, num(4294967295))),
		declare("f", conv(ir.Float32, num(0.1))), declare("big", conv(ir.Int64, num(9007199254740992))),
		declare("e", conv(ir.Base64, str("aGk="))), declare("none", undef(ir.Uint64)),
	}
	main = append(main,
		tmpl(use("n1"), sp, use("n2"), sp, use("n3")),
		tmpl(str("["), use("s1"), str("] ["), use("s2"), str("]")),
		tmpl(use("b1"), sp, use("b2")),
		logNum(toStr(use("n1"))),
		tmpl(use("i"), sp, use("u"), sp, use("f"), sp, use("big"), sp, use("e"), sp, use("none")),
	)
	src, got := buildUnvalidated(t, ir.ProgramOf(&ir.Module{Name: "t", Main: main}))
	want := "1.5 undefined null\n[hi] [null]\nfalse undefined\n1.5\n-3 4294967295 0.10000000149011612 9007199254740992 aGk= undefined\n"
	if got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
	for _, want := range []string{
		"cakebearrt.MaybeToString(n1, cakebearrt.NumberToString)",
		"cakebearrt.MaybeToString(s1, func(v string) string { return v })",
		"cakebearrt.MaybeToString(b1, cakebearrt.BoolToString)",
		"cakebearrt.MaybeToString(i, func(v int32) string { return cakebearrt.IntToString(int64(v)) })",
		"cakebearrt.MaybeToString(big, cakebearrt.IntToString)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("emitted source missing %q:\n%s", want, src)
		}
	}
}

// buildUnvalidated builds p as Build does, but without Program.Validate,
// and returns its source and what it prints.
func buildUnvalidated(t *testing.T, p *ir.Program) (string, string) {
	t.Helper()
	var e emitter
	body := e.program(p)
	if len(e.unsupported) > 0 {
		t.Fatalf("unsupported: %+v\n%s", e.unsupported, body)
	}
	src := "package main\n\nimport " + rtPkg + " \"" + genModule + "/" + rtDir + "\"\n\n" + body
	dir := t.TempDir()
	if err := writeModule(dir, src); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "prog")
	build := exec.Command("go", "build", "-trimpath", "-o", out, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GO111MODULE=on")
	if combined, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s\n%s", err, combined, src)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running the binary: %v", err)
	}
	return src, string(got)
}

// TestMaybesInAggregatesMatchNode: maybes inside objects and arrays print
// and serialise as Node's do: an undefined property printed but not
// serialised, an undefined element serialised as null.
//
//	interface P { a: number | undefined; b: string | null; c: boolean | null | undefined }
//	const o: P = { a: 1, b: null, c: undefined };
//	console.log(o);
//	const xs: (number | undefined)[] = [1, undefined, 3];
//	console.log(xs);
//	const ss: (string | null)[] = ["x", null];
//	console.log(ss);
//	console.log("label", o, xs);
//	const mo: P | undefined = o; const none: P | undefined = undefined;
//	console.log(mo, none);
//	const box: { p: P | null } = { p: o };
//	console.log(box);
//	console.log(JSON.stringify(o));
//	console.log(JSON.stringify(xs));
//	console.log(JSON.stringify(box, null, 2));
//	const empty: P = { a: undefined, b: "s", c: false };
//	console.log(JSON.stringify(empty));
func TestMaybesInAggregatesMatchNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	p := shapes.Intern("P", []ir.Field{{Name: "a", Type: ir.MaybeOf(ir.Number)}, {Name: "b", Type: ir.MaybeOf(ir.String)}, {Name: "c", Type: ir.MaybeOf(ir.Boolean)}})
	pM := ir.MaybeOf(ir.ObjectOf(p))
	box := shapes.Intern("", []ir.Field{{Name: "p", Type: pM}})
	o, xs := stable("o", ir.ObjectOf(p)), stable("xs", ir.ArrayOf(ir.MaybeOf(ir.Number)))
	json := func(v ir.Expr, space ir.Expr) ir.Stmt {
		return logNum(&ir.JSONStringify{Value: v, Space: space, Span: span})
	}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("o", ir.ObjectOf(p), lit(p, "a", some(num(1)), "b", null(ir.String), "c", undef(ir.Boolean))),
		logNum(o),
		decl("xs", xs.Typ, arrayOf(ir.MaybeOf(ir.Number), some(num(1)), undef(ir.Number), some(num(3)))),
		logNum(xs),
		decl("ss", ir.ArrayOf(ir.MaybeOf(ir.String)), arrayOf(ir.MaybeOf(ir.String), some(str("x")), null(ir.String))),
		logNum(stable("ss", ir.ArrayOf(ir.MaybeOf(ir.String)))),
		logAll(str("label"), o, xs),
		decl("mo", pM, some(o)), decl("none", pM, undef(ir.ObjectOf(p))),
		logAll(stable("mo", pM), stable("none", pM)),
		decl("box", ir.ObjectOf(box), lit(box, "p", some(o))),
		logNum(stable("box", ir.ObjectOf(box))),
		json(o, nil), json(xs, nil), json(stable("box", ir.ObjectOf(box)), num(2)),
		json(lit(p, "a", undef(ir.Number), "b", some(str("s")), "c", some(boolLit(false))), nil),
	}}
	want := "{ a: 1, b: null, c: undefined }\n[ 1, undefined, 3 ]\n[ 'x', null ]\nlabel { a: 1, b: null, c: undefined } [ 1, undefined, 3 ]\n" +
		"{ a: 1, b: null, c: undefined } undefined\n{ p: { a: 1, b: null, c: undefined } }\n" +
		"{\"a\":1,\"b\":null}\n[1,null,3]\n{\n  \"p\": {\n    \"a\": 1,\n    \"b\": null\n  }\n}\n{\"b\":\"s\",\"c\":false}\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestSpawnedMaybeObjectIsCopied: a spawn's result that may be undefined,
// an object whose field may hold another, is copied on its way to the
// caller when the spawn has work left (M4 plan D11): the caller's write
// doesn't reach the object the spawn's timer reads. Built with the race
// detector, so a copy that shared the inner object would be a race found,
// not a value printed by luck. Node shares the object, so its last line is
// 5, the accepted difference D11 lists.
//
//	interface Box { p: Point | undefined }
//	const r: Box | undefined = await spawn((): Box | undefined => {
//	  const b: Box = { p: { x: 1, y: 2 } };
//	  setTimeout(() => { console.log(b.p!.x); }, 200);
//	  return b;
//	});
//	r!.p!.x = 5;
//	console.log(r);
func TestSpawnedMaybeObjectIsCopied(t *testing.T) {
	t.Parallel()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}

	var shapes ir.Shapes
	pt := point(&shapes)
	box := shapes.Intern("Box", []ir.Field{{Name: "p", Type: ir.MaybeOf(ir.ObjectOf(pt))}})
	boxM := ir.MaybeOf(ir.ObjectOf(box))
	b, r := stable("b", ir.ObjectOf(box)), stable("r", boxM)
	timer := &ir.HostCall{Op: ir.HostSetTimeout, Typ: ir.Number, Span: span, Args: []ir.Expr{
		arrow(nil, ir.Void, logNum(get(must(get(b, "p")), "x"))), num(200),
	}}
	fn := &ir.FuncLit{Result: boxM, Typ: ir.FuncType(nil, boxM), Concurrent: true, Span: span, Body: []ir.Stmt{
		decl("b", ir.ObjectOf(box), lit(box, "p", some(lit(pt, "x", num(1), "y", num(2))))),
		do(timer),
		ret(some(b)),
	}}
	spawned := &ir.HostCall{Op: ir.HostSpawn, Args: []ir.Expr{fn}, Typ: ir.PromiseOf(boxM), Span: span}
	m := &ir.Module{Name: "t", File: "/src/t.ts", Async: true, Main: []ir.Stmt{
		decl("r", boxM, &ir.Await{Value: spawned, Typ: boxM, Span: span}),
		set(must(get(must(r), "p")), "x", num(5)),
		logNum(r),
	}}

	out := filepath.Join(t.TempDir(), "prog")
	if src, err := Build(withShapes(m, &shapes), Options{Output: out, Race: true}); err != nil {
		t.Fatalf("Build: %v\n%s", err, src)
	}
	// A binary built without the detector would find no race whatever it
	// did; Go records the flag.
	if info, err := exec.Command("go", "version", "-m", out).Output(); err != nil || !strings.Contains(string(info), "-race=true") {
		t.Fatalf("the program was not built with the race detector (go version -m: %v):\n%s", err, info)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(out)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stderr.Len() > 0 {
		t.Fatalf("running the binary: %v, stderr\n%s", err, stderr.String())
	}
	if want := "{ p: { x: 5, y: 2 } }\n1\n"; stdout.String() != want {
		t.Errorf("the program printed %q, want %q", stdout.String(), want)
	}
}
