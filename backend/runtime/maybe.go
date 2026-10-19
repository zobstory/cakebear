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
func (m Maybe[T]) Must(what string) T {
	if m.state != stateValue {
		panic(fmt.Sprintf("cakebear: %s is %s where TypeScript had narrowed it to a value (Node carries on with %s)", what, m.emptyName(), m.emptyName()))
	}
	return m.v
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
