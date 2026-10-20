package ir

import "fmt"

// Truthy is ToBoolean of Value (M5 plan D7): whether JavaScript would take
// it as true in a condition. false, 0, -0, NaN and "" are falsy, and so are
// null and undefined, a maybe's empty states; a maybe holding a value is that
// value's truthiness. An object, an array, a function, a promise or a host
// value is always truthy, though it is still evaluated. Lowering makes one
// wherever a value that isn't a boolean is tested: `if (x)`, `!x`, `x && y`.
type Truthy struct {
	Value Expr
	Span  Span
}

func (*Truthy) isExpr()          {}
func (e *Truthy) ExprType() Type { return Boolean }
func (e *Truthy) ExprSpan() Span { return e.Span }

// checkTruthy holds a Truthy to a value that has one: anything stored, or the
// literals null and undefined, never void.
func checkTruthy(x *Truthy) error {
	switch t := x.Value.ExprType(); {
	case t == Null, t == Undefined:
		return nil
	case t == Void, t == Invalid:
		return fmt.Errorf("the truthiness of a %s", t)
	}
	return nil
}
