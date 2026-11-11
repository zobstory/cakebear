package runtime

import (
	"strconv"
	"strings"
	"sync"
)

// The checked cast (S1 plan D4 (a), (c)): what an unknown holds, made a
// value of the type a program casts it to, `as T` or by a typed slot of
// JSON.parse's `any`. Node erases a cast and carries on with whatever the
// value is; cakebear checks it as the IR holds T, and throws a TypeError
// naming the path where it doesn't fit, an accepted difference.
//
// The emitter generates a decoder per type a program casts to, which calls
// these. A decoded object keeps the keys its shape doesn't name, in place
// in its key table, so a program that passes the data on prints and
// serialises what Node would.

// DecodePath is where a decoder is, for the message of a value that doesn't
// fit: `$`, then `.key` or `[i]` for each step. A decoder makes one per step
// on its stack and reads it only to fail.
type DecodePath struct {
	Parent *DecodePath
	Key    string
	Index  int
	// Cast is the root's: the type cast to, as the program writes it, and
	// Site where the cast is, which a mismatch is thrown at.
	Cast string
	Site *Site
}

// DecodeRoot is the path of the value cast, to cast, at site.
func DecodeRoot(cast string, site *Site) *DecodePath {
	return &DecodePath{Cast: cast, Site: site, Index: -1}
}

// String is the path, `$.items[3].price`.
func (p *DecodePath) String() string {
	var steps []string
	for at := p; at.Parent != nil; at = at.Parent {
		switch {
		case at.Index >= 0:
			steps = append(steps, "["+strconv.Itoa(at.Index)+"]")
		case isBareKey(at.Key):
			steps = append(steps, "."+at.Key)
		default:
			steps = append(steps, "["+strconv.Quote(at.Key)+"]")
		}
	}
	var b strings.Builder
	b.WriteString("$")
	for i := len(steps) - 1; i >= 0; i-- {
		b.WriteString(steps[i])
	}
	return b.String()
}

func (p *DecodePath) root() *DecodePath {
	for p.Parent != nil {
		p = p.Parent
	}
	return p
}

// describeDecoded is what a value is, in a mismatch's message.
func describeDecoded(v any) string {
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
	return "a " + TypeOf(v).String()
}

// DecodeMismatch throws the TypeError of a value that doesn't fit the cast:
// what it is, where, and what the type wants there.
func DecodeMismatch(v any, at *DecodePath, want string) {
	decodeFail(at, describeDecoded(v), want)
}

func decodeFail(at *DecodePath, is, want string) {
	root := at.root()
	throwError(ErrorType, at.String()+" is "+is+", where the type "+root.Cast+" wants "+want, root.Site)
}

// DecodeMissing throws for a required property the value lacks.
func DecodeMissing(at *DecodePath, key, want string) {
	decodeFail(&DecodePath{Parent: at, Key: key, Index: -1}, "missing", want)
}

// Step is the path one property further on.
func (p *DecodePath) Step(key string) DecodePath { return DecodePath{Parent: p, Key: key, Index: -1} }

// At is the path one element further on.
func (p *DecodePath) At(i int) DecodePath { return DecodePath{Parent: p, Index: i} }

// DecodeNumber, DecodeString and DecodeBoolean are a primitive of their
// type, exactly: a cast converts nothing.
func DecodeNumber(v any, at *DecodePath) float64 {
	f, ok := v.(float64)
	if !ok {
		DecodeMismatch(v, at, "a number")
	}
	return f
}

func DecodeString(v any, at *DecodePath) string {
	s, ok := v.(string)
	if !ok {
		DecodeMismatch(v, at, "a string")
	}
	return s
}

func DecodeBoolean(v any, at *DecodePath) bool {
	b, ok := v.(bool)
	if !ok {
		DecodeMismatch(v, at, "a boolean")
	}
	return b
}

// DecodeError is an Error, as one.
func DecodeError(v any, at *DecodePath) *Error {
	e, ok := v.(*Error)
	if !ok {
		DecodeMismatch(v, at, "an Error")
	}
	return e
}

// DecodeUnknown is any value, as an unknown holds it.
func DecodeUnknown(v any, _ *DecodePath) any { return v }

// DecodeMaybe is a maybe of T: undefined or null as its empty states, and
// anything else decoded as a T.
func DecodeMaybe[T any](v any, at *DecodePath, elem func(any, *DecodePath) T) Maybe[T] {
	switch v.(type) {
	case nil:
		return Maybe[T]{}
	case jsNull:
		return NullOf[T]()
	}
	return Some(elem(v, at))
}

// DecodeArray is an array of T: one that is already, as it is, and
// otherwise a new one of each element decoded.
func DecodeArray[T any](v any, at *DecodePath, elem func(any, *DecodePath) T) *Array[T] {
	if a, ok := v.(*Array[T]); ok {
		return a
	}
	a, ok := v.(array)
	if !ok {
		DecodeMismatch(v, at, "an array")
	}
	n := a.inspectLen()
	items := make([]T, n)
	for i := range n {
		step := at.At(i)
		items[i] = elem(asUnknown(a.inspectAt(i)), &step)
	}
	return NewArray(items...)
}

// DecodeSource is an object's keys and values, as a decoder reads them:
// in the order it lists them, each value as an unknown holds it.
type DecodeSource struct {
	Keys []string
	Vals []any
}

// DecodeObject is v's keys and values, or a mismatch for anything but an
// object.
func DecodeObject(v any, at *DecodePath) DecodeSource {
	switch o := v.(type) {
	case *JSONObject:
		return DecodeSource{Keys: o.layout.keys, Vals: o.vals}
	case *Error:
	case Object:
		l := o.Layout()
		vals := make([]any, len(l.keys))
		for i := range l.keys {
			vals[i] = asUnknown(o.Field(l.fields[i]))
		}
		return DecodeSource{Keys: l.keys, Vals: vals}
	}
	DecodeMismatch(v, at, "an object")
	return DecodeSource{}
}

// DecodeLayouts holds a shape's key tables for its decoded objects, one per
// key order, so a response of many records makes one.
type DecodeLayouts struct{ m sync.Map }

// NewDecodeLayouts is a shape's, which the emitter declares once.
func NewDecodeLayouts() *DecodeLayouts { return &DecodeLayouts{} }

// DecodeLayout is the key table of a decoded object whose keys are keys:
// each a field of its shape, field(key) of fields in all, or, for a key the
// shape doesn't name, the next of the object's extra values after them.
func DecodeLayout(cache *DecodeLayouts, keys []string, fields int, field func(string) int) *Layout {
	id := strconv.Itoa(len(keys)) + "\x00" + strings.Join(keys, "\x00")
	if l, ok := cache.m.Load(id); ok {
		return l.(*Layout)
	}
	indices := make([]int, len(keys))
	extra := fields
	for i, k := range keys {
		if f := field(k); f >= 0 {
			indices[i] = f
		} else {
			indices[i] = extra
			extra++
		}
	}
	l, _ := cache.m.LoadOrStore(id, NewLayout(keys, indices))
	return l.(*Layout)
}
