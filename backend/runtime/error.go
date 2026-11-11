package runtime

import (
	"fmt"
	"slices"
	"strings"
)

// Error values (M6 plan D4): Error and its built-in kinds, as Node 24.18
// prints, converts and serialises them under --stack-trace-limit=0. No
// stack frames are kept, so `err.stack` is its first line alone, `Name:
// message`, every line of the message included (R29), and console.log
// prints an Error in brackets (R25): `[TypeError: t]`. That is an accepted
// difference: Node prints V8's frames.
//
// An Error never changes once made: the IR has no write to one, and its
// stack is made from its name and message when it is read, which are
// fixed, rather than kept, so that reading it is no write (R30 makes the
// two the same text). So one can be shared between domains, and is what a
// clone of it gives (Cloner).
//
// Since S1 (plan D7) the runtime makes Errors with a cause and with own
// properties, fetch's failures: `TypeError: fetch failed`, caused by a
// system error with Node's errno, code, syscall, address and port
// (NewErrorWith). Their properties are copied when made, and a cause is
// the runtime's own Error, so they never change either. A program's own
// `new Error(m, { cause })` stays refused: its cause could be any object.

// ErrorKind is one of the built-in Error constructors, in ir.ErrorKind's
// order, with its names: ErrorType is TypeError. Every one but ErrorBase is
// a direct subclass of Error (R28).
type ErrorKind uint8

const (
	ErrorBase      ErrorKind = iota // Error
	ErrorType                       // TypeError
	ErrorRange                      // RangeError
	ErrorURI                        // URIError
	ErrorSyntax                     // SyntaxError
	ErrorReference                  // ReferenceError
	ErrorEval                       // EvalError
	// ErrorDOMException is no kind the IR has: a DOMException, which the
	// runtime makes for an abort (abort.go). Its name is its own, such as
	// TimeoutError, and instanceof finds it only as an Error.
	ErrorDOMException
)

var errorKindNames = [...]string{"Error", "TypeError", "RangeError", "URIError", "SyntaxError", "ReferenceError", "EvalError", "DOMException"}

// String is the kind's constructor name, which is also an error's name.
func (k ErrorKind) String() string { return errorKindNames[k] }

// Error is a JavaScript Error of one of the built-in kinds.
type Error struct {
	kind    ErrorKind
	message string
	// props are its own enumerable properties, in the order they were made,
	// which JSON.stringify writes and console.log prints in a block after
	// it: a code for one of Node's own errors (R34), and a system error's
	// errno, code, syscall and the rest (S1 plan D7). Each value is a string
	// or a float64.
	props []ErrorProp
	// code, for one of the errors Node's own JavaScript throws, is the code
	// its stack and String name in brackets, `Error [ERR_X]: m`, and "" for
	// the rest. Node keeps the two apart too: NodeError's toString names the
	// key its class was made for, not its code property, and a system error
	// or one Node's C++ throws has a code property its stack doesn't name.
	code string
	// cause is `e.cause`, when hasCause, an unknown as the runtime holds
	// one, so nil is undefined (S1 plan D7). It is no enumerable property:
	// console.log prints it as `[cause]` after the others, and
	// JSON.stringify leaves it out.
	cause    any
	hasCause bool
	// stack, when it isn't "", is the stack text in place of the one made
	// from the name and message: what Node writes for an error it emitted
	// as an event (writeAfterEnd).
	stack string
	// site is where the error was made, which the report of an uncaught
	// one points at in an ES module's top level (R6).
	site *Site
	// name, for a DOMException, is its name, which isn't its kind's.
	name string
}

// ErrorProp is one of an Error's own enumerable properties: its key, and
// its value, a string or a float64. StringProp and NumberProp make one.
type ErrorProp struct {
	Key   string
	Value any
}

// StringProp is a property key whose value is the string v.
func StringProp(key, v string) ErrorProp { return ErrorProp{Key: key, Value: v} }

// NumberProp is a property key whose value is the number v.
func NumberProp(key string, v float64) ErrorProp { return ErrorProp{Key: key, Value: v} }

// ErrorDetails is what NewErrorWith makes an Error with beyond its kind and
// message.
type ErrorDetails struct {
	// Props are its own enumerable properties, in order, as Node's system
	// errors have them: `{ errno: -61, code: 'ECONNREFUSED', … }`.
	Props []ErrorProp
	// Cause is its cause, when HasCause is set: an unknown as the runtime
	// holds one, nil for undefined and Null for null, so that
	// `{ cause: undefined }` is a cause and printed as one. An Error never
	// changes once made, which is what lets domains share one, so a cause
	// must be a value that never changes either: an Error, a primitive or a
	// frozen value. The runtime's causes are its own system Errors.
	Cause    any
	HasCause bool
}

// NewError is `new Error(message)`, `Error(message)`, or the same of
// another kind, made at site. message is "" for `new Error()`: an error
// with no message of its own reads "" from the prototype, and nothing the
// subset does tells the two apart (R26).
func NewError(kind ErrorKind, message string, site *Site) *Error {
	return &Error{kind: kind, message: message, site: site}
}

// NewErrorWith is NewError with own properties, a cause, or both (S1 plan
// D7): fetch's `TypeError: fetch failed` with its cause, and the system
// error that is the cause, `connect ECONNREFUSED 127.0.0.1:18901` with its
// errno, code, syscall, address and port. A property may not be one an
// Error has already (message, name, stack, cause), an integer-like key,
// which JavaScript would order first, or a key twice, and its value must be
// a string or a float64: it panics on any of those, which only the
// runtime's own bug could pass.
func NewErrorWith(kind ErrorKind, message string, d ErrorDetails, site *Site) *Error {
	checkErrorProps(d.Props)
	e := &Error{kind: kind, message: message, cause: d.Cause, hasCause: d.HasCause, site: site}
	if len(d.Props) > 0 {
		e.props = slices.Clone(d.Props)
	}
	return e
}

// newCodedError is one of Node's own errors (R34): its stack and String
// name the code, `RangeError [ERR_SOCKET_BAD_PORT]: …`, and it has the code
// as an own property.
func newCodedError(kind ErrorKind, code, message string, site *Site) *Error {
	return &Error{kind: kind, message: message, props: []ErrorProp{StringProp("code", code)}, code: code, site: site}
}

func checkErrorProps(props []ErrorProp) {
	for i, p := range props {
		switch p.Value.(type) {
		case string, float64:
		default:
			panic(fmt.Sprintf("cakebear: an Error property %q of %T, not a string or a number", p.Key, p.Value))
		}
		if p.Key == "message" || p.Key == "name" || p.Key == "stack" || p.Key == "cause" || isArrayIndexKey(p.Key) {
			panic(fmt.Sprintf("cakebear: an Error property %q, which an Error can't be given here", p.Key))
		}
		for _, q := range props[:i] {
			if q.Key == p.Key {
				panic(fmt.Sprintf("cakebear: an Error property %q twice", p.Key))
			}
		}
	}
}

// isArrayIndexKey reports whether JavaScript orders key before the others:
// an array index, a canonical integer from 0 to 2^32 - 2.
func isArrayIndexKey(key string) bool {
	if key == "" || len(key) > 10 || (key[0] == '0' && key != "0") {
		return false
	}
	n := uint64(0)
	for i := range len(key) {
		if key[i] < '0' || key[i] > '9' {
			return false
		}
		n = n*10 + uint64(key[i]-'0')
	}
	return n <= 1<<32-2
}

// Kind is the constructor e was made by.
func (e *Error) Kind() ErrorKind { return e.kind }

// Message is `e.message`.
func (e *Error) Message() string { return e.message }

// Name is `e.name`, the constructor's name.
func (e *Error) Name() string {
	if e.name != "" {
		return e.name
	}
	return e.kind.String()
}

// Stack is `e.stack`, which TypeScript types `string | undefined`: always
// a string here, its first line and nothing more.
func (e *Error) Stack() Maybe[string] { return Some(e.stackText()) }

// Cause is `e.cause`, an unknown: undefined for an error with none, as for
// one whose cause is undefined.
func (e *Error) Cause() any { return e.cause }

// HasCause reports whether e has a cause, undefined included: whether
// console.log prints a `[cause]`.
func (e *Error) HasCause() bool { return e.hasCause }

// Prop is the own enumerable property key of e, a string or a float64, and
// whether e has one.
func (e *Error) Prop(key string) (any, bool) {
	for _, p := range e.props {
		if p.Key == key {
			return p.Value, true
		}
	}
	return nil, false
}

// InstanceOf is `e instanceof K`: true for Error, and for e's own kind.
func (e *Error) InstanceOf(k ErrorKind) bool { return k == ErrorBase || k == e.kind }

// MaybeInstanceOf is `m instanceof K` for an `Error | null | undefined`:
// false when it is empty, as instanceof is for null and undefined.
func MaybeInstanceOf(m Maybe[*Error], k ErrorKind) bool {
	return m.state == stateValue && m.v.InstanceOf(k)
}

// stackText is the stack V8 makes with no frames: the name, then the code
// in brackets for one of Node's own, then `: ` and the message unless it
// is empty.
func (e *Error) stackText() string {
	if e.stack != "" {
		return e.stack
	}
	head := e.Name()
	if e.code != "" {
		head += " [" + e.code + "]"
	}
	if e.message == "" {
		return head
	}
	return head + ": " + e.message
}

// ErrorToString is `String(e)`, a template's `${e}` and `e.toString()`
// (R26): Error.prototype.toString, `Name: message`, or the name alone when
// the message is empty. Node's own errors override it to name their code,
// as their stack does.
func ErrorToString(e *Error) string {
	if e.code != "" {
		return e.Name() + " [" + e.code + "]: " + e.message
	}
	if e.message == "" {
		return e.Name()
	}
	return e.Name() + ": " + e.message
}

// errorValue is inspect.js's formatError, and formatRaw's part for an
// Error: the stack in brackets, since it has no frames (R25), its lines
// after the first indented to where it is printed. An error with own
// properties or a cause has them in a block after it, the properties in
// their order and then the cause, `[cause]: …`, which formatError pushes
// last whenever it was made: `[Error: x] { code: 'E', [cause]: 1 }`. Past
// the depth limit such an error is its constructor's name alone (R24), and
// each property and the cause are a level deeper than the error.
//
// Only an error with a block is on inspect's stack of what it is printing,
// as in formatRaw, so a cause leading back to its error is a circular
// reference.
func (c *inspector) errorValue(e *Error, recurse int) string {
	if len(e.props) == 0 && !e.hasCause {
		return c.errorBase(e)
	}
	if slices.Contains(c.seen, any(e)) {
		return fmt.Sprintf("[Circular *%d]", c.circularIndex(e))
	}
	base := c.errorBase(e)
	if recurse > inspectDepth {
		return "[" + e.Name() + "]"
	}
	recurse++
	c.seen = append(c.seen, e)
	output := make([]string, 0, len(e.props)+1)
	for _, p := range e.props {
		c.indent += 2
		value := c.value(p.Value, recurse)
		c.indent -= 2
		output = append(output, printedKey(p.Key)+": "+value)
	}
	if e.hasCause {
		c.indent += 2
		value := c.value(e.cause, recurse)
		c.indent -= 2
		output = append(output, "[cause]: "+value)
	}
	c.seen = c.seen[:len(c.seen)-1]
	if i, ok := c.circular[e]; ok {
		base = fmt.Sprintf("<ref *%d> %s", i, base)
	}
	return c.reduceToSingleString(output, base, [2]string{"{", "}"}, false, nil)
}

// errorBase is formatError's result: the stack in brackets, its later
// lines indented to where it is printed.
func (c *inspector) errorBase(e *Error) string {
	base := "[" + e.stackText() + "]"
	if e.kind == ErrorDOMException {
		// inspect's improveStack names the constructor where the name
		// differs: `[DOMException [TimeoutError]: …]` (R16).
		base = "[DOMException [" + e.name + "]" + e.stackText()[len(e.name):] + "]"
	}
	if c.indent != 0 {
		base = strings.ReplaceAll(base, "\n", "\n"+strings.Repeat(" ", c.indent))
	}
	return base
}

// errorJSON is SerializeJSONObject for an Error: its own enumerable
// properties, in their order (R27). message, name, stack and cause aren't
// enumerable, so an error with none of its own is `{}`, wherever it is.
func (e *jsonEncoder) errorJSON(err *Error, via jsonKey) {
	if len(err.props) == 0 {
		e.buf = append(e.buf, "{}"...)
		return
	}
	e.enter(err, via)
	e.buf = append(e.buf, '{')
	for i, p := range err.props {
		e.entry(i)
		e.buf = appendJSONString(e.buf, p.Key)
		e.buf = append(e.buf, ':')
		if e.lines {
			e.buf = append(e.buf, ' ')
		}
		e.value(p.Value, jsonKey{name: p.Key})
	}
	e.leave('}')
}
