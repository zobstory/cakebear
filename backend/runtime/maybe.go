package runtime

import "fmt"

// Maybe is `T | null | undefined` (M5 plan D2): a T, null or undefined.
// Its zero value is undefined, so a field or a variable nothing has set
// reads as undefined, as in JavaScript.
//
// A Maybe that is null or undefined holds T's zero value, and nothing else
// ever, so Go's == on two Maybes is JavaScript's ===: the states, then the
// values, NaN unequal to itself and -0 equal to 0, as for a T.
type Maybe[T any] struct {
	v     T
	state maybeState
}

type maybeState uint8

const (
	stateUndefined maybeState = iota // the zero value
	stateNull
	stateValue
)

// Some is v, as a Maybe.
func Some[T any](v T) Maybe[T] { return Maybe[T]{v: v, state: stateValue} }

// NullOf is null, as a Maybe of T. Undefined is the zero Maybe.
func NullOf[T any]() Maybe[T] { return Maybe[T]{state: stateNull} }

// IsUndefined is `m === undefined`.
func (m Maybe[T]) IsUndefined() bool { return m.state == stateUndefined }

// IsNull is `m === null`.
func (m Maybe[T]) IsNull() bool { return m.state == stateNull }

// IsEmpty is `m == null`: null or undefined.
func (m Maybe[T]) IsEmpty() bool { return m.state != stateValue }

// Must is m as the T TypeScript has narrowed it to (D4). TypeScript's
// narrowing can be unsound, as when a callback empties a property between
// the check and the use, and where Node would carry on with undefined, this
// panics, saying so. what names the value, for the message.
//
// Assert is the same for a program's own `m!`, which can be wrong too. The
// Read, Write and Iterate forms of each are for an m the program reads a
// property of, writes one of, or iterates, where Node throws rather than
// carry on, so their messages say so. key names the property as Node's
// TypeError does, quoted, 'x' or '0', or as "an index" for one the program
// computes, which cakebear fails before evaluating.
func (m Maybe[T]) Must(what string) T             { return m.unwrap(what, false, useValue, "") }
func (m Maybe[T]) Assert(what string) T           { return m.unwrap(what, true, useValue, "") }
func (m Maybe[T]) MustRead(what, key string) T    { return m.unwrap(what, false, useRead, key) }
func (m Maybe[T]) AssertRead(what, key string) T  { return m.unwrap(what, true, useRead, key) }
func (m Maybe[T]) MustWrite(what, key string) T   { return m.unwrap(what, false, useWrite, key) }
func (m Maybe[T]) AssertWrite(what, key string) T { return m.unwrap(what, true, useWrite, key) }
func (m Maybe[T]) MustIterate(what string) T      { return m.unwrap(what, false, useIterate, "") }
func (m Maybe[T]) AssertIterate(what string) T    { return m.unwrap(what, true, useIterate, "") }

// unwrapUse is what the program does with an unwrapped value, which decides
// what Node does with an empty one.
type unwrapUse uint8

const (
	useValue   unwrapUse = iota // uses it: Node carries on with it
	useRead                     // reads a property of it: Node throws
	useWrite                    // writes a property of it: Node throws
	useIterate                  // iterates it, with for…of: Node throws
)

func (m Maybe[T]) unwrap(what string, asserted bool, use unwrapUse, key string) T {
	if m.state != stateValue {
		panic(unwrapFailure(what, m.emptyName(), asserted, use, key))
	}
	return m.v
}

// unwrapFailure says what was empty, why the program thought it wasn't, and
// what Node does instead: carry on, or throw a TypeError, whose message is
// Node 24.18's, verbatim when key is the property's own.
func unwrapFailure(what, empty string, asserted bool, use unwrapUse, key string) string {
	why := " where TypeScript had narrowed it to a value"
	if asserted {
		why = ", though a ! asserted it isn't"
	}
	node := "carries on with " + empty
	switch use {
	case useRead:
		node = fmt.Sprintf("throws TypeError: Cannot read properties of %s (reading %s)", empty, key)
	case useWrite:
		node = fmt.Sprintf("throws TypeError: Cannot set properties of %s (setting %s)", empty, key)
	case useIterate:
		node = "throws TypeError: " + what + " is not iterable"
	}
	return fmt.Sprintf("cakebear: %s is %s%s (Node %s)", what, empty, why, node)
}

func (m Maybe[T]) emptyName() string {
	if m.state == stateNull {
		return "null"
	}
	return "undefined"
}

// MaybeToString is ToString of m (D8): "undefined", "null", or str of its
// value, as a template literal or String() writes it.
func MaybeToString[T any](m Maybe[T], str func(T) string) string {
	if m.state != stateValue {
		return m.emptyName()
	}
	return str(m.v)
}

// maybe is what inspect, JSON.stringify and the cloner need of a Maybe[T],
// whatever T is.
type maybe interface {
	// maybeState is m's state, and its value when it has one.
	maybeState() (state maybeState, value any)
	// cloneMaybe copies m for a Cloner: its value, when it is an object or an
	// array, the way a T alone would be (D11).
	cloneMaybe(c *Cloner) any
}

func (m Maybe[T]) maybeState() (maybeState, any) {
	if m.state != stateValue {
		return m.state, nil
	}
	return m.state, m.v
}

func (m Maybe[T]) cloneMaybe(c *Cloner) any {
	if m.state != stateValue {
		return m
	}
	return Some(CloneValue(c, m.v))
}

// unwrapForLog is a Maybe as util.format sees it at the top level, where a
// string is written as it is: the value it holds, or the Maybe itself when
// it is empty, which inspect writes as undefined or null.
func unwrapForLog(v any) any {
	if m, ok := v.(maybe); ok {
		if state, value := m.maybeState(); state == stateValue {
			return value
		}
	}
	return v
}
