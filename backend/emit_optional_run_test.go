package backend

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// Optional properties written, deleted and looked for (M5 step 5), built
// from IR by hand. What each program prints is what Node 24.18 printed for
// the TypeScript in its comment, the same over three runs.

// del is `delete object.name`, and has is `"name" in object`.
func del(object ir.Expr, name string) ir.Stmt {
	return &ir.FieldDelete{Object: object, Field: object.ExprType().Shape.Index(name), Span: span}
}

func has(object ir.Expr, name string) ir.Expr {
	return &ir.HasField{Object: object, Field: object.ExprType().Shape.Index(name), Span: span}
}

func jsonOf(v ir.Expr) ir.Expr { return &ir.JSONStringify{Value: v, Span: span} }

// TestOptionalPropertiesMatchNode: a property left out and one given
// undefined, which prints but doesn't serialise, and `in` on each; the key
// order after writes, deletes and re-adds, a write appending its key after
// the literal's own whatever order those are in; a property deleted twice;
// and a deleted property, which reads undefined.
//
//	interface O { a: number; b?: number; c?: number }
//	const o: O = { a: 1 };
//	console.log(o, JSON.stringify(o), "a" in o, "b" in o, "c" in o);
//	o.c = 3;
//	o.b = undefined;
//	console.log(o, JSON.stringify(o), "b" in o, "c" in o);
//	delete o.c;
//	console.log(o, JSON.stringify(o), "c" in o, o.c);
//	o.c = 5;
//	console.log(o, JSON.stringify(o));
//	delete o.b;
//	delete o.b;
//	console.log(o, "b" in o, o.b);
//	o.b = 2;
//	o.b = 7;
//	console.log(o, JSON.stringify(o));
//	const p: O = { c: 1, a: 2 };
//	p.b = 3;
//	console.log(p);
//	delete p.c;
//	console.log(p, JSON.stringify(p), "c" in p);
//	const q: O = { a: 0, b: undefined };
//	console.log(q, JSON.stringify(q), "b" in q);
//	delete q.b;
//	console.log(q, "b" in q);
func TestOptionalPropertiesMatchNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	numM := ir.MaybeOf(ir.Number)
	s := shapes.Intern("O", []ir.Field{{Name: "a", Type: ir.Number}, {Name: "b", Type: numM, Optional: true}, {Name: "c", Type: numM, Optional: true}})
	o, p, q := stable("o", ir.ObjectOf(s)), stable("p", ir.ObjectOf(s)), stable("q", ir.ObjectOf(s))
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("o", ir.ObjectOf(s), lit(s, "a", num(1))),
		logAll(o, jsonOf(o), has(o, "a"), has(o, "b"), has(o, "c")),
		set(o, "c", some(num(3))),
		set(o, "b", undef(ir.Number)),
		logAll(o, jsonOf(o), has(o, "b"), has(o, "c")),
		del(o, "c"),
		logAll(o, jsonOf(o), has(o, "c"), get(o, "c")),
		set(o, "c", some(num(5))),
		logAll(o, jsonOf(o)),
		del(o, "b"),
		del(o, "b"),
		logAll(o, has(o, "b"), get(o, "b")),
		set(o, "b", some(num(2))),
		set(o, "b", some(num(7))),
		logAll(o, jsonOf(o)),
		decl("p", ir.ObjectOf(s), lit(s, "c", some(num(1)), "a", num(2))),
		set(p, "b", some(num(3))),
		logAll(p),
		del(p, "c"),
		logAll(p, jsonOf(p), has(p, "c")),
		decl("q", ir.ObjectOf(s), lit(s, "a", num(0), "b", undef(ir.Number))),
		logAll(q, jsonOf(q), has(q, "b")),
		del(q, "b"),
		logAll(q, has(q, "b")),
	}}
	want := "{ a: 1 } {\"a\":1} true false false\n" +
		"{ a: 1, c: 3, b: undefined } {\"a\":1,\"c\":3} true true\n" +
		"{ a: 1, b: undefined } {\"a\":1} false undefined\n" +
		"{ a: 1, b: undefined, c: 5 } {\"a\":1,\"c\":5}\n" +
		"{ a: 1, c: 5 } false undefined\n" +
		"{ a: 1, c: 5, b: 7 } {\"a\":1,\"c\":5,\"b\":7}\n" +
		"{ c: 1, a: 2, b: 3 }\n" +
		"{ a: 2, b: 3 } {\"a\":2,\"b\":3} false\n" +
		"{ a: 0, b: undefined } {\"a\":0} true\n" +
		"{ a: 0 } false\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed\n%s\nwant, as Node printed,\n%s", got, want)
	}
}

// TestOptionalWritesEvaluateInOrder: a write to an optional property
// evaluates the object, then the value, then adds the key, as JavaScript
// does: a value that serialises the object, or tests it with `in`, sees it
// without the property; a value whose call deletes the property is followed
// by the write, which adds the key back, last; a value whose call assigns
// the variable the object was read from writes to the object read before;
// and an object that is a call's result is evaluated before the value.
//
//	interface S { a: number; s?: string; n?: number }
//	const o: S = { a: 1 };
//	o.s = JSON.stringify(o);
//	console.log(o);
//	o.n = ("n" in o) ? 1 : 0;
//	console.log(o);
//	function drop(x: S): number { delete x.s; return 9; }
//	o.s = String(drop(o));
//	console.log(o);
//	let cur: S = { a: 1 };
//	const first: S = cur;
//	function swap(): number { cur = { a: 2 }; return 3; }
//	cur.n = swap();
//	console.log(first, cur);
//	let last: S = { a: 0 };
//	function fresh(): S { console.log("fresh"); last = { a: 4 }; return last; }
//	function val(): number { console.log("val"); return 5; }
//	fresh().n = val();
//	console.log(last);
func TestOptionalWritesEvaluateInOrder(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	s := shapes.Intern("S", []ir.Field{{Name: "a", Type: ir.Number}, {Name: "n", Type: ir.MaybeOf(ir.Number), Optional: true}, {Name: "s", Type: ir.MaybeOf(ir.String), Optional: true}})
	obj := ir.ObjectOf(s)
	o, x, cur, last := stable("o", obj), stable("x", obj), ident("cur", obj), ident("last", obj)
	toStr := func(v ir.Expr) ir.Expr {
		return &ir.BuiltinCall{Fn: ir.ToString, Args: []ir.Expr{v}, Typ: ir.String, Span: span}
	}
	let := func(name string, init ir.Expr) ir.Stmt {
		return &ir.VarDecl{Name: name, Type: obj, Init: init, Mutable: true, Span: span}
	}
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{
			{Name: "drop", Params: []ir.Param{{Name: "x", Type: obj, Span: span}}, Result: ir.Number, Span: span,
				Body: []ir.Stmt{del(x, "s"), ret(num(9))}},
			{Name: "swap", Result: ir.Number, Span: span,
				Body: []ir.Stmt{assign("cur", lit(s, "a", num(2))), ret(num(3))}},
			{Name: "fresh", Result: obj, Span: span,
				Body: []ir.Stmt{logStr("fresh"), assign("last", lit(s, "a", num(4))), ret(last)}},
			{Name: "val", Result: ir.Number, Span: span,
				Body: []ir.Stmt{logStr("val"), ret(num(5))}},
		},
		Main: []ir.Stmt{
			decl("o", obj, lit(s, "a", num(1))),
			set(o, "s", some(jsonOf(o))),
			logAll(o),
			set(o, "n", some(cond(has(o, "n"), num(1), num(0)))),
			logAll(o),
			set(o, "s", some(toStr(callOf("drop", ir.Number, o)))),
			logAll(o),
			let("cur", lit(s, "a", num(1))),
			decl("first", obj, cur),
			set(cur, "n", some(callOf("swap", ir.Number))),
			logAll(stable("first", obj), cur),
			let("last", lit(s, "a", num(0))),
			set(callOf("fresh", obj), "n", some(callOf("val", ir.Number))),
			logAll(last),
		}}
	want := "{ a: 1, s: '{\"a\":1}' }\n{ a: 1, s: '{\"a\":1}', n: 0 }\n{ a: 1, n: 0, s: '9' }\n{ a: 1, n: 3 } { a: 2 }\nfresh\nval\n{ a: 4, n: 5 }\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed\n%s\nwant, as Node printed,\n%s", got, want)
	}
}

// TestNestedOptionalPropertiesMatchNode: optional properties of objects
// held by optional properties and in arrays, written through a narrowed
// read, a for…of variable and an element, deleted through an element, and
// an array built in a loop whose odd elements gain a property.
//
//	interface Inner { x: number; tag?: string }
//	interface Outer { name: string; inner?: Inner; list: Inner[] }
//	const out: Outer = { name: "o", list: [{ x: 1 }, { x: 2, tag: "t" }] };
//	console.log(out);
//	out.inner = { x: 3 };
//	out.inner.tag = "deep";
//	for (const it of out.list) {
//	  if (!("tag" in it)) {
//	    it.tag = "none";
//	  }
//	}
//	console.log(out, JSON.stringify(out));
//	delete out.list[1].tag;
//	console.log(JSON.stringify(out.list));
//	delete out.inner;
//	console.log(out, "inner" in out);
//	const xs: Inner[] = [];
//	for (let i = 0; i < 4; i++) {
//	  const v: Inner = { x: i };
//	  if (i % 2 === 1) {
//	    v.tag = "odd";
//	  }
//	  xs.push(v);
//	}
//	console.log(xs, JSON.stringify(xs));
func TestNestedOptionalPropertiesMatchNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	in := shapes.Intern("Inner", []ir.Field{{Name: "tag", Type: ir.MaybeOf(ir.String), Optional: true}, {Name: "x", Type: ir.Number}})
	inner := ir.ObjectOf(in)
	outer := shapes.Intern("Outer", []ir.Field{
		{Name: "inner", Type: ir.MaybeOf(inner), Optional: true}, {Name: "list", Type: ir.ArrayOf(inner)}, {Name: "name", Type: ir.String},
	})
	out, it, v, i := stable("out", ir.ObjectOf(outer)), stable("it", inner), stable("v", inner), ident("i", ir.Number)
	xs := stable("xs", ir.ArrayOf(inner))
	not := func(x ir.Expr) ir.Expr { return &ir.Unary{Op: ir.OpNot, Operand: x, Typ: ir.Boolean, Span: span} }
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("out", ir.ObjectOf(outer), lit(outer, "name", str("o"), "list", arrayOf(inner, lit(in, "x", num(1)), lit(in, "x", num(2), "tag", some(str("t")))))),
		logAll(out),
		set(out, "inner", some(lit(in, "x", num(3)))),
		set(must(get(out, "inner")), "tag", some(str("deep"))),
		&ir.ForOf{Name: "it", Type: inner, Array: get(out, "list"), Span: span, Body: []ir.Stmt{
			&ir.If{Cond: not(has(it, "tag")), Then: []ir.Stmt{set(it, "tag", some(str("none")))}, Span: span},
		}},
		logAll(out, jsonOf(out)),
		del(elem(get(out, "list"), num(1)), "tag"),
		logAll(jsonOf(get(out, "list"))),
		del(out, "inner"),
		logAll(out, has(out, "inner")),
		decl("xs", xs.Typ, arrayOf(inner)),
		&ir.For{Init: []ir.Stmt{&ir.VarDecl{Name: "i", Type: ir.Number, Init: num(0), Mutable: true, Span: span}},
			Cond: binary(ir.OpLess, i, num(4), ir.Boolean),
			Post: []ir.Stmt{assign("i", binary(ir.OpAdd, i, num(1), ir.Number))},
			Span: span, Body: []ir.Stmt{
				decl("v", inner, lit(in, "x", i)),
				&ir.If{Cond: binary(ir.OpEqual, binary(ir.OpMod, i, num(2), ir.Number), num(1), ir.Boolean),
					Then: []ir.Stmt{set(v, "tag", some(str("odd")))}, Span: span},
				do(method(ir.ArrayPush, xs, ir.Number, v)),
			}},
		logAll(xs, jsonOf(xs)),
	}}
	want := "{ name: 'o', list: [ { x: 1 }, { x: 2, tag: 't' } ] }\n" +
		"{\n  name: 'o',\n  list: [ { x: 1, tag: 'none' }, { x: 2, tag: 't' } ],\n  inner: { x: 3, tag: 'deep' }\n}" +
		" {\"name\":\"o\",\"list\":[{\"x\":1,\"tag\":\"none\"},{\"x\":2,\"tag\":\"t\"}],\"inner\":{\"x\":3,\"tag\":\"deep\"}}\n" +
		"[{\"x\":1,\"tag\":\"none\"},{\"x\":2}]\n" +
		"{ name: 'o', list: [ { x: 1, tag: 'none' }, { x: 2 } ] } false\n" +
		"[ { x: 0 }, { x: 1, tag: 'odd' }, { x: 2 }, { x: 3, tag: 'odd' } ] [{\"x\":0},{\"x\":1,\"tag\":\"odd\"},{\"x\":2},{\"x\":3,\"tag\":\"odd\"}]\n"
	if got := buildAndRun(t, withShapes(m, &shapes)); got != want {
		t.Errorf("the program printed\n%s\nwant, as Node printed,\n%s", got, want)
	}
}

// TestReadOnlyOptionalPropertiesMatchNode: the frozen const's use, read and
// never written: literals that leave a property out, give it a value or give
// it undefined, read, looked for with `in` and defaulted with `??`, and the
// whole printed and serialised. Nothing writes or deletes, so the shape has
// no setter or deleter.
//
//	interface Conf { readonly host: string; readonly port?: number }
//	const confs: readonly Conf[] = [{ host: "a" }, { host: "b", port: 80 }, { port: undefined, host: "c" }];
//	for (const c of confs) {
//	  console.log(c.host, c.port, "port" in c, c.port ?? 443);
//	}
//	console.log(confs, JSON.stringify(confs));
func TestReadOnlyOptionalPropertiesMatchNode(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	s := shapes.Intern("Conf", []ir.Field{{Name: "host", Type: ir.String}, {Name: "port", Type: ir.MaybeOf(ir.Number), Optional: true}})
	conf := ir.ObjectOf(s)
	c, confs := stable("c", conf), stable("confs", ir.ArrayOf(conf))
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		decl("confs", confs.Typ, arrayOf(conf, lit(s, "host", str("a")), lit(s, "host", str("b"), "port", some(num(80))), lit(s, "port", undef(ir.Number), "host", str("c")))),
		&ir.ForOf{Name: "c", Type: conf, Array: confs, Span: span, Body: []ir.Stmt{
			logAll(get(c, "host"), get(c, "port"), has(c, "port"), nullish(get(c, "port"), num(443))),
		}},
		logAll(confs, jsonOf(confs)),
	}}
	p := withShapes(m, &shapes)
	want := "a undefined false 443\nb 80 true 80\nc undefined true 443\n" +
		"[\n  { host: 'a' },\n  { host: 'b', port: 80 },\n  { port: undefined, host: 'c' }\n] [{\"host\":\"a\"},{\"host\":\"b\",\"port\":80},{\"host\":\"c\"}]\n"
	if got := buildAndRun(t, p); got != want {
		t.Errorf("the program printed\n%s\nwant, as Node printed,\n%s", got, want)
	}
	if src := Emit(p); strings.Contains(src, "ts_set") || strings.Contains(src, "ts_delete") {
		t.Errorf("a program that writes no optional property has a setter or a deleter:\n%s", src)
	}
}

// TestSpawnedObjectKeepsItsProperties: a spawn's object result is copied
// when the spawn has work left (M4 plan D11), and the copy has the
// properties the object had, in its order, written after its literal. The
// caller then writes and deletes one while the spawn's timer deletes another
// from the original, both stepping from the table the two share, under the
// race detector. Node shares the object, and prints the same, since the
// timer's steps end where they would have from the caller's.
//
//	interface Box { a: number; p?: number; q?: string }
//	const r: Box = await spawn((): Box => {
//	  const b: Box = { a: 1 };
//	  b.q = "q";
//	  b.p = 2;
//	  setTimeout(() => {
//	    delete b.q;
//	    b.p = undefined;
//	    console.log(b, "q" in b);
//	  }, 200);
//	  return b;
//	});
//	console.log(r, JSON.stringify(r));
//	r.q = "again";
//	delete r.p;
//	console.log(r, "p" in r);
//
// Node ran it with spawn = fn => Promise.resolve().then(fn).
func TestSpawnedObjectKeepsItsProperties(t *testing.T) {
	t.Parallel()
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("the race detector needs cgo, which this toolchain has disabled")
	}

	var shapes ir.Shapes
	s := shapes.Intern("Box", []ir.Field{{Name: "a", Type: ir.Number}, {Name: "p", Type: ir.MaybeOf(ir.Number), Optional: true}, {Name: "q", Type: ir.MaybeOf(ir.String), Optional: true}})
	box := ir.ObjectOf(s)
	b, r := stable("b", box), stable("r", box)
	timer := &ir.HostCall{Op: ir.HostSetTimeout, Typ: ir.Number, Span: span, Args: []ir.Expr{
		arrow(nil, ir.Void, del(b, "q"), set(b, "p", undef(ir.Number)), logAll(b, has(b, "q"))), num(200),
	}}
	fn := &ir.FuncLit{Result: box, Typ: ir.FuncType(nil, box), Concurrent: true, Span: span, Body: []ir.Stmt{
		decl("b", box, lit(s, "a", num(1))),
		set(b, "q", some(str("q"))),
		set(b, "p", some(num(2))),
		do(timer),
		ret(b),
	}}
	spawned := &ir.HostCall{Op: ir.HostSpawn, Args: []ir.Expr{fn}, Typ: ir.PromiseOf(box), Span: span}
	m := &ir.Module{Name: "t", File: "/src/t.ts", Async: true, Main: []ir.Stmt{
		decl("r", box, &ir.Await{Value: spawned, Typ: box, Span: span}),
		logAll(r, jsonOf(r)),
		set(r, "q", some(str("again"))),
		del(r, "p"),
		logAll(r, has(r, "p")),
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
	if want := "{ a: 1, q: 'q', p: 2 } {\"a\":1,\"q\":\"q\",\"p\":2}\n{ a: 1, q: 'again' } false\n{ a: 1, p: undefined } false\n"; stdout.String() != want {
		t.Errorf("the program printed\n%s\nwant, as Node printed,\n%s", stdout.String(), want)
	}
}
