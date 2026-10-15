package lower

import (
	"strconv"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// fit holds a value to the slot it fills, the contextual type the checker
// gives the expression there: an object exactly (exact), and a function
// value adapted to its slot's signature (adapt). It returns the value to
// use, or nil once it has refused it.
func (l *lowerer) fit(n *ast.Node, v ir.Expr) ir.Expr {
	if t := v.ExprType(); t.Kind == ir.KindFunc {
		if slot := l.slotType(n); slot.Kind == ir.KindFunc && !slot.Equal(t) {
			return l.adapt(n, v, slot)
		}
	}
	if !l.exact(n, v) {
		return nil
	}
	return v
}

// slotType is the IR type of the slot n fills, or ir.Invalid where there is
// none, or none the IR can hold.
func (l *lowerer) slotType(n *ast.Node) ir.Type {
	ct := l.checker.GetContextualType(n, checker.ContextFlagsNone)
	if ct == nil {
		return ir.Invalid
	}
	return l.typeFromType(ct)
}

// adapt fits a function value to a slot of another signature. TypeScript
// lets a function taking fewer parameters stand in for one taking more, as
// `xs.map(double)` does, but a Go func type must match exactly: the value
// becomes a literal of the slot's signature that passes on what it takes,
// and drops a result the slot has no use for. A function literal written in
// place already takes its slot's signature (closureFor); this is for a
// name, since the IR calls a function by name.
func (l *lowerer) adapt(n *ast.Node, v ir.Expr, slot ir.Type) ir.Expr {
	own, want := v.ExprType().Sig, slot.Sig
	id, named := v.(*ir.Ident)
	fits := named && own != nil && want != nil && len(own.Params) <= len(want.Params) &&
		(want.Result == ir.Void || own.Result.Equal(want.Result))
	for i := 0; fits && i < len(own.Params); i++ {
		fits = own.Params[i].Equal(want.Params[i])
	}
	if !fits {
		l.failf(n, "a %s can't be used as a %s yet: write an arrow function in its place that calls it", v.ExprType(), slot)
		return nil
	}
	span := v.ExprSpan()
	call := &ir.Call{Callee: id.Name, Module: id.Module, Typ: own.Result, Span: span}
	lit := &ir.FuncLit{Result: want.Result, Typ: slot, Span: span}
	for i, pt := range want.Params {
		param := ir.Param{Type: pt, Span: span}
		if i < len(own.Params) {
			// A space is in no TypeScript identifier, so no name of the
			// program's can meet this one.
			param.Name = "ts arg" + strconv.Itoa(i)
			call.Args = append(call.Args, &ir.Ident{Name: param.Name, Stable: true, Typ: pt, Span: span})
		}
		lit.Params = append(lit.Params, param)
	}
	if want.Result == ir.Void {
		lit.Body = []ir.Stmt{&ir.ExprStmt{Expr: call, Span: span}}
	} else {
		lit.Body = []ir.Stmt{&ir.Return{Value: call, Span: span}}
	}
	if id.Module == "" {
		lit.Captures = []ir.Capture{{Name: id.Name, Type: id.Typ, Mutable: !id.Stable}}
	}
	return lit
}
