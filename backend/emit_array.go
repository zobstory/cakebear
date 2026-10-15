package backend

import (
	"fmt"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// Arrays (M4 plan D7–D9, D13, D16): a TypeScript array is a *cakebearrt.Array[T],
// a reference to its elements, and every access goes through a method that
// checks what Node would answer with undefined or a hole (runtime/array.go).
// A method call is a call, so Go orders an element read among the calls of
// its expression without the help ts_read gives a variable's.

// goArrayType spells T[].
func goArrayType(t ir.Type) (string, bool) {
	if t.Elem == nil {
		return "", false
	}
	elem, ok := goType(*t.Elem)
	return "*" + rtPkg + ".Array[" + elem + "]", ok
}

// arrayLit is `[a, b]`. The element type is spelled out, so `[]` builds an
// empty array of the type its slot gave it.
func (e *emitter) arrayLit(x *ir.ArrayLit) string {
	e.usesRuntime = true
	elems := make([]string, len(x.Elems))
	for i, v := range x.Elems {
		elems[i] = e.expr(v)
	}
	return fmt.Sprintf("%s.NewArray[%s](%s)", rtPkg, e.typ(*x.Typ.Elem, x.Span), strings.Join(elems, ", "))
}

func (e *emitter) indexGet(x *ir.IndexGet) string {
	return fmt.Sprintf("%s.Get(%s)", e.expr(x.Array), e.expr(x.Index))
}

// indexSet is `xs[i] = v`. JavaScript evaluates xs, then i, then v; with a
// call in any of them, the reads among them are ordered as an expression's
// are (order.go).
func (e *emitter) indexSet(s *ir.IndexSet) string {
	saved := e.order
	e.order = false
	for _, x := range []ir.Expr{s.Array, s.Index, s.Value} {
		e.order = e.order || isCall(x) || innerCall(x)
	}
	defer func() { e.order = saved }()
	return fmt.Sprintf("%s.Set(%s, %s)", e.expr(s.Array), e.expr(s.Index), e.expr(s.Value))
}

// arrayOp emits an array member (D16), each a method or a function of the
// runtime's (runtime/array.go, array_methods.go, array_join.go). The array
// is evaluated first and then each argument, in source order, as JavaScript
// evaluates them: they are a Go call's receiver and arguments, or all its
// arguments, and with a call among them full has ordered the reads
// (order.go). A method that takes a callback passes the domain the call
// runs in, which the callback runs in too.
func (e *emitter) arrayOp(x *ir.ArrayOp) string {
	arr := e.expr(x.Array)
	args := make([]string, len(x.Args))
	for i, a := range x.Args {
		args[i] = e.expr(a)
	}
	switch x.Method {
	case ir.ArrayLength:
		return arr + ".Length()"
	case ir.ArraySetLength:
		return arr + ".SetLength(" + args[0] + ")"
	case ir.ArrayPush:
		return arr + ".Push(" + strings.Join(args, ", ") + ")"
	case ir.ArrayPop:
		return arr + ".Pop()"
	case ir.ArraySlice:
		return arr + ".Slice(" + strings.Join(args, ", ") + ")"
	case ir.ArrayJoin:
		elem := *x.Array.ExprType().Elem
		if why := unjoinable(elem); why != "" {
			return e.cannot(x.Span, "the backend cannot join an array of "+elem.String()+" yet: "+why)
		}
		if len(args) == 0 {
			args = []string{`","`}
		}
		return arr + ".Join(" + args[0] + ")"
	case ir.ArrayIncludes, ir.ArrayIndexOf:
		// A function value has no == in Go, and nothing to compare but
		// identity in JavaScript; lowering refuses searching for one.
		if elem := *x.Array.ExprType().Elem; elem.Kind == ir.KindFunc {
			return e.cannot(x.Span, "the backend cannot emit "+x.Method.String()+" on an array of "+elem.String()+": Go can't compare functions")
		}
		e.usesRuntime = true
		return rtPkg + "." + arrayFuncs[x.Method] + "(" + arr + ", " + args[0] + ")"
	case ir.ArrayMap, ir.ArrayReduce:
		e.usesRuntime = true
		return rtPkg + "." + arrayFuncs[x.Method] + "(" + strings.Join(append([]string{e.dom(), arr}, args...), ", ") + ")"
	}
	if method, ok := callbackMethods[x.Method]; ok {
		return arr + "." + method + "(" + e.dom() + ", " + args[0] + ")"
	}
	return e.cannot(x.Span, "the backend cannot emit the array method "+x.Method.String()+" yet")
}

// callbackMethods are the runtime's methods for the array members taking a
// callback alone, and arrayFuncs the functions for those that are
// functions: Map and Reduce, whose results have a type parameter of their
// own, and Includes and IndexOf, which need their elements comparable.
var (
	callbackMethods = map[ir.ArrayMethod]string{
		ir.ArrayFilter: "Filter", ir.ArrayForEach: "ForEach", ir.ArraySome: "Some",
		ir.ArrayEvery: "Every", ir.ArrayFindIndex: "FindIndex", ir.ArraySort: "Sort",
	}
	arrayFuncs = map[ir.ArrayMethod]string{
		ir.ArrayMap: "Map", ir.ArrayReduce: "Reduce", ir.ArrayIncludes: "Includes", ir.ArrayIndexOf: "IndexOf",
	}
)

// unjoinable says why join can't convert an element of type t to a string,
// or returns "" if it can (runtime.joinString). A function's string is its
// source text, a promise's and a host value's are their classes', and an
// object with a toString of its own has ToString call it.
func unjoinable(t ir.Type) string {
	switch t.Kind {
	case ir.KindNumber, ir.KindString, ir.KindBoolean, ir.KindInt32, ir.KindInt64,
		ir.KindUint32, ir.KindUint64, ir.KindFloat32, ir.KindBase64:
		return ""
	case ir.KindArray:
		if t.Elem == nil {
			return "it has no element type"
		}
		return unjoinable(*t.Elem)
	case ir.KindObject:
		if t.Shape != nil && t.Shape.Index("toString") >= 0 {
			return "an object with its own toString"
		}
		return ""
	}
	return "a " + t.String() + " has no string the runtime can give"
}

// forOf is `for (const x of xs)`. xs is evaluated once, as JavaScript
// evaluates the iterable once, and the length is read before every
// iteration, as an array's iterator reads it: an element pushed inside the
// loop is visited, so this must never be a range over a snapshot (D13). x
// is declared in the loop body, a binding per iteration as JavaScript's is.
func (e *emitter) forOf(s *ir.ForOf, pad string, depth int) {
	name := mangle(s.Name)
	fmt.Fprintf(&e.buf, "%s{\n", pad)
	fmt.Fprintf(&e.buf, "%s\t%s := %s\n", pad, ofVar, e.full(s.Array))
	fmt.Fprintf(&e.buf, "%s\tfor %s := 0; %s < %s.Len(); %s++ {\n", pad, ofIndex, ofIndex, ofVar, ofIndex)
	fmt.Fprintf(&e.buf, "%s\t\t%s := %s.At(%s)\n", pad, name, ofVar, ofIndex)
	fmt.Fprintf(&e.buf, "%s\t\t_ = %s\n", pad, name)
	e.stmts(s.Body, depth+2)
	fmt.Fprintf(&e.buf, "%s\t}\n%s}\n", pad, pad)
}

// The array and the index of a for…of. mangle gives no user name a ts_
// prefix of its own, so neither can collide with one; an inner loop's
// shadow its outer loop's, which nothing else reads.
const (
	ofVar   = "ts_of"
	ofIndex = "ts_i"
)
