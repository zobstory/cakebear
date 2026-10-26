package runtime

import (
	"fmt"
	"math"
)

// unknown at run time (M6 plan D5). An unknown is a Go `any` holding a value
// of a type the subset holds, in one form for each JavaScript value:
//
//	undefined          nil, the zero any, as the zero Maybe is undefined
//	null               Null
//	a number           float64: an extension type is widened first
//	a string, boolean  string, bool
//	an Error           *Error
//	an object, array   the pointer the subset holds it as: an Object, *Array[T]
//
// It never holds a Maybe (BoxMaybe gives the value its state is), a
// function, a promise or a host value: lowering boxes only data (ir.Box).
// So each JavaScript value has one form, and Go's == on two unknowns is
// JavaScript's ===: identity for objects, arrays and errors, NaN unequal to
// itself, -0 equal to 0.
//
// Anything else flows in by assignment, a T into an `any` slot, which Go
// converts; only a maybe needs BoxMaybe. Nothing flows out but by Unbox,
// once TypeScript has narrowed the unknown, which is TypeScript's own rule.

// jsNull is null's type, in an unknown.
type jsNull struct{}

// Null is null, as an unknown holds it.
var Null any = jsNull{}

// BoxMaybe is m as an unknown: undefined, null or its value.
func BoxMaybe[T any](m Maybe[T]) any {
	switch m.state {
	case stateNull:
		return Null
	case stateValue:
		return m.v
	}
	return nil
}

// Unbox is u as the T TypeScript has narrowed it to, by typeof, instanceof
// or a test of its own (ir.Unbox). The narrowing can be unsound, as when a
// callback assigns the variable between the test and the use: then this
// panics, saying what u held, where Node would carry on with it. what names
// the value, as Must's does.
func Unbox[T any](u any, what string) T {
	if v, ok := u.(T); ok {
		return v
	}
	var zero T
	panic(unboxFailure(what, u, zero))
}

// UnboxMaybe is Unbox to `T | null | undefined`, which is what typeof
// "object" narrows an unknown to when T is the object: null and undefined
// are the maybe's own.
func UnboxMaybe[T any](u any, what string) Maybe[T] {
	switch v := u.(type) {
	case nil:
		return Maybe[T]{}
	case jsNull:
		return NullOf[T]()
	case T:
		return Some(v)
	}
	var zero T
	panic(unboxFailure(what, u, zero))
}

func unboxFailure(what string, held, narrowed any) string {
	is, want := describeUnknown(held), describeUnknown(narrowed)
	if is == want {
		want += " of another shape" // two objects, or arrays, of different types
	}
	return fmt.Sprintf("cakebear: %s is %s where TypeScript had narrowed it to %s (Node carries on with it)", what, is, want)
}

// describeUnknown says what kind of value v is, for a message.
func describeUnknown(v any) string {
	switch v.(type) {
	case nil:
		return "undefined"
	case jsNull:
		return "null"
	case float64:
		return "a number"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case *Error:
		return "an Error"
	case array:
		return "an array"
	case Object:
		return "an object"
	}
	return fmt.Sprintf("a %T", v)
}

// TypeTag is what typeof gives, in ir.TypeTag's order.
type TypeTag uint8

const (
	TagUndefined TypeTag = iota
	TagObject            // an object, an array, an Error, or null
	TagBoolean
	TagNumber
	TagString
	TagFunction // never an unknown's: no function is boxed
	TagSymbol
	TagBigint
)

var typeTagNames = [...]string{"undefined", "object", "boolean", "number", "string", "function", "symbol", "bigint"}

// String is the tag as typeof writes it.
func (t TypeTag) String() string { return typeTagNames[t] }

// TypeOf is `typeof u`, as a tag: `typeof u === "number"` is
// `TypeOf(u) == TagNumber`.
func TypeOf(u any) TypeTag {
	switch u.(type) {
	case nil:
		return TagUndefined
	case float64:
		return TagNumber
	case string:
		return TagString
	case bool:
		return TagBoolean
	}
	return TagObject
}

// InstanceOf is `u instanceof K` for an unknown u and a built-in kind K.
func InstanceOf(u any, k ErrorKind) bool {
	e, ok := u.(*Error)
	return ok && e.InstanceOf(k)
}

// IsUndefinedUnknown is `u === undefined`, IsNullUnknown `u === null`, and
// IsEmptyUnknown `u == null`, either.
func IsUndefinedUnknown(u any) bool { return u == nil }
func IsNullUnknown(u any) bool      { return u == Null }
func IsEmptyUnknown(u any) bool     { return u == nil || u == Null }

// TruthyUnknown is ToBoolean of u (M5 plan D7): false for undefined, null,
// false, 0, -0, NaN and "", and true for anything else, an object, an
// array or an Error whatever it holds.
func TruthyUnknown(u any) bool {
	switch x := u.(type) {
	case nil, jsNull:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// UnknownToString is ToString of u, as `String(u)` and a template's `${u}`
// write it: "undefined", "null", a number by Number::toString, an Error by
// its toString (R26), an object as "[object Object]", and an array as its
// join with ",", in which undefined and null are "".
func UnknownToString(u any) string {
	switch u.(type) {
	case nil:
		return "undefined"
	case jsNull:
		return "null"
	}
	return joinString(u, nil)
}
