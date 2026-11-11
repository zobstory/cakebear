package runtime

import (
	"math"
	"strconv"
)

// An unknown narrowed to an object or an array (S1 plan D4 (b)): `"k" in u`,
// `u.k`, `Array.isArray(u)`, `u.length` and `u[i]`, which is how a program
// reads what JSON.parse made, wherever the checker narrowed the unknown.
// TypeScript holds a program to the narrowing, but an unknown can hold any
// data value, so each works on all of them, as JavaScript's would.

// asUnknown is a property's or an element's value as an unknown holds it:
// a maybe becomes what its state is (D5).
func asUnknown(v any) any {
	if m, ok := v.(maybe); ok {
		state, value := m.maybeState()
		switch state {
		case stateUndefined:
			return nil
		case stateNull:
			return Null
		}
		return value
	}
	return v
}

// objectPrototype is what every object has from Object.prototype, which
// `in` sees: the methods a parsed object inherits (and an unknown can't
// hold, being functions).
var objectPrototype = map[string]bool{
	"constructor": true, "hasOwnProperty": true, "isPrototypeOf": true, "propertyIsEnumerable": true,
	"toLocaleString": true, "toString": true, "valueOf": true, "__proto__": true,
	"__defineGetter__": true, "__defineSetter__": true, "__lookupGetter__": true, "__lookupSetter__": true,
}

// arrayPrototype is what an array has from Array.prototype, besides
// Object.prototype's.
var arrayPrototype = map[string]bool{
	"length": true, "at": true, "concat": true, "copyWithin": true, "entries": true, "every": true, "fill": true,
	"filter": true, "find": true, "findIndex": true, "findLast": true, "findLastIndex": true, "flat": true,
	"flatMap": true, "forEach": true, "includes": true, "indexOf": true, "join": true, "keys": true,
	"lastIndexOf": true, "map": true, "pop": true, "push": true, "reduce": true, "reduceRight": true,
	"reverse": true, "shift": true, "slice": true, "some": true, "sort": true, "splice": true,
	"toReversed": true, "toSorted": true, "toSpliced": true, "unshift": true, "values": true, "with": true,
}

// errorPrototype is what an Error has from Error.prototype, besides
// Object.prototype's, and its own message and stack.
var errorPrototype = map[string]bool{"name": true, "message": true, "stack": true}

// UnknownIn is `key in u`: own and inherited properties, as JavaScript's
// `in` sees them. Of a value that isn't an object it throws Node's
// TypeError (R7 of M6's measurements, `in` at its operator).
func UnknownIn(key string, u any, site *Site) bool {
	switch x := u.(type) {
	case *JSONObject:
		_, own := x.Get(key)
		return own || objectPrototype[key]
	case *Error:
		_, own := x.Prop(key)
		return own || errorPrototype[key] || key == "cause" && x.HasCause() || objectPrototype[key]
	case Object:
		_, own := objectKey(x, key)
		return own || objectPrototype[key]
	case array:
		i, index := arrayKey(key)
		return index && i < x.inspectLen() || arrayPrototype[key] || objectPrototype[key]
	}
	throwError(ErrorType, "Cannot use 'in' operator to search for '"+key+"' in "+UnknownToString(u), site)
	return false
}

// objectKey is the value of o's key, through its table.
func objectKey(o Object, key string) (any, bool) {
	l := o.Layout()
	for i, k := range l.keys {
		if k == key {
			return o.Field(l.fields[i]), true
		}
	}
	return nil, false
}

// arrayKey reads key as an array index, as JavaScript does: a canonical
// numeric string below 2^32 - 1.
func arrayKey(key string) (int, bool) {
	n, err := strconv.ParseUint(key, 10, 32)
	if err != nil || n == math.MaxUint32 || strconv.FormatUint(n, 10) != key {
		return 0, false
	}
	return int(n), true
}

// UnknownGet is `u.key`: an own property's value, or undefined. Reading one
// of null or undefined throws Node's TypeError, at the property's name.
// What an object inherits is a function, which an unknown can't hold: that
// is a panic, saying so.
func UnknownGet(u any, key string, site *Site) any {
	switch x := u.(type) {
	case nil, jsNull:
		throwError(ErrorType, "Cannot read properties of "+UnknownToString(u)+" (reading '"+key+"')", site)
	case *JSONObject:
		if v, ok := x.Get(key); ok {
			return v
		}
	case *Error:
		switch key {
		case "message":
			return x.Message()
		case "name":
			return x.Name()
		case "stack":
			return x.stackText()
		case "cause":
			return x.Cause()
		}
		if v, ok := x.Prop(key); ok {
			return v
		}
		if objectPrototype[key] {
			inheritedFunction(key)
		}
		return nil
	case Object:
		if v, ok := objectKey(x, key); ok {
			return asUnknown(v)
		}
	case array:
		if key == "length" {
			return float64(x.inspectLen())
		}
		if i, ok := arrayKey(key); ok && i < x.inspectLen() {
			return asUnknown(x.inspectAt(i))
		}
		if arrayPrototype[key] {
			inheritedFunction(key)
		}
		return nil
	case string:
		if key == "length" {
			return StrLength(x)
		}
		return nil
	default:
		return nil // a number's or a boolean's properties are their prototypes', all functions
	}
	if objectPrototype[key] {
		inheritedFunction(key)
	}
	return nil
}

func inheritedFunction(key string) {
	panic("cakebear: " + key + " is a function an object inherits, which an unknown can't hold")
}

// UnknownIsArray is `Array.isArray(u)`.
func UnknownIsArray(u any) bool {
	_, ok := u.(array)
	return ok
}

// UnknownLength is `u.length` of an unknown the checker narrowed to an
// array.
func UnknownLength(u any, site *Site) float64 {
	if a, ok := u.(array); ok {
		return float64(a.inspectLen())
	}
	return unknownNumber(UnknownGet(u, "length", site))
}

func unknownNumber(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return math.NaN()
}

// UnknownIndex is `u[i]` of an unknown the checker narrowed to an array:
// the element, or undefined past the end, before the start or between
// integers, as Node reads it.
func UnknownIndex(u any, i float64, site *Site) any {
	a, ok := u.(array)
	if !ok {
		return UnknownGet(u, NumberToString(i), site)
	}
	if i < 0 || i != math.Trunc(i) || i >= float64(a.inspectLen()) {
		return nil
	}
	return asUnknown(a.inspectAt(int(i)))
}
