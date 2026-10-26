package runtime

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// JSON.stringify (M4 plan D15), for what the subset builds: numbers,
// strings, booleans, and the objects and arrays made of them. It follows
// SerializeJSONProperty in ECMA-262, and V8's message for a cycle, as Node
// 24.18 has them. Lowering refuses a replacer, and a value holding a
// function, a promise or a host value, so none of those reaches here. A
// Maybe's null is null, and its undefined is left out of an object, and
// null in an array, as SerializeJSONProperty gives them (M5 plan D8), and
// so is an unknown's. An Error is its own enumerable properties (M6 plan
// D4): `{}`, or its code for one of Node's own (errorJSON).

// JSONStringify is JSON.stringify(v, null, space), given the gap
// JSONGapNumber or JSONGapString made of space, or JSONGap{} for none.
//
// An object's keys are in the order its literal wrote them, as inspect
// prints them (R6). A value reached twice is written twice, and one that
// leads back to itself throws the TypeError Node throws (see
// circularMessage), made at site, the call's (M6 plan D9).
func JSONStringify(v any, gap JSONGap, site *Site) string {
	e := jsonEncoder{JSONGap: gap, site: site}
	e.value(v, jsonKey{})
	return string(e.buf)
}

// JSONGap is how JSON.stringify lays its output out, as its space argument
// says. The zero JSONGap is the one for no space: everything on one line.
//
// Laying entries out a line each, and indenting them, are two things, not
// one: V8, unlike the spec, gives a number space from 0 to 1 a gap of no
// spaces that still breaks lines, so `JSON.stringify({a: 1}, null, 0.5)`
// is "{\n\"a\": 1\n}" under Node.
type JSONGap struct {
	indent string // what each level of nesting is indented by
	lines  bool   // whether each entry has a line of its own
}

// JSONGapNumber is the gap for a number space, as V8 makes it: the space,
// at most 10, truncated to that many spaces. A space of 0 or less, or NaN,
// gives none.
func JSONGapNumber(space float64) JSONGap {
	n := math.Min(10, space)
	if !(n > 0) { // NaN fails every comparison
		return JSONGap{}
	}
	return JSONGap{indent: strings.Repeat(" ", int(n)), lines: true}
}

// jsonEncoder is the state of one JSON.stringify.
type jsonEncoder struct {
	JSONGap
	buf   []byte
	stack []jsonFrame // the objects and arrays being written, outermost first
	site  *Site       // the call's, for the error a cycle throws
}

// jsonFrame is an object or an array being written, and the key it was
// reached by, which a cycle's message names.
type jsonFrame struct {
	container any
	via       jsonKey
}

// jsonKey is how a value was reached: by an object's property, or by an
// array's index. The top-level value's is the property "", the key the spec
// gives it in its wrapper object.
type jsonKey struct {
	name    string
	index   int
	isIndex bool
}

// jsonArray is an *Array[T], whatever T is.
type jsonArray interface {
	appendJSON(e *jsonEncoder, via jsonKey)
}

// value is SerializeJSONProperty for a value of the subset. The extension
// types are written as the integers they are, as console.log prints them.
func (e *jsonEncoder) value(v any, via jsonKey) {
	switch x := v.(type) {
	case float64:
		e.number(x)
	case float32:
		e.number(float64(x))
	case int32:
		e.buf = strconv.AppendInt(e.buf, int64(x), 10)
	case int64:
		e.buf = strconv.AppendInt(e.buf, x, 10)
	case uint32:
		e.buf = strconv.AppendUint(e.buf, uint64(x), 10)
	case uint64:
		e.buf = strconv.AppendUint(e.buf, x, 10)
	case bool:
		e.buf = append(e.buf, BoolToString(x)...)
	case string:
		e.buf = appendJSONString(e.buf, x)
	case jsNull:
		e.buf = append(e.buf, "null"...)
	case nil:
		// An unknown's undefined, as a Maybe's below: an object leaves it
		// out, an array writes null, and lowering refuses a bare unknown,
		// whose JSON.stringify may give undefined.
		if !via.isIndex {
			panic("cakebear: JSON.stringify of undefined, which returns undefined rather than a string")
		}
		e.buf = append(e.buf, "null"...)
	case *Error:
		e.errorJSON(x, via)
	case Object:
		e.object(x, via)
	case jsonArray:
		x.appendJSON(e, via)
	case maybe:
		switch state, value := x.maybeState(); state {
		case stateValue:
			e.value(value, via)
		case stateNull:
			e.buf = append(e.buf, "null"...)
		default:
			// An object leaves an undefined property out (object), and an
			// array writes null for one. Lowering refuses JSON.stringify of
			// a value that may itself be undefined, since that returns
			// undefined, not a string.
			if !via.isIndex {
				panic("cakebear: JSON.stringify of undefined, which returns undefined rather than a string")
			}
			e.buf = append(e.buf, "null"...)
		}
	default:
		panic(fmt.Sprintf("cakebear: JSON.stringify has no way to write a %T", v))
	}
}

// number is a finite number as Number::toString writes it, which makes -0
// "0", and NaN and the infinities null.
func (e *jsonEncoder) number(f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		e.buf = append(e.buf, "null"...)
		return
	}
	e.buf = append(e.buf, NumberToString(f)...)
}

// object is SerializeJSONObject: each key quoted as a string is, then `:`,
// and a space after it when entries have lines of their own.
func (e *jsonEncoder) object(o Object, via jsonKey) {
	layout := o.Layout()
	if len(layout.keys) == 0 {
		e.buf = append(e.buf, "{}"...)
		return
	}
	e.enter(o, via)
	e.buf = append(e.buf, '{')
	written := 0
	for i, key := range layout.keys {
		value := o.Field(layout.fields[i])
		if isUndefined(value) {
			continue // SerializeJSONProperty gives undefined: no entry at all
		}
		e.entry(written)
		written++
		e.buf = appendJSONString(e.buf, key)
		e.buf = append(e.buf, ':')
		if e.lines {
			e.buf = append(e.buf, ' ')
		}
		e.value(value, jsonKey{name: key})
	}
	if written == 0 {
		// Every property was undefined: "{}", with no line inside, gap or not.
		e.stack = e.stack[:len(e.stack)-1]
		e.buf = append(e.buf, '}')
		return
	}
	e.leave('}')
}

// isUndefined reports whether v is undefined: a Maybe that is, or an
// unknown's.
func isUndefined(v any) bool {
	if v == nil {
		return true
	}
	m, ok := v.(maybe)
	if !ok {
		return false
	}
	state, _ := m.maybeState()
	return state == stateUndefined
}

// appendJSON is SerializeJSONArray.
func (a *Array[T]) appendJSON(e *jsonEncoder, via jsonKey) {
	if len(a.items) == 0 {
		e.buf = append(e.buf, "[]"...)
		return
	}
	e.enter(a, via)
	e.buf = append(e.buf, '[')
	// The common element types skip the conversion to any, which would
	// allocate for each number and string.
	switch items := any(a.items).(type) {
	case []float64:
		for i, f := range items {
			e.entry(i)
			e.number(f)
		}
	case []string:
		for i, s := range items {
			e.entry(i)
			e.buf = appendJSONString(e.buf, s)
		}
	default:
		for i, v := range a.items {
			e.entry(i)
			e.value(v, jsonKey{index: i, isIndex: true})
		}
	}
	e.leave(']')
}

// enter puts a container on the stack, and throws if it is on it already:
// a value that contains itself has no JSON.
func (e *jsonEncoder) enter(container any, via jsonKey) {
	for i, f := range e.stack {
		if f.container == container {
			throwError(ErrorType, circularMessage(e.stack[i:], via), e.site)
		}
	}
	e.stack = append(e.stack, jsonFrame{container: container, via: via})
}

// entry starts a container's i'th entry: after a comma unless it is the
// first, and on a line of its own if the gap gives it one, indented once
// per level of nesting.
func (e *jsonEncoder) entry(i int) {
	if i > 0 {
		e.buf = append(e.buf, ',')
	}
	e.newline()
}

// leave takes the container off the stack and closes it, on a line of its
// own if the gap gives it one, at the level it opened on.
func (e *jsonEncoder) leave(closing byte) {
	e.stack = e.stack[:len(e.stack)-1]
	e.newline()
	e.buf = append(e.buf, closing)
}

func (e *jsonEncoder) newline() {
	if !e.lines {
		return
	}
	e.buf = append(e.buf, '\n')
	for range len(e.stack) {
		e.buf = append(e.buf, e.indent...)
	}
}

// circularMessage is the message of the TypeError V8 throws for a cycle:
// the container it starts at, the steps back to it, and the key that closes
// it. Of the steps it lists the first two and the last, with a `...` line
// for any in between. path is the stack from the container reached again.
func circularMessage(path []jsonFrame, closing jsonKey) string {
	var b strings.Builder
	b.WriteString("Converting circular structure to JSON\n    --> starting at object with constructor ")
	b.WriteString(constructorName(path[0].container))
	steps := path[1:]
	first := min(len(steps), 2)
	for _, f := range steps[:first] {
		writeStep(&b, f)
	}
	if len(steps) > first+1 {
		b.WriteString("\n    |     ...")
	}
	for _, f := range steps[max(first, len(steps)-1):] {
		writeStep(&b, f)
	}
	b.WriteString("\n    --- ")
	b.WriteString(closing.describe())
	b.WriteString(" closes the circle")
	return b.String()
}

func writeStep(b *strings.Builder, f jsonFrame) {
	b.WriteString("\n    |     ")
	b.WriteString(f.via.describe())
	b.WriteString(" -> object with constructor ")
	b.WriteString(constructorName(f.container))
}

// describe is a key as V8's message names it: the property's name as it
// is, unescaped, or <anonymous> for "".
func (k jsonKey) describe() string {
	switch {
	case k.isIndex:
		return "index " + strconv.Itoa(k.index)
	case k.name == "":
		return "<anonymous>"
	}
	return "property '" + k.name + "'"
}

func constructorName(container any) string {
	if _, ok := container.(Object); ok {
		return "'Object'"
	}
	return "'Array'"
}
