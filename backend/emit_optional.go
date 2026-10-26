package backend

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// Optional properties (M5 plan D3). Whether an object has one is its key
// table's business: the runtime prints and serialises the keys the table
// lists, so a property's key moves with it, onto the table when the program
// writes it and off when it deletes it (runtime.Layout's With and Without).
// The struct field holds a Maybe, or an unknown's any, which is undefined,
// its zero, whenever the object lacks the property, so a read needs no look
// at the table.
//
// A write to an optional property, and a delete of one, is a method of the
// shape's struct, one per field and kind, generated for those the program
// uses:
//
//	// ts_set1 is `o.b = v`: an o without b gains its key, last.
//	func (o *ts_shape0) ts_set1(v cakebearrt.Maybe[string]) {
//		o.ts_layout = o.ts_layout.With(1, "b")
//		o.b = v
//	}
//
//	// ts_delete1 is `delete o.b`, after which o.b reads undefined.
//	func (o *ts_shape0) ts_delete1() {
//		o.ts_layout = o.ts_layout.Without(1)
//		o.b = cakebearrt.Maybe[string]{}
//	}
//
// A method keeps JavaScript's order with nothing more: Go evaluates the
// receiver and then the argument before the call, and the table changes
// inside it, so in `o.b = JSON.stringify(o)` the value is of an o still
// without b. order.go orders the receiver's read against a call in the
// value, as for any field write.
//
// No property's field has a method's name: mangle gives a ts_ name only to
// a keyword, a predeclared name or a name starting ts_ already, and none of
// the first two is set or delete followed by digits.

// optionalOp is one of the generated methods: field's write, or its delete,
// in shape's struct.
type optionalOp struct {
	shape  *ir.Shape
	field  int
	delete bool
}

func setterName(field int) string  { return "ts_set" + strconv.Itoa(field) }
func deleterName(field int) string { return "ts_delete" + strconv.Itoa(field) }

// useOptional records that the program needs op's method.
func (e *emitter) useOptional(op optionalOp) {
	if e.optionals == nil {
		e.optionals = map[optionalOp]bool{}
	}
	e.optionals[op] = true
}

// optionalSet emits `obj.f = v` for an optional f, given obj as fieldSet
// emitted it, as a call of f's setter.
func (e *emitter) optionalSet(s *ir.FieldSet, object string) string {
	e.useOptional(optionalOp{shape: s.Object.ExprType().Shape, field: s.Field})
	return object + "." + setterName(s.Field) + "(" + e.expr(s.Value) + ")"
}

// fieldDelete emits `delete obj.f`, a call of f's deleter. Only obj is
// evaluated, so it is ordered as a whole statement's expression is (full).
func (e *emitter) fieldDelete(s *ir.FieldDelete) string {
	e.useOptional(optionalOp{shape: s.Object.ExprType().Shape, field: s.Field, delete: true})
	saved := e.order
	e.order = innerCall(s.Object)
	defer func() { e.order = saved }()
	return e.object(s.Object, deleting(s.Span)) + "." + deleterName(s.Field) + "()"
}

// hasField emits `"f" in obj` as a look at obj's table, which lists a
// required field always and an optional one once it is written, even with
// undefined. Has is a call, so Go orders it among the calls around it, and
// obj.ts_layout is read before it, lexically.
func (e *emitter) hasField(x *ir.HasField) string {
	return e.object(x.Object, searching(keyOf(x.Object, x.Field), x.Span)) + "." + layoutField + ".Has(" + strconv.Itoa(x.Field) + ")"
}

// optionalMethods renders the setters and deleters of s's fields that the
// program uses, after the methods every shape has (shapeDecl).
func (e *emitter) optionalMethods(b *strings.Builder, s *ir.Shape) {
	name := shapeName(s)
	for i, f := range s.Fields {
		// A field the backend can't spell has been reported in the struct.
		typ, ok := goType(f.Type)
		if !ok {
			continue
		}
		field, key := fieldName(f.Name), strconv.Quote(f.Name)
		if e.optionals[optionalOp{shape: s, field: i}] {
			fmt.Fprintf(b, "// %s is `o%s = v`: an o without it gains its key, last.\n", setterName(i), propertyAccess(f.Name))
			fmt.Fprintf(b, "func (o *%s) %s(v %s) {\n", name, setterName(i), typ)
			fmt.Fprintf(b, "\to.%s = o.%[1]s.With(%d, %s)\n\to.%s = v\n}\n\n", layoutField, i, key, field)
		}
		if e.optionals[optionalOp{shape: s, field: i, delete: true}] {
			// The value goes with the key, so a read of the field reads
			// undefined without looking at the table.
			zero := typ + "{}"
			if f.Type == ir.Unknown {
				zero = "nil"
			}
			fmt.Fprintf(b, "// %s is `delete o%s`.\n", deleterName(i), propertyAccess(f.Name))
			fmt.Fprintf(b, "func (o *%s) %s() {\n", name, deleterName(i))
			fmt.Fprintf(b, "\to.%s = o.%[1]s.Without(%d)\n\to.%s = %s\n}\n\n", layoutField, i, field, zero)
		}
	}
}
