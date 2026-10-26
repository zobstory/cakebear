package backend

import (
	"fmt"
	"strconv"
	"unicode"

	"github.com/zobstory/cakebear/ir"
)

// Null and undefined (M5 plan D2-D8, D12). A `T | null | undefined` is a
// cakebearrt.Maybe[T], a value holding a T and a state, whose zero value is
// undefined (runtime/maybe.go):
//
//	var v cakebearrt.Maybe[float64] = cakebearrt.Some[float64](5.0)
//	if v.IsEmpty() { … }
//	cakebearrt.LogNumber(v.Must("v") + 1.0)
//
// A Maybe that is null or undefined holds T's zero value and nothing else,
// so Go's == on two Maybes is JavaScript's ===, and `===` and `!==` between
// two of them are emitted as any other operands' are (binary).

// goMaybeType spells `T | null | undefined`.
func goMaybeType(t ir.Type) (string, bool) {
	if t.Elem == nil {
		return "", false
	}
	elem, ok := goType(*t.Elem)
	return rtPkg + ".Maybe[" + elem + "]", ok
}

// maybe emits the nodes that move a value between its states. Cond and Let
// are cond's (emit_cond.go).
func (e *emitter) maybe(x ir.Expr) string {
	e.usesRuntime = true
	switch x := x.(type) {
	case *ir.Wrap:
		// The type argument is spelled out, as an array literal's is, so the
		// Maybe is of the slot's T whatever Go would infer from the value.
		return fmt.Sprintf("%s.Some[%s](%s)", rtPkg, e.typ(*x.Typ.Elem, x.Span), e.expr(x.Value))
	case *ir.Unwrap:
		return e.unwrap(x, access{})
	case *ir.Empty:
		if x.Null {
			return fmt.Sprintf("%s.NullOf[%s]()", rtPkg, e.typ(*x.Typ.Elem, x.Span))
		}
		return e.typ(x.Typ, x.Span) + "{}"
	case *ir.IsEmpty:
		test := "IsEmpty" // `x == null`
		switch {
		case !x.Null:
			test = "IsUndefined"
		case !x.Undefined:
			test = "IsNull"
		}
		if x.Value.ExprType() == ir.Unknown { // tested as it is (runtime/unknown.go)
			return fmt.Sprintf("%s.%sUnknown(%s)", rtPkg, test, e.expr(x.Value))
		}
		return e.operand(x.Value) + "." + test + "()"
	}
	return e.cannot(x.ExprSpan(), describeUnsupported(x))
}

// operand emits x as the receiver of a method. An undefined Empty is a
// composite literal, which Go reads as the block of an if or a for when it
// starts the condition, so it is parenthesised. A binary operation needs
// nothing: binary parenthesises the whole of it.
func (e *emitter) operand(x ir.Expr) string {
	if empty, ok := x.(*ir.Empty); ok && !empty.Null {
		return "(" + e.expr(x) + ")"
	}
	return e.expr(x)
}

// An access is what a program does to a value beyond using it: read a
// property of it, write one, or iterate it. Node does each only to a value
// that is neither null nor undefined, and throws for one that is, where a
// value only used carries on. So an Unwrap that is an access's receiver
// fails saying Node throws, and how (runtime/maybe.go), at the place the
// node making the access gives (emit_site.go). The zero access is a use.
type access struct {
	kind string  // the suffix of the runtime's method: Read, Write, Delete, In or Iterate
	key  string  // the property, as Node's TypeError names it
	at   ir.Span // where an uncaught report of the failure points
}

// iterating is for…of's access, and deleting delete's: Node's TypeError for
// either names no property.
func iterating(at ir.Span) access { return access{kind: "Iterate", at: at} }
func deleting(at ir.Span) access  { return access{kind: "Delete", at: at} }

// reading, writing and searching are accesses of the property named key,
// searching being `"key" in x`'s.
func reading(key string, at ir.Span) access   { return access{"Read", quoteKey(key), at} }
func writing(key string, at ir.Span) access   { return access{"Write", quoteKey(key), at} }
func searching(key string, at ir.Span) access { return access{"In", quoteKey(key), at} }

// quoteKey quotes a property as Node's TypeError does: 'x', 'a'b'.
func quoteKey(key string) string { return "'" + key + "'" }

// elementAccess is the access of xs[i]. Node's TypeError names i's value,
// which Node evaluates before it throws, and cakebear's check fails before
// i is evaluated, so only a number the source writes is named, and any
// other index is "an index".
func elementAccess(index ir.Expr, write bool, at ir.Span) access {
	key := "an index"
	if n, ok := index.(*ir.NumberLit); ok {
		key = quoteKey(indexName(n))
	}
	if write {
		return access{"Write", key, at}
	}
	return access{"Read", key, at}
}

// receiver emits x as the receiver of a, as itself, or, when it is an
// Unwrap, checked for a.
func (e *emitter) receiver(x ir.Expr, a access) string {
	if u, ok := x.(*ir.Unwrap); ok {
		return e.unwrap(u, a)
	}
	return e.expr(x)
}

// unwrap emits x for the access a makes of it: Must where the checker
// narrowed it and Assert where the program's own `!` did, each followed by
// a's kind, so that an empty one fails saying what Node would have done
// (runtime/maybe.go): `p.MustRead("p", "'x'").x`.
func (e *emitter) unwrap(x *ir.Unwrap, a access) string {
	method := "Must"
	if x.Asserted {
		method = "Assert"
	}
	args := strconv.Quote(e.unwrapped(x.Value))
	if a.key != "" {
		args += ", " + strconv.Quote(a.key)
	}
	if a.kind != "" {
		args += ", " + e.siteArg(a.at) // the forms Node throws for take a site
	}
	return fmt.Sprintf("%s.%s%s(%s)", e.operand(x.Value), method, a.kind, args)
}

// unwrapped names the value an Unwrap reads, as the source writes it, for
// Must's message: `user`, `user.name`, `xs[i]`, `find(…)`. Anything else is
// named by where it is.
func (e *emitter) unwrapped(x ir.Expr) string {
	if name, ok := e.sourceName(x); ok {
		return name
	}
	return "the value at " + x.ExprSpan().String()
}

// sourceName is x as the source writes it, when x is a name, a path of
// fields and elements from one, or a call: a binding by its TypeScript name,
// not its Go one, and a Let's by the name of the value it holds.
func (e *emitter) sourceName(x ir.Expr) (string, bool) {
	switch x := x.(type) {
	case *ir.Ident:
		if h, ok := e.held[x.Name]; ok && x.Module == "" {
			return h.name, h.named
		}
		return x.Name, true
	case *ir.Unwrap:
		return e.sourceName(x.Value)
	case *ir.FieldGet:
		object, ok := e.sourceName(x.Object)
		return object + propertyAccess(x.Object.ExprType().Shape.Fields[x.Field].Name), ok
	case *ir.IndexGet:
		array, ok := e.sourceName(x.Array)
		return array + "[" + indexName(x.Index) + "]", ok
	case *ir.Call:
		return x.Callee + argsName(x.Args), true
	case *ir.ArrayOp:
		array, ok := e.sourceName(x.Array)
		return array + "." + x.Method.String() + argsName(x.Args), ok
	case *ir.BuiltinCall:
		if !isMethod(x.Fn) || len(x.Args) == 0 {
			return "", false
		}
		s, ok := e.sourceName(x.Args[0])
		switch x.Fn {
		case ir.StrIndex, ir.StrIndexOrUndefined:
			return s + "[" + indexName(x.Args[1]) + "]", ok
		case ir.StrLength:
			return s + ".length", ok
		}
		return s + "." + x.Fn.String() + argsName(x.Args[1:]), ok
	}
	return "", false
}

// propertyAccess is how the source reads property name: `.name`, or
// `["content-type"]` for a name that isn't an identifier.
func propertyAccess(name string) string {
	for i, r := range name {
		if r != '_' && r != '$' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
			return "[" + strconv.Quote(name) + "]"
		}
	}
	if name == "" {
		return `[""]`
	}
	return "." + name
}

// indexName is an index as the source writes it, when it is a name or a
// number, and an ellipsis otherwise.
func indexName(x ir.Expr) string {
	switch x := x.(type) {
	case *ir.Ident:
		return x.Name
	case *ir.NumberLit:
		return strconv.FormatFloat(x.Value, 'f', -1, 64)
	}
	return "…"
}

func argsName(args []ir.Expr) string {
	if len(args) == 0 {
		return "()"
	}
	return "(…)"
}

// maybeToString is ToString of a Maybe of a primitive (D8): "undefined",
// "null", or ToString of its value, through the function the element's own
// ToString calls.
func (e *emitter) maybeToString(v string, t ir.Type, at ir.Span) string {
	elem := *t.Elem
	f, ok := directToString[elem]
	if !ok {
		body, ok := primitiveToString(elem, "v")
		if !ok {
			return e.cannot(at, "the backend cannot convert a "+t.String()+" to a string yet")
		}
		f = fmt.Sprintf("func(v %s) string { return %s }", e.typ(elem, at), body)
	}
	return fmt.Sprintf("%s.MaybeToString(%s, %s)", rtPkg, v, f)
}

// directToString are the runtime's ToString functions that take a T as it
// is, so a Maybe's ToString can be given one by name.
var directToString = map[ir.Type]string{
	ir.Number:  rtPkg + ".NumberToString",
	ir.Boolean: rtPkg + ".BoolToString",
	ir.Int64:   rtPkg + ".IntToString",
	ir.Uint64:  rtPkg + ".UintToString",
}

// cloned reports whether a field of type t is copied when its object is
// (D11): an object, an array, or a maybe of either, whose value CloneValue
// copies as it would copy one alone. A primitive is a value, and a function
// is shared, as JavaScript shares it.
func cloned(t ir.Type) bool {
	switch t.Kind {
	case ir.KindObject, ir.KindArray:
		return true
	case ir.KindMaybe:
		return t.Elem != nil && cloned(*t.Elem)
	}
	return false
}

// uncomparable reports whether Go has no == for values of type t: a
// function, or a maybe of one. JavaScript compares functions by identity,
// which a Go func doesn't have.
func uncomparable(t ir.Type) bool {
	if t.Kind == ir.KindMaybe && t.Elem != nil {
		t = *t.Elem
	}
	return t.Kind == ir.KindFunc
}
