package backend

import "github.com/zobstory/cakebear/ir"

// goBinaryOp spells op in Go. ok is false for an operator it doesn't know.
func goBinaryOp(op ir.BinaryOp) (string, bool) {
	switch op {
	case ir.OpAdd:
		return "+", true
	case ir.OpSub:
		return "-", true
	case ir.OpMul:
		return "*", true
	case ir.OpDiv:
		return "/", true
	case ir.OpLess:
		return "<", true
	case ir.OpLessEq:
		return "<=", true
	case ir.OpGreater:
		return ">", true
	case ir.OpGreaterEq:
		return ">=", true
	case ir.OpEqual:
		return "==", true
	case ir.OpNotEqual:
		return "!=", true
	case ir.OpAnd:
		return "&&", true
	case ir.OpOr:
		return "||", true
	default:
		return "", false
	}
}

// goUnaryOp spells op in Go. ok is false for an operator it doesn't know.
func goUnaryOp(op ir.UnaryOp) (string, bool) {
	switch op {
	case ir.OpNeg:
		return "-", true
	case ir.OpNot:
		return "!", true
	}
	return "", false
}
