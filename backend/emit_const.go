package backend

import (
	"math"

	"github.com/zobstory/cakebear/ir"
)

// Constant arithmetic (binary in emit.go). Go evaluates an expression of
// constants exactly at compile time; JavaScript rounds each step as a
// double. These say when an operation would be one, and make it a run-time
// one instead.

func arithmetic(op ir.BinaryOp) bool {
	return op == ir.OpAdd || op == ir.OpSub || op == ir.OpMul || op == ir.OpDiv
}

// goConstant reports whether x emits as a Go constant: a number literal
// (except -0, which emits as a variable), its negation, or a conversion of
// one. Arithmetic on two of them is never emitted as a constant (binary),
// so no other expression is one.
func goConstant(x ir.Expr) bool {
	switch x := x.(type) {
	case *ir.NumberLit:
		return !(x.Value == 0 && math.Signbit(x.Value))
	case *ir.Unary:
		return x.Op == ir.OpNeg && goConstant(x.Operand)
	case *ir.Convert:
		return goConstant(x.Value)
	}
	return false
}

// runtimeValue passes v, a constant, through readFunc, the identity
// function, which makes it a run-time value of its own type.
func (e *emitter) runtimeValue(v string) string {
	e.usesRead = true
	return readFunc + "(" + v + ")"
}
