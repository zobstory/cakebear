package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// The programs here are built from IR by hand, and what each prints is what
// Node 24.18 printed for the TypeScript in its comment.

// TestObjectKeyOrderMatchesNode: two literals of one shape print in the
// order each wrote its keys, and nest as Node nests them. Literals listing
// their keys alike share a key table.
//
//	interface Point { x: number; y: number }
//	const a: Point = { x: 1, y: 2 };
//	const b: Point = { y: 2, x: 1 };
//	const c: Point = { x: 3, y: 4 };
//	console.log(a); console.log(b); console.log(c);
//	const n = { a: 1, b: { c: "x", d: true } };
//	console.log(n); console.log(n.b); console.log(n.b.c);
func TestObjectKeyOrderMatchesNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	inner := shapes.Intern("", []ir.Field{{Name: "c", Type: ir.String}, {Name: "d", Type: ir.Boolean}})
	outer := shapes.Intern("", []ir.Field{{Name: "a", Type: ir.Number}, {Name: "b", Type: ir.ObjectOf(inner)}})
	n := stable("n", ir.ObjectOf(outer))
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		&ir.VarDecl{Name: "a", Type: ir.ObjectOf(pt), Init: lit(pt, "x", num(1), "y", num(2)), Span: span},
		&ir.VarDecl{Name: "b", Type: ir.ObjectOf(pt), Init: lit(pt, "y", num(2), "x", num(1)), Span: span},
		&ir.VarDecl{Name: "c", Type: ir.ObjectOf(pt), Init: lit(pt, "x", num(3), "y", num(4)), Span: span},
		logNum(stable("a", ir.ObjectOf(pt))), logNum(stable("b", ir.ObjectOf(pt))), logNum(stable("c", ir.ObjectOf(pt))),
		&ir.VarDecl{Name: "n", Type: ir.ObjectOf(outer), Span: span,
			Init: lit(outer, "a", num(1), "b", lit(inner, "c", str("x"), "d", &ir.BoolLit{Value: true, Span: span}))},
		logNum(n), logNum(get(n, "b")), logNum(get(get(n, "b"), "c")),
	}}
	p := withShapes(m, &shapes)
	want := "{ x: 1, y: 2 }\n{ y: 2, x: 1 }\n{ x: 3, y: 4 }\n{ a: 1, b: { c: 'x', d: true } }\n{ c: 'x', d: true }\nx\n"
	if got := buildAndRun(t, p); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
	// One table each for x-then-y, y-then-x, inner and outer: c reuses a's.
	if src := Emit(p); strings.Count(src, ".NewLayout(") != 4 || !strings.Contains(src, "c = &ts_shape0{ts_layout: ts_layout0, x: 3.0") {
		t.Errorf("want four key tables, c's shared with a:\n%s", src)
	}
}

// TestObjectFieldsMatchNode: reads and writes, seen through an alias, from a
// function the object is passed to, on one a function returns, in a closure
// and of a literal, and `===` as identity.
//
//	function moveRight(p: Point): void { p.x = p.x + 10; }
//	function origin(): Point { return { x: 0, y: 0 }; }
//	const p: Point = { x: 1, y: 2 };
//	const q: Point = p;
//	q.y = 5;
//	console.log(p.y);
//	moveRight(p);
//	console.log(p);
//	const o: Point = origin();
//	console.log(o);
//	const bumpX = (): void => { o.x = o.x + 1; };
//	bumpX(); bumpX();
//	console.log(o.x);
//	console.log(p === q); console.log(p === origin());
//	const r: Point = { x: 11, y: 5 };
//	console.log(p === r); console.log(p !== r);
//	console.log(({ x: 5, y: 6 }).y);
func TestObjectFieldsMatchNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	obj := ir.ObjectOf(pt)
	p, q, o, r := stable("p", obj), stable("q", obj), stable("o", obj), stable("r", obj)
	origin := &ir.Call{Callee: "origin", Typ: obj, Span: span}
	callVoid := func(name string, args ...ir.Expr) ir.Stmt {
		return &ir.ExprStmt{Expr: &ir.Call{Callee: name, Args: args, Typ: ir.Void, Span: span}, Span: span}
	}
	is := func(op ir.BinaryOp, l, r ir.Expr) ir.Stmt { return logNum(binary(op, l, r, ir.Boolean)) }
	bumpX := &ir.FuncLit{Result: ir.Void, Typ: ir.FuncType(nil, ir.Void), Span: span, Body: []ir.Stmt{
		set(o, "x", binary(ir.OpAdd, get(o, "x"), num(1), ir.Number)),
	}}
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{
			{Name: "moveRight", Params: []ir.Param{{Name: "p", Type: obj}}, Result: ir.Void, Span: span,
				Body: []ir.Stmt{set(p, "x", binary(ir.OpAdd, get(p, "x"), num(10), ir.Number))}},
			{Name: "origin", Result: obj, Span: span, Body: returns(lit(pt, "x", num(0), "y", num(0)))},
		},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "p", Type: obj, Init: lit(pt, "x", num(1), "y", num(2)), Span: span},
			&ir.VarDecl{Name: "q", Type: obj, Init: p, Span: span},
			set(q, "y", num(5)),
			logNum(get(p, "y")),
			callVoid("moveRight", p),
			logNum(p),
			&ir.VarDecl{Name: "o", Type: obj, Init: origin, Span: span},
			logNum(o),
			&ir.VarDecl{Name: "bumpX", Type: bumpX.Typ, Init: bumpX, Span: span},
			callVoid("bumpX"), callVoid("bumpX"),
			logNum(get(o, "x")),
			is(ir.OpEqual, p, q), is(ir.OpEqual, p, origin),
			&ir.VarDecl{Name: "r", Type: obj, Init: lit(pt, "x", num(11), "y", num(5)), Span: span},
			is(ir.OpEqual, p, r), is(ir.OpNotEqual, p, r),
			logNum(get(lit(pt, "x", num(5), "y", num(6)), "y")),
		}}
	want := "5\n{ x: 11, y: 5 }\n{ x: 0, y: 0 }\n2\ntrue\nfalse\nfalse\ntrue\n6\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestFieldReadOrder (M4 check 7): a field is read where JavaScript reads it
// relative to the calls around it: before a call on its right in a sum or a
// literal, even one under another field read, and the object of an
// assignment, a binding or a path of fields, before its value.
//
//	interface Box { p: Point }
//	let p: Point = { x: 1, y: 0 };
//	function bump(): number { p.x = p.x + 10; return 100; }
//	console.log(p.x + bump());
//	const lit = { a: p.x, b: bump() };
//	console.log(lit);
//	const first: Point = p;
//	function swap(): number { p = { x: -1, y: -1 }; return 7; }
//	p.x = swap();
//	console.log(first); console.log(p);
//	function moved(): Point { p.x = p.x + 10; return { x: 0, y: 1000 }; }
//	console.log(p.x + moved().y);
//	const box: Box = { p: p };
//	function swapBox(): number { box.p = { x: -2, y: -2 }; return 8; }
//	const held: Point = box.p;
//	box.p.x = swapBox();
//	console.log(held); console.log(box);
func TestFieldReadOrder(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	ab := shapes.Intern("", []ir.Field{{Name: "a", Type: ir.Number}, {Name: "b", Type: ir.Number}})
	obj := ir.ObjectOf(pt)
	bx := shapes.Intern("Box", []ir.Field{{Name: "p", Type: obj}})
	p, box := ident("p", obj), stable("box", ir.ObjectOf(bx))
	call := func(name string) *ir.Call { return &ir.Call{Callee: name, Typ: ir.Number, Span: span} }
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{
			{Name: "bump", Result: ir.Number, Span: span, Body: []ir.Stmt{
				set(p, "x", binary(ir.OpAdd, get(p, "x"), num(10), ir.Number)),
				&ir.Return{Value: num(100), Span: span},
			}},
			{Name: "swap", Result: ir.Number, Span: span, Body: []ir.Stmt{
				&ir.Assign{Name: "p", Value: lit(pt, "x", num(-1), "y", num(-1)), Span: span},
				&ir.Return{Value: num(7), Span: span},
			}},
			{Name: "moved", Result: obj, Span: span, Body: []ir.Stmt{
				set(p, "x", binary(ir.OpAdd, get(p, "x"), num(10), ir.Number)),
				&ir.Return{Value: lit(pt, "x", num(0), "y", num(1000)), Span: span},
			}},
			{Name: "swapBox", Result: ir.Number, Span: span, Body: []ir.Stmt{
				set(box, "p", lit(pt, "x", num(-2), "y", num(-2))),
				&ir.Return{Value: num(8), Span: span},
			}},
		},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "p", Type: obj, Init: lit(pt, "x", num(1), "y", num(0)), Mutable: true, Span: span},
			logNum(binary(ir.OpAdd, get(p, "x"), call("bump"), ir.Number)),
			&ir.VarDecl{Name: "lit", Type: ir.ObjectOf(ab), Init: lit(ab, "a", get(p, "x"), "b", call("bump")), Span: span},
			logNum(stable("lit", ir.ObjectOf(ab))),
			&ir.VarDecl{Name: "first", Type: obj, Init: p, Span: span},
			set(p, "x", call("swap")),
			logNum(stable("first", obj)),
			logNum(p),
			logNum(binary(ir.OpAdd, get(p, "x"), get(&ir.Call{Callee: "moved", Typ: obj, Span: span}, "y"), ir.Number)),
			&ir.VarDecl{Name: "box", Type: ir.ObjectOf(bx), Init: lit(bx, "p", p), Span: span},
			&ir.VarDecl{Name: "held", Type: obj, Init: get(box, "p"), Span: span},
			set(get(box, "p"), "x", call("swapBox")),
			logNum(stable("held", obj)),
			logNum(box),
		}}
	want := "101\n{ a: 11, b: 100 }\n{ x: 7, y: 0 }\n{ x: -1, y: -1 }\n999\n{ x: 8, y: -1 }\n{ p: { x: -2, y: -2 } }\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestOddPropertyNamesMatchNode: properties whose names Go can't use as they
// are, or would confuse with the struct's own, keep their keys.
//
//	const o = { $x: 1, type: "a", Layout: true, "content-type": "text/plain", Field: 2, CloneObject: 3,
//	  ts_layout: 4, "": 5, "1st": 6, _: 7, "line\nbreak": 8 };
//	console.log(o);
//	console.log(o.$x, o.type, o.Layout, o["content-type"], o.Field, o.CloneObject, o.ts_layout, o[""], o["1st"], o._, o["line\nbreak"]);
func TestOddPropertyNamesMatchNode(t *testing.T) {
	t.Parallel()

	keys := []string{"$x", "type", "Layout", "content-type", "Field", "CloneObject", "ts_layout", "", "1st", "_", "line\nbreak"}
	values := []ir.Expr{num(1), str("a"), &ir.BoolLit{Value: true, Span: span}, str("text/plain"), num(2), num(3), num(4), num(5), num(6), num(7), num(8)}
	fields := make([]ir.Field, len(keys))
	kv := make([]any, 0, 2*len(keys))
	for i, k := range keys {
		fields[i] = ir.Field{Name: k, Type: values[i].ExprType()}
		kv = append(kv, k, values[i])
	}
	var shapes ir.Shapes
	s := shapes.Intern("", fields)
	o := stable("o", ir.ObjectOf(s))
	reads := make([]ir.Expr, len(keys))
	for i, k := range keys {
		reads[i] = get(o, k)
	}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		&ir.VarDecl{Name: "o", Type: ir.ObjectOf(s), Init: lit(s, kv...), Span: span},
		logNum(o),
		logAll(reads...),
	}}
	want := "{\n  '$x': 1,\n  type: 'a',\n  Layout: true,\n  'content-type': 'text/plain',\n  Field: 2,\n  CloneObject: 3,\n" +
		"  ts_layout: 4,\n  '': 5,\n  '1st': 6,\n  _: 7,\n  'line\\nbreak': 8\n}\n" +
		"1 a true text/plain 2 3 4 5 6 7 8\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestArrayIndexKeysPrintFirst: keys that are array indices print first,
// ascending, as Node lists them, while every value is still evaluated in
// source order; two literals listing their keys alike that way share a key
// table.
//
//	function t(n: number): number { console.log(n); return n; }
//	const o = { "4294967295": t(1), b: t(2), "4294967294": t(3), "01": t(4), "10": t(5), "9": t(6), "-1": t(7), "1.5": t(8), "0": t(9) };
//	console.log(o);
//	const q1 = { "2": t(10), c: t(11), "1": t(12) };
//	const q2 = { "1": t(13), "2": t(14), c: t(15) };
//	console.log(q1, q2);
func TestArrayIndexKeysPrintFirst(t *testing.T) {
	t.Parallel()

	tap := &ir.Func{Name: "t", Params: []ir.Param{{Name: "n", Type: ir.Number}}, Result: ir.Number, Span: span,
		Body: []ir.Stmt{logNum(stable("n", ir.Number)), &ir.Return{Value: stable("n", ir.Number), Span: span}}}
	n := 0
	tapped := func(keys ...string) []any {
		kv := []any{}
		for _, k := range keys {
			n++
			kv = append(kv, k, &ir.Call{Callee: "t", Args: []ir.Expr{num(float64(n))}, Typ: ir.Number, Span: span})
		}
		return kv
	}
	numbers := func(keys ...string) []ir.Field {
		fields := make([]ir.Field, len(keys))
		for i, k := range keys {
			fields[i] = ir.Field{Name: k, Type: ir.Number}
		}
		return fields
	}
	var shapes ir.Shapes
	big := shapes.Intern("", numbers("4294967295", "b", "4294967294", "01", "10", "9", "-1", "1.5", "0"))
	small := shapes.Intern("", numbers("1", "2", "c"))
	o := lit(big, tapped("4294967295", "b", "4294967294", "01", "10", "9", "-1", "1.5", "0")...)
	q1, q2 := lit(small, tapped("2", "c", "1")...), lit(small, tapped("1", "2", "c")...)
	m := &ir.Module{Name: "t", Funcs: []*ir.Func{tap}, Main: []ir.Stmt{logNum(o), logAll(q1, q2)}}
	want := "1\n2\n3\n4\n5\n6\n7\n8\n9\n" +
		"{\n  '0': 9,\n  '9': 6,\n  '10': 5,\n  '4294967294': 3,\n  '4294967295': 1,\n  b: 2,\n  '01': 4,\n  '-1': 7,\n  '1.5': 8\n}\n" +
		"10\n11\n12\n13\n14\n15\n{ '1': 12, '2': 10, c: 11 } { '1': 13, '2': 14, c: 15 }\n"
	p := withShapes(m, &shapes)
	if got := buildAndRun(t, p); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
	if src := Emit(p); strings.Count(src, ".NewLayout(") != 2 {
		t.Errorf("want two key tables, q1's shared with q2:\n%s", src)
	}
}

// TestFunctionFieldHoldsAClosure: a field can hold a closure, which comes
// back out of it still closed over its binding.
//
//	interface Task { name: string; run: () => number }
//	const k: number = 40;
//	const t: Task = { name: "t", run: (): number => k + 2 };
//	function pass(x: Task): Task { return x; }
//	const f: () => number = pass(t).run;
//	console.log(f()); console.log(t.name);
func TestFunctionFieldHoldsAClosure(t *testing.T) {
	t.Parallel()

	run := ir.FuncType(nil, ir.Number)
	var shapes ir.Shapes
	task := shapes.Intern("Task", []ir.Field{{Name: "name", Type: ir.String}, {Name: "run", Type: run}})
	tk := stable("t", ir.ObjectOf(task))
	closure := &ir.FuncLit{Result: ir.Number, Typ: run, Span: span,
		Body: returns(binary(ir.OpAdd, stable("k", ir.Number), num(2), ir.Number))}
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "pass", Params: []ir.Param{{Name: "x", Type: ir.ObjectOf(task)}}, Result: ir.ObjectOf(task), Span: span,
			Body: returns(stable("x", ir.ObjectOf(task)))}},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "k", Type: ir.Number, Init: num(40), Span: span},
			&ir.VarDecl{Name: "t", Type: ir.ObjectOf(task), Init: lit(task, "name", str("t"), "run", closure), Span: span},
			&ir.VarDecl{Name: "f", Type: run, Span: span,
				Init: get(&ir.Call{Callee: "pass", Args: []ir.Expr{tk}, Typ: ir.ObjectOf(task), Span: span}, "run")},
			logNum(&ir.Call{Callee: "f", Typ: ir.Number, Span: span}),
			logNum(get(tk, "name")),
		}}
	if got, want := buildAndRun(t, withShapes(m, &shapes)), "42\nt\n"; got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestFieldSetInALoopClause: a field write can be a for loop's update.
//
//	const c = { n: 0 };
//	for (; c.n < 3; c.n = c.n + 1) { console.log(c.n); }
//	console.log(c);
func TestFieldSetInALoopClause(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	s := shapes.Intern("", []ir.Field{{Name: "n", Type: ir.Number}})
	c := stable("c", ir.ObjectOf(s))
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		&ir.VarDecl{Name: "c", Type: ir.ObjectOf(s), Init: lit(s, "n", num(0)), Span: span},
		&ir.For{
			Cond: binary(ir.OpLess, get(c, "n"), num(3), ir.Boolean),
			Post: []ir.Stmt{set(c, "n", binary(ir.OpAdd, get(c, "n"), num(1), ir.Number))},
			Body: []ir.Stmt{logNum(get(c, "n"))},
			Span: span,
		},
		logNum(c),
	}}
	if got, want := buildAndRun(t, withShapes(m, &shapes)), "0\n1\n2\n{ n: 3 }\n"; got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestObjectsAcrossModules: shapes and key tables are the program's, not a
// module's, so an object made in one module is printed and compared in
// another.
//
//	// lib.ts
//	export interface Point { x: number; y: number }
//	export function make(): Point { return { y: 2, x: 1 }; }
//	// main.ts
//	const p: Point = make();
//	console.log(p); console.log(p === make()); console.log(p === p);
func TestObjectsAcrossModules(t *testing.T) {
	t.Parallel()

	const lib, main = "/p/lib.ts", "/p/main.ts"
	var shapes ir.Shapes
	pt := point(&shapes)
	obj := ir.ObjectOf(pt)
	mk := &ir.Call{Callee: "make", Module: lib, Typ: obj, Span: span}
	p := &ir.Ident{Name: "p", Module: main, Stable: true, Typ: obj, Span: span}
	prog := &ir.Program{Entry: 1, Shapes: shapes.All(), Modules: []*ir.Module{
		{File: lib, Funcs: []*ir.Func{{Name: "make", Result: obj, Span: span, Body: returns(lit(pt, "y", num(2), "x", num(1)))}}},
		{Name: "main", File: main, Imports: []string{lib}, Main: []ir.Stmt{
			&ir.VarDecl{Name: "p", Type: obj, Init: mk, Span: span},
			logNum(p),
			logNum(binary(ir.OpEqual, p, mk, ir.Boolean)),
			logNum(binary(ir.OpEqual, p, p, ir.Boolean)),
		}},
	}}
	if got, want := buildAndRun(t, prog), "{ y: 2, x: 1 }\nfalse\ntrue\n"; got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

// TestCloneObjectKeepsAliasesAndCycles: no IR reaches CloneObject until a
// spawn's result is copied (D11, step 6), so a Go file built into the same
// package calls it on the generated structs. What it prints is what Node's
// structuredClone gives for the same objects.
//
//	const n = { name: "a" }; n.next = n;
//	const c = structuredClone(n);
//	console.log(c, c !== n, c.next === c);
//	const pt = { x: 1, y: 2 };
//	const cp = structuredClone({ a: pt, b: pt });
//	console.log(cp, cp.a === cp.b, cp.a !== pt);
func TestCloneObjectKeepsAliasesAndCycles(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	node := shapes.Reserve("Node")
	pt := point(&shapes)
	shapes.Intern("Pair", []ir.Field{{Name: "a", Type: ir.ObjectOf(pt)}, {Name: "b", Type: ir.ObjectOf(pt)}})
	shapes.Define(node, []ir.Field{{Name: "name", Type: ir.String}, {Name: "next", Type: ir.ObjectOf(node)}})
	const check = `package main

import (
	"fmt"

	cakebearrt "cakebear.local/out/cakebearrt"
)

func init() {
	n := &ts_shape0{ts_layout: cakebearrt.NewLayout([]string{"name", "next"}, []int{0, 1}), name: "a"}
	n.next = n
	c := cakebearrt.Clone(n)
	fmt.Println(cakebearrt.Inspect(c), c != n, c.next == c)
	pt := &ts_shape1{ts_layout: cakebearrt.NewLayout([]string{"x", "y"}, []int{0, 1}), x: 1, y: 2}
	cp := cakebearrt.Clone(&ts_shape2{ts_layout: cakebearrt.NewLayout([]string{"a", "b"}, []int{0, 1}), a: pt, b: pt})
	fmt.Println(cakebearrt.Inspect(cp), cp.a == cp.b, cp.a != pt)
}
`
	src, unsupported, err := emit(withShapes(&ir.Module{Name: "t"}, &shapes))
	if err != nil || len(unsupported) > 0 {
		t.Fatalf("emit: %v %+v", err, unsupported)
	}
	dir := t.TempDir()
	if err := writeModule(dir, src); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "check.go"), []byte(check), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "prog")
	build := exec.Command("go", "build", "-o", out, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GO111MODULE=on")
	if combined, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s\n%s", err, combined, src)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running the binary: %v", err)
	}
	want := "<ref *1> { name: 'a', next: [Circular *1] } true true\n{ a: { x: 1, y: 2 }, b: { x: 1, y: 2 } } true true\n"
	if string(got) != want {
		t.Errorf("the clones printed %q, want %q", got, want)
	}
}
