package backend

import (
	"go/token"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// withShapes is the program of m alone, holding shapes.
func withShapes(m *ir.Module, shapes *ir.Shapes) *ir.Program {
	p := ir.ProgramOf(m)
	p.Shapes = shapes.All()
	return p
}

// lit is a literal of shape s: its keys and values alternate, in source
// order.
func lit(s *ir.Shape, kv ...any) *ir.ObjectLit {
	x := &ir.ObjectLit{Typ: ir.ObjectOf(s), Span: span}
	for i := 0; i < len(kv); i += 2 {
		x.Fields = append(x.Fields, ir.FieldInit{Field: s.Index(kv[i].(string)), Value: kv[i+1].(ir.Expr), Span: span})
	}
	return x
}

func get(object ir.Expr, name string) *ir.FieldGet {
	s := object.ExprType().Shape
	i := s.Index(name)
	return &ir.FieldGet{Object: object, Field: i, Typ: s.Fields[i].Type, Span: span}
}

func set(object ir.Expr, name string, value ir.Expr) *ir.FieldSet {
	return &ir.FieldSet{Object: object, Field: object.ExprType().Shape.Index(name), Value: value, Span: span}
}

// stable is a read of a binding nothing assigns after its declaration.
func stable(name string, t ir.Type) *ir.Ident {
	return &ir.Ident{Name: name, Stable: true, Typ: t, Span: span}
}

func str(s string) ir.Expr { return &ir.StringLit{Value: s, Span: span} }

func logAll(args ...ir.Expr) ir.Stmt {
	return &ir.ExprStmt{Expr: &ir.ConsoleLog{Args: args, Span: span}, Span: span}
}

func point(shapes *ir.Shapes) *ir.Shape {
	return shapes.Intern("Point", []ir.Field{{Name: "x", Type: ir.Number}, {Name: "y", Type: ir.Number}})
}

// TestEmitObject: a shape's struct, its methods and key table, a literal, and
// a field read ordered against a call, as the Go a reader would check them
// against.
func TestEmitObject(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	p := ident("p", ir.ObjectOf(pt))
	m := &ir.Module{Name: "t",
		Funcs: []*ir.Func{{Name: "bump", Result: ir.Number, Span: span, Body: returns(num(1))}},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "p", Type: ir.ObjectOf(pt), Init: lit(pt, "y", num(2), "x", num(1)), Mutable: true, Span: span},
			logNum(binary(ir.OpAdd, get(p, "x"), &ir.Call{Callee: "bump", Typ: ir.Number, Span: span}, ir.Number)),
			logNum(get(p, "y")),
		}}
	got := Emit(withShapes(m, &shapes))
	mustParse(t, got)
	for _, want := range []string{
		"// ts_shape0 is Point.\ntype ts_shape0 struct {\n\tts_layout *cakebearrt.Layout\n\tx float64\n\ty float64\n}\n",
		"func (o *ts_shape0) Layout() *cakebearrt.Layout { return o.ts_layout }\n",
		"func (o *ts_shape0) Field(i int) any {\n\tswitch i {\n\tcase 0:\n\t\treturn o.x\n\tcase 1:\n\t\treturn o.y\n\t}\n",
		"\tcopied := new(ts_shape0)\n\tc.Remember(o, copied)\n\t*copied = *o\n\treturn copied\n",
		`var ts_layout0 = cakebearrt.NewLayout([]string{"y", "x"}, []int{1, 0})`,
		"var p *ts_shape0\n",
		"\tp = &ts_shape0{ts_layout: ts_layout0, y: 2.0, x: 1.0}\n",
		"cakebearrt.LogNumber((ts_read(p.x) + bump(ts_domain)))",
		// With no call to race with, a read is left alone.
		"cakebearrt.LogNumber(p.y)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted Go lacks %q:\n%s", want, got)
		}
	}
}

// TestFieldName: every property, however it is spelled, gets a Go field
// name of its own, never the key table's or a method's.
func TestFieldName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"x": "x", "type": "ts_type", "$x": "ts_x__Dx", "content-type": "ts_x_content_u2D_type",
		"Layout": "ts_Layout", "Field": "ts_Field", "CloneObject": "ts_CloneObject",
		"ts_Layout": "ts_ts_Layout", "ts_layout": "ts_ts_layout", "": "ts_x_", "1st": "ts_x_1st", "_": "ts_blank",
	} {
		if got := fieldName(name); got != want {
			t.Errorf("fieldName(%q) = %q, want %q", name, got, want)
		}
	}
	names := []string{"x", "type", "ts_type", "$x", "ts_x__Dx", "_Dx", "Layout", "ts_Layout", "Field", "CloneObject",
		"layout", "ts_layout", "", "_", "ts_blank", "blank", "1st", "ts_x_1st", "x_1st", "٣", "line\nbreak", "a b", "日本"}
	seen := map[string]string{}
	for _, name := range names {
		got := fieldName(name)
		if !token.IsIdentifier(got) || got == layoutField || objectMethods[got] {
			t.Errorf("fieldName(%q) = %q, which a struct field can't be", name, got)
		}
		if other, ok := seen[got]; ok {
			t.Errorf("fieldName(%q) and fieldName(%q) are both %q", name, other, got)
		}
		seen[got] = name
	}
}

// TestMangleLeavesShapeNamesFree: no identifier mangles onto a shape's
// struct or a key table, which are package-level names like a function's.
func TestMangleLeavesShapeNamesFree(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	generated := map[string]bool{shapeName(point(&shapes)): true, "ts_layout0": true}
	for _, name := range []string{"shape0", "ts_shape0", "layout0", "ts_layout0", "ts_layout", "shape", "layout"} {
		if got := mangle(name); generated[got] {
			t.Errorf("mangle(%q) = %q, which the emitter uses itself", name, got)
		}
	}
}

func TestGoObjectType(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	for _, tt := range []struct {
		in   ir.Type
		want string
	}{
		{ir.ObjectOf(pt), "*ts_shape0"},
		{ir.FuncType([]ir.Type{ir.ObjectOf(pt)}, ir.ObjectOf(pt)), "func(*cakebearrt.Domain, *ts_shape0) *ts_shape0"},
		{ir.PromiseOf(ir.ObjectOf(pt)), "*cakebearrt.Promise[*ts_shape0]"},
	} {
		if got, ok := goType(tt.in); !ok || got != tt.want {
			t.Errorf("goType(%s) = %q, %v, want %q", tt.in, got, ok, tt.want)
		}
		if !mentionsRuntime(tt.in) {
			t.Errorf("mentionsRuntime(%s) = false", tt.in)
		}
	}
	if got, ok := goType(ir.Type{Kind: ir.KindObject}); ok {
		t.Errorf("goType of an object with no shape = %q, want no spelling", got)
	}
}

// TestUnspellableFieldIsUnsupported: a shape has no span, so a field the
// backend can't spell yet is reported where the program first uses the
// shape, or at the entry module if nothing does, and never emitted as a
// stand-in.
func TestUnspellableFieldIsUnsupported(t *testing.T) {
	t.Parallel()

	at := ir.Span{File: "t.ts", Line: 4, Col: 2}
	for _, tt := range []struct {
		name string
		used bool
		want ir.Span
	}{
		{"used", true, at},
		{"unused", false, ir.Span{File: "t.ts"}},
	} {
		var shapes ir.Shapes
		list := shapes.Intern("List", []ir.Field{{Name: "items", Type: ir.HostOf(ir.HostNone)}})
		// The shape reached through a field is a use of it too.
		box := shapes.Intern("Box", []ir.Field{{Name: "list", Type: ir.ObjectOf(list)}})
		m := &ir.Module{Name: "t", File: "t.ts"}
		if tt.used {
			m.Funcs = []*ir.Func{{Name: "f", Params: []ir.Param{{Name: "b", Type: ir.ObjectOf(box), Span: at}}, Result: ir.Void, Span: at}}
		}
		src, unsupported, err := emit(withShapes(m, &shapes))
		if err != nil {
			t.Fatalf("%s: emit: %v", tt.name, err)
		}
		if len(unsupported) != 1 || unsupported[0].Span != tt.want || !strings.Contains(unsupported[0].What, "List yet: its items is a host") {
			t.Errorf("%s: unsupported = %+v, want one at %v naming List's items\n%s", tt.name, unsupported, tt.want, src)
		}
	}
}

// TestPropertyOrder: array indices first, ascending, then the rest as
// written, which is how Node lists them; anything not an index's canonical
// spelling is one of the rest.
func TestPropertyOrder(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]bool{
		"0": true, "9": true, "10": true, "4294967294": true,
		"4294967295": false, "01": false, "-1": false, "+1": false, "1.5": false, "1e3": false, " 1": false,
		"": false, "٣": false, "99999999999": false, "x": false,
	} {
		if _, got := arrayIndex(key); got != want {
			t.Errorf("arrayIndex(%q) = %v, want %v", key, got, want)
		}
	}
}

// TestCommentSafe: a name that would break a line comment is quoted.
func TestCommentSafe(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"Point": "Point", "{ x: number }": "{ x: number }", "日本": "日本",
		"{ line\nbreak: number }": `"{ line\nbreak: number }"`, "a\x00b": `"a\x00b"`, "bom\ufeff": `"bom\ufeff"`, "\xff": `"\xff"`,
	} {
		if got := commentSafe(in); got != want {
			t.Errorf("commentSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFieldSetOrdersItsObject: JavaScript evaluates the object of `p.x = v`
// before v, so with a call in v, a p code writes to is read through ts_read;
// one nothing writes to, or a v with no call, needs nothing.
func TestFieldSetOrdersItsObject(t *testing.T) {
	t.Parallel()

	var shapes ir.Shapes
	pt := point(&shapes)
	g := &ir.Call{Callee: "g", Typ: ir.Number, Span: span}
	for _, tt := range []struct {
		s    *ir.FieldSet
		want string
	}{
		{set(ident("p", ir.ObjectOf(pt)), "x", g), "ts_read(p).x = g(ts_domain)"},
		{set(stable("p", ir.ObjectOf(pt)), "x", g), "\tp.x = g(ts_domain)"},
		{set(ident("p", ir.ObjectOf(pt)), "x", num(1)), "\tp.x = 1.0"},
		{set(ident("p", ir.ObjectOf(pt)), "x", get(ident("q", ir.ObjectOf(pt)), "y")), "\tp.x = q.y"},
		{set(ident("p", ir.ObjectOf(pt)), "x", binary(ir.OpAdd, get(ident("q", ir.ObjectOf(pt)), "y"), g, ir.Number)),
			"ts_read(p).x = (ts_read(q.y) + g(ts_domain))"},
		// The object is a call: the value's reads come after it.
		{set(&ir.Call{Callee: "h", Typ: ir.ObjectOf(pt), Span: span}, "x", get(ident("q", ir.ObjectOf(pt)), "y")),
			"\th(ts_domain).x = ts_read(q.y)"},
	} {
		m := &ir.Module{Name: "t", Funcs: []*ir.Func{{
			Name: "f", Result: ir.Void, Span: span,
			Params: []ir.Param{{Name: "p", Type: ir.ObjectOf(pt)}, {Name: "q", Type: ir.ObjectOf(pt)}},
			Body:   []ir.Stmt{tt.s},
		}}}
		if got := Emit(withShapes(m, &shapes)); !strings.Contains(got, tt.want+"\n") {
			t.Errorf("emitted Go lacks %q:\n%s", tt.want, got)
		}
	}
}
