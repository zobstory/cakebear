package runtime

import "strings"

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
)

var errorKindNames = [...]string{"Error", "TypeError", "RangeError", "URIError", "SyntaxError", "ReferenceError", "EvalError"}

// String is the kind's constructor name, which is also an error's name.
func (k ErrorKind) String() string { return errorKindNames[k] }

// Error is a JavaScript Error of one of the built-in kinds.
type Error struct {
	kind    ErrorKind
	message string
	// code is Node's code, for one of the errors Node's own modules throw
	// (R34), and "" for the rest. It is the one own enumerable property an
	// Error has here, so the one JSON.stringify writes and console.log
	// prints in a block after it.
	code string
	// stack, when it isn't "", is the stack text in place of the one made
	// from the name and message: what Node writes for an error it emitted
	// as an event (writeAfterEnd).
	stack string
	// site is where the error was made, which the report of an uncaught
	// one points at in an ES module's top level (R6).
	site *Site
}

// NewError is `new Error(message)`, `Error(message)`, or the same of
// another kind, made at site. message is "" for `new Error()`: an error
// with no message of its own reads "" from the prototype, and nothing the
// subset does tells the two apart (R26).
func NewError(kind ErrorKind, message string, site *Site) *Error {
	return &Error{kind: kind, message: message, site: site}
}

// newCodedError is one of Node's own errors (R34): its stack and String
// name the code, `RangeError [ERR_SOCKET_BAD_PORT]: …`, and it has the code
// as an own property.
func newCodedError(kind ErrorKind, code, message string, site *Site) *Error {
	return &Error{kind: kind, message: message, code: code, site: site}
}

// Kind is the constructor e was made by.
func (e *Error) Kind() ErrorKind { return e.kind }

// Message is `e.message`.
func (e *Error) Message() string { return e.message }

// Name is `e.name`, the constructor's name.
func (e *Error) Name() string { return e.kind.String() }

// Stack is `e.stack`, which TypeScript types `string | undefined`: always
// a string here, its first line and nothing more.
func (e *Error) Stack() Maybe[string] { return Some(e.stackText()) }

// Cause is `e.cause`, an unknown: undefined, since nothing in M6 makes an
// error with a cause.
func (e *Error) Cause() any { return nil }

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
// after the first indented to where it is printed; then, for an error with
// a code, a block of that property, `[Error: x] { code: 'E' }`, which past
// the depth limit is the constructor's name alone (R24).
func (c *inspector) errorValue(e *Error, recurse int) string {
	base := "[" + e.stackText() + "]"
	if c.indent != 0 {
		base = strings.ReplaceAll(base, "\n", "\n"+strings.Repeat(" ", c.indent))
	}
	if e.code == "" {
		return base
	}
	if recurse > inspectDepth {
		return "[" + e.Name() + "]"
	}
	c.indent += 2
	code := "code: " + c.str(e.code)
	c.indent -= 2
	return c.reduceToSingleString([]string{code}, base, [2]string{"{", "}"}, false, nil)
}

// errorJSON is SerializeJSONObject for an Error: its own enumerable
// properties, which are its code or nothing (R27). message, name and stack
// aren't enumerable, so an error is `{}`, wherever it is.
func (e *jsonEncoder) errorJSON(err *Error, via jsonKey) {
	if err.code == "" {
		e.buf = append(e.buf, "{}"...)
		return
	}
	e.enter(err, via)
	e.buf = append(e.buf, '{')
	e.entry(0)
	e.buf = appendJSONString(e.buf, "code")
	e.buf = append(e.buf, ':')
	if e.lines {
		e.buf = append(e.buf, ' ')
	}
	e.buf = appendJSONString(e.buf, err.code)
	e.leave('}')
}
