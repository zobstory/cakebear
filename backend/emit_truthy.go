package backend

import (
	"fmt"

	"github.com/zobstory/cakebear/ir"
)

// Truthiness (M5 plan D7): ToBoolean of a value that isn't a boolean, where
// a condition or a `!` tests one. Each kind's rule is a comparison written
// in place where Go has one, and the runtime's where NaN or a maybe's state
// takes more (runtime/truthy.go):
//
//	if (s != "") { … }                            // a string: only "" is falsy
//	if (n != 0) { … }                             // an i32, i64, u32 or u64
//	if cakebearrt.TruthyNumber(f) { … }           // a number: 0, -0 and NaN are falsy
//	if m.Truthy(cakebearrt.TruthyNumber) { … }    // a maybe: empty, or its value's
//	if cakebearrt.AlwaysTruthy(p) { … }           // an object: true, once evaluated
//
// Each evaluates the value once, where JavaScript does, and runs nothing
// else that could change a binding, so to the order pass a Truthy is no
// call (order.go).

// truthy emits ToBoolean of x's value.
func (e *emitter) truthy(x *ir.Truthy) string {
	t := x.Value.ExprType()
	switch {
	case t.Kind == ir.KindMaybe:
		return e.maybeTruthy(x.Value, x.Span)
	case alwaysTruthy(t):
		e.usesRuntime = true
		return rtPkg + ".AlwaysTruthy(" + e.expr(x.Value) + ")"
	case t == ir.Null || t == ir.Undefined:
		// A bare null or undefined is a literal, with nothing to evaluate.
		switch x.Value.(type) {
		case *ir.NullLit, *ir.UndefinedLit:
			return "false"
		}
	}
	if test, ok := e.primitiveTruthy(t, e.expr(x.Value)); ok {
		return test
	}
	return e.cannot(x.Span, "the backend cannot test the truthiness of a "+t.String()+" yet")
}

// alwaysTruthy reports whether every value of type t is truthy: an object,
// an array, a function, a promise or a host value.
func alwaysTruthy(t ir.Type) bool {
	switch t.Kind {
	case ir.KindObject, ir.KindArray, ir.KindFunc, ir.KindPromise, ir.KindHost:
		return true
	}
	return false
}

// primitiveTruthy is ToBoolean of v, a Go expression of type t, written so
// that v is evaluated once.
func (e *emitter) primitiveTruthy(t ir.Type, v string) (string, bool) {
	switch t {
	case ir.Boolean:
		return v, true
	case ir.String, ir.Base64:
		return "(" + v + ` != "")`, true
	case ir.Int32, ir.Int64, ir.Uint32, ir.Uint64:
		return "(" + v + " != 0)", true
	case ir.Number:
		e.usesRuntime = true
		return rtPkg + ".TruthyNumber(" + v + ")", true
	case ir.Float32:
		// Widening keeps NaN and -0, which are all the rule looks at.
		e.usesRuntime = true
		return rtPkg + ".TruthyNumber(float64(" + v + "))", true
	}
	return "", false
}

// maybeTruthy is ToBoolean of v, a maybe: false when it is null or
// undefined, and otherwise its value's, which Truthy is given as a
// function: the runtime's for a number, and a literal of the element's own
// rule for the other primitives, as maybeToString does. A maybe of a value
// that is always truthy is truthy when it holds one.
func (e *emitter) maybeTruthy(v ir.Expr, at ir.Span) string {
	e.usesRuntime = true
	elem := *v.ExprType().Elem
	recv := e.operand(v)
	switch {
	case alwaysTruthy(elem):
		return "!" + recv + ".IsEmpty()"
	case elem == ir.Number:
		return recv + ".Truthy(" + rtPkg + ".TruthyNumber)"
	}
	test, ok := e.primitiveTruthy(elem, "v")
	if !ok {
		return e.cannot(at, "the backend cannot test the truthiness of a "+v.ExprType().String()+" yet")
	}
	return fmt.Sprintf("%s.Truthy(func(v %s) bool { return %s })", recv, e.typ(elem, at), test)
}
