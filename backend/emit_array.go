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

// indexGet is `xs[i]`, which panics outside the array (D8), or, where the
// read may be undefined, is undefined there, as Node's is (M5 plan D9).
func (e *emitter) indexGet(x *ir.IndexGet) string {
	arr := e.receiver(x.Array, elementAccess(x.Index, false))
	if x.OrUndefined {
		return e.orUndefined(x.Array, arr+".Lookup("+e.expr(x.Index)+")")
	}
	return fmt.Sprintf("%s.Get(%s)", arr, e.expr(x.Index))
}

// orUndefined makes read, a runtime call giving an element of array and
// whether there was one, the `T | undefined` of the IR's OrUndefined (D9):
// a Maybe of the element, or for an array of maybes, the element itself,
// whose own undefined is the read's.
//
//	cakebearrt.OrUndefined(xs.Pop())
//	cakebearrt.MaybeOrUndefined(ms.Lookup(i))
func (e *emitter) orUndefined(array ir.Expr, read string) string {
	e.usesRuntime = true
	wrap := "OrUndefined"
	if elem := array.ExprType().Elem; elem != nil && elem.Kind == ir.KindMaybe {
		wrap = "MaybeOrUndefined"
	}
	return rtPkg + "." + wrap + "(" + read + ")"
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
	return fmt.Sprintf("%s.Set(%s, %s)", e.receiver(s.Array, elementAccess(s.Index, true)), e.expr(s.Index), e.expr(s.Value))
}

// arrayOp emits an array member (D16), each a method or a function of the
// runtime's (runtime/array.go, array_methods.go, array_join.go). The array
// is evaluated first and then each argument, in source order, as JavaScript
// evaluates them: they are a Go call's receiver and arguments, or all its
// arguments, and with a call among them full has ordered the reads
// (order.go). A method that takes a callback passes the domain the call
// runs in, which the callback runs in too.
func (e *emitter) arrayOp(x *ir.ArrayOp) string {
	use := reading(x.Method.String())
	if x.Method == ir.ArraySetLength {
		use = writing("length") // its String is "length ="
	}
	arr := e.receiver(x.Array, use)
	args := make([]string, len(x.Args))
	for i, a := range x.Args {
		args[i] = e.expr(a)
	}
	switch x.Method {
	case ir.ArrayLength:
		return arr + ".Length()"
	case ir.ArraySetLength:
		return arr + ".SetLength(" + args[0] + ", " + e.siteArg(x.Span) + ")"
	case ir.ArrayPush:
		return arr + ".Push(" + strings.Join(args, ", ") + ")"
	case ir.ArrayPop, ir.ArrayShift:
		read := arr + "." + removals[x.Method] + "()"
		if x.Typ == ir.Void { // a statement, which drops the element
			return read
		}
		return e.orUndefined(x.Array, read)
	case ir.ArrayAt:
		return e.orUndefined(x.Array, arr+".RelativeAt("+args[0]+")")
	case ir.ArrayFind, ir.ArrayFindLast:
		return e.orUndefined(x.Array, arr+"."+searches[x.Method]+"("+e.dom()+", "+args[0]+")")
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
		// identity in JavaScript; lowering refuses searching for one, or for
		// a maybe of one.
		if elem := *x.Array.ExprType().Elem; uncomparable(elem) {
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
// removals and searches are the members that may give undefined, each a
// method giving an element and whether there was one (orUndefined).
var (
	removals        = map[ir.ArrayMethod]string{ir.ArrayPop: "Pop", ir.ArrayShift: "Shift"}
	searches        = map[ir.ArrayMethod]string{ir.ArrayFind: "Find", ir.ArrayFindLast: "FindLast"}
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
	fmt.Fprintf(&e.buf, "%s\t%s := %s\n", pad, ofVar, e.fullAs(s.Array, iterating))
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
