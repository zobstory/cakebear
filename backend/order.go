package backend

import (
	"fmt"

	"github.com/zobstory/cakebear/ir"
)

// Evaluation order.
//
// JavaScript evaluates an expression's operands left to right, reads
// included: in `a + f()`, a is read before f runs, even when f changes it, so
// with a = 1 and f setting a to 11, the sum is 12. Go orders only the calls,
// receives and sends among an expression's operands. When a plain variable
// read happens relative to them is unspecified, and gc reads after the
// calls, which gave 22.
//
// The fix leans on what Go does order: calls, left to right, each after its
// arguments. In an expression with a call below its root, every read of a
// binding that is ever written (ir.Ident.Stable is false) becomes a call,
// ts_read(a), and so happens where JavaScript would read it. A read with no
// call below the root to race with needs nothing: `g(a)` evaluates a before g
// runs, in either language.
//
// A field read is ordered the same way, always, since any call can write to
// a field through an alias: `p.x + bump()` reads `ts_read(p.x)`
// (emit_object.go).

// readFunc is the generated identity function ordered reads go through. No
// mangled or prefixed name can be ts_read (mangle.go, emit_program.go).
const readFunc = "ts_read"

// full emits x, the whole expression of a statement, ordering its reads if
// they need it.
func (e *emitter) full(x ir.Expr) string { return e.fullAs(x, access{}) }

// fullAs is full for x as the receiver of an access (emit_maybe.go).
func (e *emitter) fullAs(x ir.Expr, a access) string {
	saved := e.order
	e.order = innerCall(x)
	defer func() { e.order = saved }()
	return e.receiver(x, a)
}

// read emits a read of a binding, through readFunc when the expression's
// reads are being ordered and the binding is one code writes to.
func (e *emitter) read(x *ir.Ident) string {
	name := e.top(x.Module, x.Name)
	if !e.order || x.Stable {
		return name
	}
	e.usesRead = true
	return readFunc + "(" + name + ")"
}

// innerCall reports whether x has a call anywhere below its root. A function
// literal's body is not part of evaluating x, and is not looked into.
func innerCall(x ir.Expr) bool {
	for _, c := range operands(x) {
		if isCall(c) || innerCall(c) {
			return true
		}
	}
	return false
}

// isCall reports whether evaluating x runs code that could change a binding:
// a call of any kind, including the runtime's.
//
// A Cond and a Let are not, though each is a call of a function literal in
// Go (emit_cond.go). They run only what is under them, which innerCall looks
// at through their operands. Go orders the literal among the calls around
// it, and a plain read beside it before or after, unspecified, which only
// matters if the literal can change what the read reads: with no call
// under it, it can't, and with one, innerCall has found that call, and the
// read is ordered.
func isCall(x ir.Expr) bool {
	switch x.(type) {
	case *ir.Call, *ir.HostCall, *ir.HostProp, *ir.Await, *ir.NewPromise, *ir.ConsoleLog, *ir.ArrayOp, *ir.JSONStringify, *ir.BuiltinCall:
		return true
	}
	return false
}

// operands are the expressions evaluating x evaluates first. It knows every
// expression type: one it doesn't panics rather than hide the calls under it.
func operands(x ir.Expr) []ir.Expr {
	switch x := x.(type) {
	case nil, *ir.NumberLit, *ir.StringLit, *ir.BoolLit, *ir.NullLit, *ir.UndefinedLit, *ir.Ident, *ir.FuncLit:
		return nil
	case *ir.Binary:
		return []ir.Expr{x.Left, x.Right}
	case *ir.Unary:
		return []ir.Expr{x.Operand}
	case *ir.Convert:
		return []ir.Expr{x.Value}
	case *ir.Call:
		return x.Args
	case *ir.Await:
		return []ir.Expr{x.Value}
	case *ir.NewPromise:
		return []ir.Expr{x.Executor}
	case *ir.ConsoleLog:
		return x.Args
	case *ir.BuiltinCall:
		return x.Args
	case *ir.Concat:
		return x.Parts
	case *ir.Wrap:
		return []ir.Expr{x.Value}
	case *ir.Unwrap:
		return []ir.Expr{x.Value}
	case *ir.Empty:
		return nil
	case *ir.IsEmpty:
		return []ir.Expr{x.Value}
	case *ir.Cond:
		// Only one branch runs, but a call in either may, so both count.
		return []ir.Expr{x.Test, x.Then, x.Else}
	case *ir.Let:
		return []ir.Expr{x.Value, x.Body}
	case *ir.JSONStringify:
		if x.Space == nil {
			return []ir.Expr{x.Value}
		}
		return []ir.Expr{x.Value, x.Space}
	case *ir.HostCall:
		return append([]ir.Expr{x.Recv}, x.Args...)
	case *ir.HostProp:
		return []ir.Expr{x.Recv}
	case *ir.ObjectLit:
		values := make([]ir.Expr, len(x.Fields))
		for i, f := range x.Fields {
			values[i] = f.Value
		}
		return values
	case *ir.FieldGet:
		return []ir.Expr{x.Object}
	case *ir.HasField:
		return []ir.Expr{x.Object}
	case *ir.ArrayLit:
		return x.Elems
	case *ir.IndexGet:
		return []ir.Expr{x.Array, x.Index}
	case *ir.ArrayOp:
		return append([]ir.Expr{x.Array}, x.Args...)
	default:
		panic(fmt.Sprintf("backend: evaluation order has no case for %T", x))
	}
}

// readDecl is readFunc's declaration, written once when anything uses it.
const readDecl = "// ts_read orders a read against the calls around it; see backend/order.go.\n" +
	"func " + readFunc + "[T any](v T) T { return v }\n\n"
