package lower

import (
	"slices"
	"strconv"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// fit holds a value to the slot it fills, the contextual type the checker
// gives the expression there: never readonly into mutable (readonlyOK), an
// object exactly (exact), and a function value adapted to its slot's
// signature (adapt). It returns the value to use, or nil once it has refused
// it.
func (l *lowerer) fit(n *ast.Node, v ir.Expr) ir.Expr {
	if !l.readonlyOK(n, v) {
		return nil
	}
	v = l.widened(n, v)
	v = l.wrapped(n, v)
	if v = l.boxed(n, v); v == nil { // errors.go
		return nil
	}
	if t := v.ExprType(); t.Kind == ir.KindFunc {
		if slot := l.slotType(n); slot.Kind == ir.KindFunc && !slot.Equal(t) {
			return l.adapt(n, v, slot)
		}
	}
	if slot := l.slotType(n); slot.Kind == ir.KindArray && holdsUnknown(slot) && !slot.Equal(v.ExprType()) {
		// An array holds values of its element type only, so one of
		// numbers isn't one of unknowns, which TypeScript allows.
		if v.ExprType() == ir.Unknown {
			l.failf(n, "an unknown narrowed to an array, as Array.isArray narrows it, is not supported yet")
		} else {
			l.failf(n, "%s can't be used as %s yet: an array holds values of one type in cakebear, so build one of unknowns instead", aType(v.ExprType()), aType(slot))
		}
		return nil
	}
	if !l.exact(n, v) {
		return nil
	}
	return v
}

// holdsUnknown reports whether t is an unknown, or an array or a maybe of
// one, at any depth.
func holdsUnknown(t ir.Type) bool {
	if t == ir.Unknown {
		return true
	}
	return (t.Kind == ir.KindArray || t.Kind == ir.KindMaybe) && t.Elem != nil && holdsUnknown(*t.Elem)
}

// fitField holds v, the value of a shorthand property, to its field's type
// t, as fit holds an expression to its contextual type: a shorthand has no
// expression of its own for the checker to give one.
func (l *lowerer) fitField(n *ast.Node, v ir.Expr, t ir.Type) ir.Expr {
	if v == nil {
		return nil
	}
	if v = into(v, t); v.ExprType().Equal(t) {
		return v
	}
	switch got := v.ExprType(); {
	case t == ir.Unknown:
		return l.box(n, v) // errors.go
	case got.Kind == ir.KindFunc && t.Kind == ir.KindFunc:
		return l.adapt(n, v, t)
	case got.Kind == ir.KindObject && t.Kind == ir.KindObject:
		l.failf(n, "%s can't be used as %s: an object has exactly the fields of its type in cakebear%s", aType(got), aType(t), extraFields(got.Shape, t.Shape))
	default:
		l.failf(n, "%s can't be used as %s", aType(got), aType(t))
	}
	return nil
}

// slotType is the IR type of the slot n fills, or ir.Invalid where there is
// none, or none the IR can hold.
func (l *lowerer) slotType(n *ast.Node) ir.Type {
	ct := l.contextualType(n)
	if ct == nil {
		return ir.Invalid
	}
	t := l.slotOf(ct)
	if mentionsUnknown(t) && l.libraryTyped(n) { // errors.go
		return ir.Invalid
	}
	return t
}

// contextualType is the checker's type for the slot n fills, or nil.
func (l *lowerer) contextualType(n *ast.Node) *checker.Type {
	return l.checker.GetContextualType(n, checker.ContextFlagsNone)
}

// slotOf maps ct, a slot's type, onto the IR. The checker types the slot of
// what an async function returns as `T | PromiseLike<T>`, the second for a
// promise the function's would adopt, which cakebear refuses (async.go): the
// slot is T's.
func (l *lowerer) slotOf(ct *checker.Type) ir.Type {
	if ct.Flags()&checker.TypeFlagsUnion != 0 {
		members := slices.DeleteFunc(slices.Clone(ct.Types()), func(m *checker.Type) bool {
			sym := m.Symbol()
			return sym != nil && sym.Name() == "PromiseLike" && sym.Flags()&ast.SymbolFlagsInterface != 0 && onlyInBundledLib(sym, "")
		})
		if len(members) < len(ct.Types()) {
			return l.unionOf(members)
		}
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
