package lower

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/scanner"
	"github.com/zobstory/cakebear/ir"
	"github.com/zobstory/cakebear/types"
)

// Arithmetic on cakebear's numeric types: i32, i64, u32, u64 and f32.
//
// TypeScript types `a + b` on two i32s as number, not i32: the sum can
// overflow, and narrowing it back is the program's own `i32(a + b)`. So the
// arithmetic is Go's int32 only there, inside the conversion, where the
// truncation is written down, and an i32 sum, difference or product wraps as
// JavaScript's `(a + b) | 0` does. Anywhere else (a number's slot,
// console.log, a comparison, another conversion) the operands are widened and
// the arithmetic is a number's, as TypeScript types it. It used to be native
// everywhere, so `const x: number = a / b` was 3 for 7 and 2, and the sum of
// two large i32s printed wrapped.

// markNative records which of a conversion's argument's arithmetic is native
// to target, and returns what forgets it again.
//   - Unary minus is, wherever it is, since negating rounds nothing.
//   - For an integer type, every +, - and * below the argument is, since
//     wrapping each step is wrapping the exact result; a / or % only as the
//     argument itself, since a quotient truncated mid-expression is not the
//     number the conversion truncates once.
//   - For f32, only the argument's own operator is: float32 rounds every
//     step, where a number rounds once, at the conversion.
func (l *lowerer) markNative(arg *ast.Node, target ir.Type) (unmark func()) {
	if !target.IsNumeric() || target == ir.Number {
		return func() {}
	}
	var nodes []*ast.Node
	var walk func(n *ast.Node, root bool)
	walk = func(n *ast.Node, root bool) {
		n = ast.SkipParentheses(n)
		switch n.Kind {
		case ast.KindPrefixUnaryExpression:
			if u := n.AsPrefixUnaryExpression(); u.Operator == ast.KindMinusToken {
				nodes = append(nodes, n)
				walk(u.Operand, root)
			}
		case ast.KindBinaryExpression:
			b := n.AsBinaryExpression()
			switch b.OperatorToken.Kind {
			case ast.KindPlusToken, ast.KindMinusToken, ast.KindAsteriskToken:
				if target == ir.Float32 {
					if root {
						nodes = append(nodes, n)
					}
					return
				}
				nodes = append(nodes, n)
				walk(b.Left, false)
				walk(b.Right, false)
			case ast.KindSlashToken, ast.KindPercentToken:
				if root {
					nodes = append(nodes, n)
				}
			}
		}
	}
	walk(arg, true)
	if l.native == nil {
		l.native = make(map[*ast.Node]ir.Type)
	}
	for _, n := range nodes {
		l.native[n] = target
	}
	return func() {
		for _, n := range nodes {
			delete(l.native, n)
		}
	}
}

// numericOperands widens the operands of an arithmetic operator or a
// comparison when one is of an extension type: a number with an i32 is a
// number's arithmetic, which TypeScript allows, and so is an i32 with an i32
// outside the conversion that makes it native (markNative). A comparison of
// two values of one type is exact as it is.
func (l *lowerer) numericOperands(n *ast.Node, op ir.BinaryOp, left, right ir.Expr) (ir.Expr, ir.Expr) {
	lt, rt := left.ExprType(), right.ExprType()
	if !lt.IsNumeric() || !rt.IsNumeric() || (lt == ir.Number && rt == ir.Number) {
		return left, right
	}
	switch op {
	case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod:
		if native, ok := l.native[n]; ok && lt == native && rt == native {
			return left, right
		}
	default:
		if lt == rt {
			return left, right
		}
	}
	return asNumber(left), asNumber(right)
}

// negOperand widens the operand of a unary minus, as numericOperands does,
// unless the negation is native.
func (l *lowerer) negOperand(n *ast.Node, v ir.Expr) ir.Expr {
	if native, ok := l.native[n]; ok && v.ExprType() == native {
		return v
	}
	return asNumber(v)
}

// asNumber is v widened to a number when it has one of the numeric
// extension types.
func asNumber(v ir.Expr) ir.Expr {
	if t := v.ExprType(); !t.IsNumeric() || t == ir.Number {
		return v
	}
	return &ir.Convert{Value: v, Typ: ir.Number, Span: v.ExprSpan()}
}

// unwidened is a conversion's argument without the widening its slot, a
// number's, gave it (fit). A conversion from the value itself, an i64 to an
// i64 or an i32 to a u32, is exact, or wraps as JavaScript's `| 0` and
// `>>> 0` do; through a float64 it lost what a number can't hold past 2^53,
// and an out-of-range value converted as the machine converts a float.
func unwidened(v ir.Expr) ir.Expr {
	if c, ok := v.(*ir.Convert); ok && c.Typ == ir.Number {
		if t := c.Value.ExprType(); t.IsNumeric() && t != ir.Number {
			return c.Value
		}
	}
	return v
}

// exactLiteral refuses a literal argument to an integer conversion that a
// number can't hold exactly. TypeScript reads the literal in
// `i64(9007199254740993)` as a number, 9007199254740992, before the
// conversion sees it, so the program would hold a value it didn't write.
func exactLiteral(arg *ast.Node, target ir.Type, v ir.Expr) string {
	lit, ok := v.(*ir.NumberLit)
	if !ok || !target.IsNumeric() || target == ir.Number || target == ir.Float32 {
		return ""
	}
	text, ok := literalText(arg)
	if !ok {
		return ""
	}
	written, _, err := big.ParseFloat(strings.ReplaceAll(text, "_", ""), 0, 4096, big.ToNearestEven)
	if err != nil || written.Cmp(big.NewFloat(lit.Value)) == 0 {
		return ""
	}
	held := strconv.FormatFloat(lit.Value, 'g', -1, 64)
	if lit.Value == math.Trunc(lit.Value) {
		held = strconv.FormatFloat(lit.Value, 'f', 0, 64)
	}
	return fmt.Sprintf("%s is %s as a number, which is how TypeScript reads it before %s sees it; write a literal a number holds exactly", text, held, target)
}

// literalText is the source text of a numeric literal, negated or not,
// through parentheses: as written, since the node's own text is the number's
// shortest digits, 4611686018427388000 for 4611686018427387904.
func literalText(n *ast.Node) (string, bool) {
	n = ast.SkipParentheses(n)
	sign := ""
	if n.Kind == ast.KindPrefixUnaryExpression && n.AsPrefixUnaryExpression().Operator == ast.KindMinusToken {
		sign = "-"
		n = ast.SkipParentheses(n.AsPrefixUnaryExpression().Operand)
	}
	if n.Kind != ast.KindNumericLiteral {
		return "", false
	}
	return sign + scanner.GetSourceTextOfNodeFromSourceFile(ast.GetSourceFileOfNode(n), n, false), true
}

// extensionType maps a brand name onto its IR type.
func extensionType(brand string) ir.Type {
	switch brand {
	case types.I32:
		return ir.Int32
	case types.I64:
		return ir.Int64
	case types.U32:
		return ir.Uint32
	case types.U64:
		return ir.Uint64
	case types.F32:
		return ir.Float32
	case types.Base64:
		return ir.Base64
	default:
		return ir.Invalid
	}
}
