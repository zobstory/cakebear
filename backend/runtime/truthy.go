package runtime

import "math"

// ToBoolean (M5 plan D7), for the values whose truthiness takes more than
// the comparison the emitter writes in place for a string, `s != ""`, or an
// integer, `n != 0`.

// TruthyNumber is ToBoolean of a number: false for 0, -0 and NaN, and true
// for anything else, the infinities and the smallest subnormal included. -0
// equals 0 in Go as in JavaScript, so only NaN needs a test of its own.
func TruthyNumber(f float64) bool { return f != 0 && !math.IsNaN(f) }

// AlwaysTruthy is ToBoolean of an object, an array, a function, a promise
// or a host value, which is true whatever it holds. It takes the value so
// the expression is still evaluated, once and where JavaScript evaluates
// it, since it may be a call.
func AlwaysTruthy[T any](T) bool { return true }

// Truthy is ToBoolean of m: false when it is null or undefined, and
// otherwise test of its value, test being ToBoolean of a T.
func (m Maybe[T]) Truthy(test func(T) bool) bool { return m.state == stateValue && test(m.v) }
