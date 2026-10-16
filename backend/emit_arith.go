package backend

import (
	"fmt"
	"strconv"

	"github.com/zobstory/cakebear/ir"
)

// arith emits % on every numeric type, and / on the integer types, through
// the runtime: Go has no % on floats, and its integer division by zero
// panics with Go's message, where cakebear's says what the number would have
// been. ok is false for any other operation.
func (e *emitter) arith(x *ir.Binary) (string, bool) {
	var fn string
	switch {
	case x.Op == ir.OpMod && (x.Typ == ir.Number || x.Typ == ir.Float32):
		fn = "Mod"
	case x.Op == ir.OpMod && integerType(x.Typ):
		fn = "IntRem"
	case x.Op == ir.OpDiv && integerType(x.Typ):
		fn = "IntQuo"
	default:
		return "", false
	}
	left, right := e.expr(x.Left), e.expr(x.Right)
	e.usesRuntime = true
	if fn == "Mod" {
		return fmt.Sprintf("%s.Mod(%s, %s)", rtPkg, left, right), true
	}
	return fmt.Sprintf("%s.%s(%s, %s, %q)", rtPkg, fn, left, right, x.Typ.String()), true
}

// unary emits a unary operator. Negating an integer constant is a run-time
// operation, as arithmetic on two constants is (binary): Go refuses
// `-int32(-2147483648)` as an overflow, which the program means to wrap.
func (e *emitter) unary(op string, x *ir.Unary) string {
	operand := e.expr(x.Operand)
	if x.Op == ir.OpNeg && integerType(x.Typ) && goConstant(x.Operand) {
		operand = e.runtimeValue(operand)
	}
	return fmt.Sprintf("(%s%s)", op, operand)
}

// convert emits a conversion: a plain Go one, whose truncation and wrapping
// are Go's, the machine's, for a value known only at run time. The range
// check in types/ refuses a literal outside the type before it gets here.
// A constant is converted as JavaScript converts the number it is:
//   - a literal to an integer type, from its exact digits: goFloat's are the
//     shortest that read back as the float64, and Go converts a constant
//     exactly, so i64(4611686018427387904), 2^62, held 4611686018427388000;
//   - a literal to f32, by way of a float64, as Math.fround rounds a number,
//     where rounding the decimal straight to float32 can differ in the last
//     bit;
//   - a constant of another extension type to an integer type, at run time:
//     Go refuses a constant conversion that doesn't fit, `uint32(int32(-1))`,
//     which the program means to wrap.
func (e *emitter) convert(x *ir.Convert) string {
	typ := e.typ(x.Typ, x.Span)
	if lit, ok := x.Value.(*ir.NumberLit); ok {
		switch {
		case integerType(x.Typ):
			return fmt.Sprintf("%s(%s)", typ, strconv.FormatFloat(lit.Value, 'f', 0, 64))
		case x.Typ == ir.Float32:
			return fmt.Sprintf("%s(float64(%s))", typ, e.expr(lit))
		}
	}
	value := e.expr(x.Value)
	if integerType(x.Typ) && goConstant(x.Value) && x.Value.ExprType() != ir.Number {
		value = e.runtimeValue(value)
	}
	return fmt.Sprintf("%s(%s)", typ, value)
}

// unfused wraps a floating-point product in a conversion to its own type.
// Go may fuse `a * b + c` into one multiply-add, rounding once, where
// JavaScript rounds the product and then the sum, and on arm64 it does:
// 0.6764556596678251 * 0.4613862177205994 - 0.5085473976760264 printed
// -0.1964400794061956, where Node prints -0.19644007940619557. An explicit
// conversion is what the Go spec says a fusion can't cross. Every product
// since P4 was emitted bare.
func unfused(x *ir.Binary, product string) string {
	if x.Op != ir.OpMul || (x.Typ != ir.Number && x.Typ != ir.Float32) {
		return product
	}
	if x.Typ == ir.Float32 {
		return "float32" + product
	}
	return "float64" + product
}

func integerType(t ir.Type) bool {
	return t == ir.Int32 || t == ir.Int64 || t == ir.Uint32 || t == ir.Uint64
}
