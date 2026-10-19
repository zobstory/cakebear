package backend

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zobstory/cakebear/ir"
)

// Objects (M4 plan D2, D4). Each shape in ir.Program.Shapes becomes a Go
// struct, and an object is a pointer to one, so `===` is Go's pointer ==, and
// a write through one alias is seen through every other, as in JavaScript:
//
//	// ts_shape0 is Point.
//	type ts_shape0 struct {
//		ts_layout *cakebearrt.Layout
//		x float64
//		y float64
//	}
//
// The fields are in the shape's order, which is canonical. The order a
// literal lists its keys in, which is the order Node prints them in, is the
// literal's: it points ts_layout at a key table, one per shape and key order,
// shared by every literal that lists them so (ts_layout<k>). Three methods
// make the struct a cakebearrt.Object, which is all the runtime knows of it:
// Layout, Field by the shape's index, and CloneObject for spawn's copy (D11).
//
// No user name collides with these: mangle produces a ts_ name only from a
// Go keyword, a predeclared name or one already starting ts_, and none of
// those is shape or layout followed by a digit.

// layoutField is the struct field holding an object's key table. fieldName
// never gives a property this name: mangle turns `ts_layout` into
// ts_ts_layout.
const layoutField = "ts_layout"

// objectMethods are the methods of every shape's struct. Go rejects a field
// with the name of a method.
var objectMethods = map[string]bool{"Layout": true, "Field": true, "CloneObject": true}

func shapeName(s *ir.Shape) string { return "ts_shape" + strconv.Itoa(s.ID) }

// goObjectType spells an object of shape s: a pointer to its struct.
func goObjectType(s *ir.Shape) (string, bool) {
	if s == nil {
		return "", false
	}
	return "*" + shapeName(s), true
}

// fieldName is the Go name of the field holding property name. It is
// mangle's, but for two cases a property has and an identifier doesn't. A
// property can be any string, so one that is empty or starts with a digit is
// escaped, as a name holding a rune Go rejects is. And one named as a method
// gets a ts_ prefix. Both keep the mapping injective: every other ts_x_ name
// comes from escape, which reads back to one original, and mangle prefixes
// any name already starting ts_, so a property `ts_Layout` is ts_ts_Layout.
func fieldName(name string) string {
	if r, _ := utf8.DecodeRuneInString(name); name == "" || unicode.IsDigit(r) {
		return escapedPrefix + escape(name)
	}
	if objectMethods[name] {
		return "ts_" + name
	}
	return mangle(name)
}

// noteShapes records at as where the program first uses each shape t
// reaches, through its fields too. A shape has no span of its own, so a field
// the backend can't spell is reported there (shapeDecl).
func (e *emitter) noteShapes(t ir.Type, at ir.Span) {
	switch t.Kind {
	case ir.KindObject:
		if t.Shape == nil {
			return
		}
		if _, ok := e.shapeUse[t.Shape]; ok {
			return
		}
		if e.shapeUse == nil {
			e.shapeUse = map[*ir.Shape]ir.Span{}
		}
		e.shapeUse[t.Shape] = at
		for _, f := range t.Shape.Fields {
			e.noteShapes(f.Type, at)
		}
	case ir.KindFunc:
		if t.Sig != nil {
			for _, p := range t.Sig.Params {
				e.noteShapes(p, at)
			}
			e.noteShapes(t.Sig.Result, at)
		}
	case ir.KindPromise, ir.KindArray, ir.KindMaybe:
		if t.Elem != nil {
			e.noteShapes(*t.Elem, at)
		}
	}
}

// shapeDecls renders every shape's struct and methods, in the order of their
// IDs. They are rendered after everything else, once the program's uses of
// them are known: a shape nothing uses is reported, if it must be, at
// unused.
func (e *emitter) shapeDecls(shapes []*ir.Shape, unused ir.Span) string {
	var b strings.Builder
	for _, s := range shapes {
		at, ok := e.shapeUse[s]
		if !ok {
			at = unused
		}
		e.shapeDecl(&b, s, at)
	}
	return b.String()
}

// shapeDecl renders s's struct and the methods that make it an Object.
func (e *emitter) shapeDecl(b *strings.Builder, s *ir.Shape, at ir.Span) {
	name := shapeName(s)
	fmt.Fprintf(b, "// %s is %s.\n", name, commentSafe(s.String()))
	fmt.Fprintf(b, "type %s struct {\n\t%s *%s.Layout\n", name, layoutField, rtPkg)
	for _, f := range s.Fields {
		typ, ok := goType(f.Type)
		if !ok {
			typ = e.cannot(at, "the backend cannot emit a value of type "+s.String()+" yet: its "+f.Name+" is a "+f.Type.String())
		}
		fmt.Fprintf(b, "\t%s %s", fieldName(f.Name), typ)
		if fieldName(f.Name) != f.Name {
			fmt.Fprintf(b, " // %s", strconv.Quote(f.Name))
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "func (o *%s) Layout() *%s.Layout { return o.%s }\n\n", name, rtPkg, layoutField)

	fmt.Fprintf(b, "func (o *%s) Field(i int) any {\n", name)
	if len(s.Fields) > 0 {
		b.WriteString("\tswitch i {\n")
		for i, f := range s.Fields {
			fmt.Fprintf(b, "\tcase %d:\n\t\treturn o.%s\n", i, fieldName(f.Name))
		}
		b.WriteString("\t}\n")
	}
	fmt.Fprintf(b, "\tpanic(%s)\n}\n\n", strconv.Quote("cakebear: "+s.String()+" has no field of that index"))

	// The copy is remembered before any field is copied, so a field leading
	// back to o finds it rather than starting another: a cycle stays a cycle.
	// It starts as o, its table too, so it has the optional properties o has
	// at the time: tables are never changed, only replaced.
	fmt.Fprintf(b, "func (o *%s) CloneObject(c *%s.Cloner) %s.Object {\n", name, rtPkg, rtPkg)
	fmt.Fprintf(b, "\tcopied := new(%s)\n\tc.Remember(o, copied)\n\t*copied = *o\n", name)
	for _, f := range s.Fields {
		if cloned(f.Type) {
			fmt.Fprintf(b, "\tcopied.%s = %s.CloneValue(c, o.%[1]s)\n", fieldName(f.Name), rtPkg)
		}
	}
	b.WriteString("\treturn copied\n}\n\n")
	e.optionalMethods(b, s)
}

// commentSafe returns s as it can stand in a line comment: as it is, or
// quoted if any rune of it can't. A property name can hold a newline, which
// would end the comment and leave the rest of the name to be read as Go.
func commentSafe(s string) string {
	if utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return !strconv.IsPrint(r) }) {
		return s
	}
	return strconv.Quote(s)
}

// objectLit emits a literal as a composite literal with its fields keyed in
// source order. Go evaluates the calls in a composite literal left to right,
// as it does any expression's, so source order is JavaScript's order of
// evaluation, and order.go orders the reads among them.
func (e *emitter) objectLit(x *ir.ObjectLit) string {
	s := x.Typ.Shape
	e.noteShapes(x.Typ, x.Span)
	parts := []string{layoutField + ": " + e.layout(s, propertyOrder(s, x))}
	for _, f := range x.Fields {
		parts = append(parts, fieldName(s.Fields[f.Field].Name)+": "+e.expr(f.Value))
	}
	return "&" + shapeName(s) + "{" + strings.Join(parts, ", ") + "}"
}

// layout names the package-level key table for an object of shape s whose
// properties are listed in order, by shape field index, declaring it the
// first time that pair is met.
func (e *emitter) layout(s *ir.Shape, order []int) string {
	key := shapeName(s) + fmt.Sprint(order)
	if name, ok := e.layouts[key]; ok {
		return name
	}
	name := "ts_layout" + strconv.Itoa(len(e.layoutDecls))
	keys := make([]string, len(order))
	fields := make([]string, len(order))
	for i, f := range order {
		keys[i] = strconv.Quote(s.Fields[f].Name)
		fields[i] = strconv.Itoa(f)
	}
	e.layoutDecls = append(e.layoutDecls, fmt.Sprintf("var %s = %s.NewLayout([]string{%s}, []int{%s})\n",
		name, rtPkg, strings.Join(keys, ", "), strings.Join(fields, ", ")))
	if e.layouts == nil {
		e.layouts = map[string]string{}
	}
	e.layouts[key] = name
	return name
}

// propertyOrder is the order JavaScript lists a literal's properties in,
// which is the order Node prints them and JSON.stringify writes them: the
// array indices first, ascending, then the other keys as the literal wrote
// them (OrdinaryOwnPropertyKeys). `{ b: 1, 2: 'x', 1: 'y' }` prints as
// `{ '1': 'y', '2': 'x', b: 1 }` on Node 24.18. It is the key table's order
// only: the values are still evaluated in source order.
func propertyOrder(s *ir.Shape, x *ir.ObjectLit) []int {
	order := make([]int, len(x.Fields))
	for i, f := range x.Fields {
		order[i] = f.Field
	}
	slices.SortStableFunc(order, func(a, b int) int {
		ia, aIndex := arrayIndex(s.Fields[a].Name)
		ib, bIndex := arrayIndex(s.Fields[b].Name)
		switch {
		case aIndex && bIndex:
			return cmp.Compare(ia, ib)
		case aIndex:
			return -1
		case bIndex:
			return 1
		}
		return 0
	})
	return order
}

// arrayIndex reports whether key is an array index, the canonical decimal
// spelling of an integer below 2^32 − 1, and if so which. Node lists
// "4294967295", "01" and "-1" among the other keys, in the order written.
func arrayIndex(key string) (uint64, bool) {
	if key == "" || len(key) > 10 || (key[0] == '0' && len(key) > 1) {
		return 0, false
	}
	n, err := strconv.ParseUint(key, 10, 64)
	return n, err == nil && n < 1<<32-1
}

// fieldGet emits obj.f. A field is state any call can change, so in an
// expression whose reads are being ordered it is read through readFunc, as
// a variable is (order.go): the argument is evaluated when the call runs, so
// the whole path, `ts_read(p.a.b)`, is read in call order.
func (e *emitter) fieldGet(x *ir.FieldGet) string {
	if !e.order {
		return e.object(x.Object, reading(keyOf(x.Object, x.Field))) + "." + fieldOf(x.Object, x.Field)
	}
	e.usesRead = true
	return readFunc + "(" + e.path(x) + ")"
}

// path emits a field read inside readFunc's argument: the binding and the
// fields leading to it are read as that argument, so none is wrapped again.
// Anything else the path starts from is ordered as usual.
func (e *emitter) path(x *ir.FieldGet) string {
	var object string
	switch o := x.Object.(type) {
	case *ir.Ident:
		object = e.top(o.Module, o.Name)
	case *ir.FieldGet:
		object = e.path(o)
	default:
		object = e.object(o, reading(keyOf(o, x.Field)))
	}
	return object + "." + fieldOf(x.Object, x.Field)
}

// fieldSet emits obj.f = v. JavaScript evaluates obj before v, and Go orders
// only the calls in an assignment, so a call in either side orders the reads
// of both: `ts_read(p).x = g()` writes to the object p held before g ran.
// An optional f's write is a call of its setter, which gives an object that
// lacks f its key (emit_optional.go): a plain write would leave the key out
// of the object's table, so the value would never print or serialise.
func (e *emitter) fieldSet(s *ir.FieldSet) string {
	saved := e.order
	e.order = isCall(s.Object) || innerCall(s.Object) || isCall(s.Value) || innerCall(s.Value)
	defer func() { e.order = saved }()
	object := e.object(s.Object, writing(keyOf(s.Object, s.Field)))
	if s.Object.ExprType().Shape.Fields[s.Field].Optional {
		return e.optionalSet(s, object)
	}
	return object + "." + fieldOf(s.Object, s.Field) + " = " + e.expr(s.Value)
}

// object emits an expression whose field is selected, the receiver of a. A
// literal is parenthesised: `&T{…}.f` would take the address of the field.
func (e *emitter) object(x ir.Expr, a access) string {
	if _, ok := x.(*ir.ObjectLit); ok {
		return "(" + e.expr(x) + ")"
	}
	return e.receiver(x, a)
}

// fieldOf names field i of object's shape, which Validate has checked, and
// keyOf is its property's key.
func fieldOf(object ir.Expr, i int) string { return fieldName(keyOf(object, i)) }
func keyOf(object ir.Expr, i int) string   { return object.ExprType().Shape.Fields[i].Name }
