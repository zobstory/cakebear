package backend

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestEmitOptional: writes, deletes and `in` on optional properties as the
// Go a reader would check them against. Each write or delete is a call of a
// method the shape's struct has only when the program uses it; a receiver
// that was narrowed or asserted is checked for its access; a write's
// receiver is ordered against a call in its value; and a literal receiver
// is parenthesised.
func TestEmitOptional(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	strM := ir.MaybeOf(ir.String)
	s := shapes.Intern("Opt", []ir.Field{
		{Name: "a", Type: ir.Number}, {Name: "b", Type: strM, Optional: true},
		{Name: "content-type", Type: strM, Optional: true}, {Name: "unused", Type: strM, Optional: true},
	})
	obj, objM := ir.ObjectOf(s), ir.MaybeOf(ir.ObjectOf(s))
	o, p, cur := stable("o", obj), stable("p", objM), ident("cur", obj)
	i := ident("i", ir.Number)
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{
			{Name: "name", Result: ir.String, Span: span, Body: []ir.Stmt{ret(str("n"))}},
			{Name: "g", Result: ir.Void, Span: span, Params: []ir.Param{{Name: "o", Type: obj}, {Name: "p", Type: objM}}, Body: []ir.Stmt{
				set(o, "b", some(str("x"))),
				del(o, "b"),
				logAll(has(o, "b"), has(o, "a")),
				set(o, "content-type", some(str("json"))),
				set(must(p), "b", some(str("y"))),
				del(asserted(p), "b"),
				logAll(has(must(p), "b")),
				set(lit(s, "a", num(1)), "b", some(str("z"))),
				del(lit(s, "a", num(2)), "b"),
				&ir.For{Init: []ir.Stmt{&ir.VarDecl{Name: "i", Type: ir.Number, Init: num(0), Mutable: true, Span: span}},
					Cond: binary(ir.OpLess, i, num(1), ir.Boolean), Post: []ir.Stmt{del(o, "b")},
					Body: []ir.Stmt{assign("i", num(1))}, Span: span},
			}},
		},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "cur", Type: obj, Init: lit(s, "a", num(0)), Mutable: true, Span: span},
			set(cur, "b", some(callOf("name", ir.String))),
		},
	}
	got := Emit(withShapes(m, &shapes))
	mustParse(t, got)
	for _, want := range []string{
		"// ts_set1 is `o.b = v`: an o without it gains its key, last.\n" +
			"func (o *ts_shape0) ts_set1(v cakebearrt.Maybe[string]) {\n" +
			"\to.ts_layout = o.ts_layout.With(1, \"b\")\n\to.b = v\n}\n",
		"// ts_delete1 is `delete o.b`.\n" +
			"func (o *ts_shape0) ts_delete1() {\n" +
			"\to.ts_layout = o.ts_layout.Without(1)\n\to.b = cakebearrt.Maybe[string]{}\n}\n",
		"// ts_set2 is `o[\"content-type\"] = v`: an o without it gains its key, last.\n" +
			"func (o *ts_shape0) ts_set2(v cakebearrt.Maybe[string]) {\n" +
			"\to.ts_layout = o.ts_layout.With(2, \"content-type\")\n\to.ts_x_content_u2D_type = v\n}\n",
		"\to.ts_set1(cakebearrt.Some[string](\"x\"))\n",
		"\to.ts_delete1()\n",
		"cakebearrt.Log(o.ts_layout.Has(1), o.ts_layout.Has(0))",
		"\tp.MustWrite(\"p\", \"'b'\", nil).ts_set1(cakebearrt.Some[string](\"y\"))\n",
		"\tp.AssertDelete(\"p\", nil).ts_delete1()\n",
		"p.MustIn(\"p\", \"'b'\", nil).ts_layout.Has(1)",
		"\t(&ts_shape0{ts_layout: ts_layout0, a: 1.0}).ts_set1(cakebearrt.Some[string](\"z\"))\n",
		"\t(&ts_shape0{ts_layout: ts_layout0, a: 2.0}).ts_delete1()\n",
		"; (i < 1.0); o.ts_delete1() {\n",
		// The call in the value could assign cur, so cur is read first.
		"\tts_read(cur).ts_set1(cakebearrt.Some[string](name(ts_domain)))\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
	// Only what the program uses: content-type is written and never
	// deleted, and unused neither.
	for _, unwanted := range []string{"ts_delete2", "ts_set3", "ts_delete3"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("emitted source has %s, which nothing uses:\n%s", unwanted, got)
		}
	}
}

// TestOptionalAccessOfAnEmptyPanics: a delete from, or an `in` on, a value
// TypeScript was wrong to think isn't empty throws the TypeError Node
// throws, which, uncaught, ends the program as Node's does, exit 1 (M6 plan
// D9, checkFailure).
//
//	interface O { a: number; p?: number }
//	const u: O | undefined = undefined;
//	delete u!.p;     // Node throws TypeError: Cannot convert undefined or null to object
//	const n: O | null = null;
//	"p" in n!;       // Node throws TypeError: Cannot use 'in' operator to search for 'p' in null
func TestOptionalAccessOfAnEmptyPanics(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	s := shapes.Intern("O", []ir.Field{{Name: "a", Type: ir.Number}, {Name: "p", Type: ir.MaybeOf(ir.Number), Optional: true}})
	objM := ir.MaybeOf(ir.ObjectOf(s))
	for _, tt := range []struct {
		main []ir.Stmt
		want string
	}{
		{[]ir.Stmt{decl("u", objM, undef(ir.ObjectOf(s))), del(asserted(stable("u", objM)), "p")},
			"TypeError: Cannot convert undefined or null to object"},
		{[]ir.Stmt{decl("n", objM, null(ir.ObjectOf(s))), logAll(has(asserted(stable("n", objM)), "p"))},
			"TypeError: Cannot use 'in' operator to search for 'p' in null"},
		{[]ir.Stmt{decl("n", objM, null(ir.ObjectOf(s))), set(must(stable("n", objM)), "p", some(num(1)))},
			"TypeError: Cannot set properties of null (setting 'p')"},
	} {
		out := filepath.Join(t.TempDir(), "prog")
		if src, err := Build(withShapes(&ir.Module{Name: "t", Main: tt.main}, &shapes), Options{Output: out}); err != nil {
			t.Fatalf("Build: %v\n%s", err, src)
		}
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(out)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		checkFailure(t, err, stdout.String(), stderr.String(), tt.want)
	}
}
