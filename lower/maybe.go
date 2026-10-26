package lower

import (
	"slices"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// null, undefined, and the values that may be either (M5 plan D1-D6, D10,
// D12). A binding, a field or a result of `T | undefined`, `T | null` or both
// is a maybe of T (ir.KindMaybe). A T going into a maybe's slot is wrapped,
// and a use of one the checker has narrowed to T is unwrapped, checked.

// unionType maps a union onto the IR. T with null or undefined, or both, is a
// maybe of T (D2). A union whose other members are one type is that type, so
// `"a" | "b"` is a string and `true | false` a boolean (D10). Anything else is
// refused: unions of objects, or of a number and a string, wait for a later
// milestone (D1).
func (l *lowerer) unionType(t *checker.Type) ir.Type {
	return l.unionOf(t.Types())
}

// unionOf is unionType of a union's members.
func (l *lowerer) unionOf(members []*checker.Type) ir.Type {
	elem, empty := ir.Invalid, false
	for _, m := range members {
		if m.Flags()&(checker.TypeFlagsNull|checker.TypeFlagsUndefined) != 0 {
			empty = true
			continue
		}
		mt := l.typeFromType(m)
		switch {
		case mt == ir.Invalid:
			return ir.Invalid // why is mt's
		case mt == ir.Void:
			l.because("a union with void is not supported yet")
			return ir.Invalid
		case elem == ir.Invalid:
			elem = mt
		case !mt.Equal(elem):
			if mt.Kind == ir.KindObject && elem.Kind == ir.KindObject {
				l.because("unions of object types are not supported yet")
			} else {
				l.because("a union of a " + elem.String() + " and a " + mt.String() + " is not supported yet")
			}
			return ir.Invalid
		}
	}
	switch {
	case elem == ir.Invalid:
		l.because("a type that is only null or undefined is not supported yet")
		return ir.Invalid
	case empty:
		return ir.MaybeOf(elem)
	}
	return elem
}

// storedMaybe is the maybe type a binding n names is stored as, when it is
// one. Its declared type, that is: a use's own type (typeOf) is narrowed by
// the checks before it, as `x` is a string inside `if (x !== undefined)`.
func (l *lowerer) storedMaybe(n *ast.Node) (ir.Type, bool) {
	sym := l.p.resolve(l.valueSymbol(n))
	if sym == nil {
		return ir.Invalid, false
	}
	if t := l.typeFromType(l.checker.GetTypeOfSymbol(sym)); t.Kind == ir.KindMaybe {
		return t, true
	}
	return ir.Invalid, false
}

// narrowed is a read of a maybe, stored, as its use needs it (D4): unwrapped
// where the checker has narrowed it to its element, and as it is otherwise.
// A read of an unknown the checker has narrowed is unboxed, checked, to the
// type it narrowed it to (M6 plan D5).
func narrowed(read ir.Expr, use ir.Type, span ir.Span) ir.Expr {
	t := read.ExprType()
	switch {
	case t.Kind == ir.KindMaybe && use.Equal(*t.Elem):
		return &ir.Unwrap{Value: read, Typ: use, Span: span}
	case t == ir.Unknown && use != ir.Unknown && use != ir.Invalid && use != ir.Void:
		return &ir.Unbox{Value: read, Typ: use, Span: span}
	}
	return read
}

// stored is the type the binding n names is stored as, when that is a maybe
// or an unknown, which a read is narrowed from (narrowed).
func (l *lowerer) stored(n *ast.Node) (ir.Type, bool) {
	if t, ok := l.storedMaybe(n); ok {
		return t, true
	}
	return l.storedUnknown(n)
}

// storedUnknown is ir.Unknown when the binding n names is declared one, as
// storedMaybe is a maybe's type: a use's own type is what the checks before
// it narrowed it to.
func (l *lowerer) storedUnknown(n *ast.Node) (ir.Type, bool) {
	sym := l.p.resolve(l.valueSymbol(n))
	if sym == nil {
		return ir.Invalid, false
	}
	if l.typeFromType(l.checker.GetTypeOfSymbol(sym)) == ir.Unknown {
		return ir.Unknown, true
	}
	return ir.Invalid, false
}

// wrapped holds v to a maybe's slot, when n's is one (D6): null and
// undefined become the slot's empty states, and a T is wrapped, a number's
// extension type widened first, as fit widens one into a number's slot.
func (l *lowerer) wrapped(n *ast.Node, v ir.Expr) ir.Expr {
	if slot := l.slotType(n); slot.Kind == ir.KindMaybe && !l.emptyOutside(n) {
		return intoMaybe(v, slot)
	}
	return v
}

// intoMaybe is v as a value of slot, a maybe, where v is a T, null or
// undefined. Anything else, a maybe already among it, is v unchanged.
func intoMaybe(v ir.Expr, slot ir.Type) ir.Expr {
	if slot.Kind != ir.KindMaybe || slot.Elem == nil {
		return v
	}
	switch vt := v.ExprType(); {
	case vt == ir.Null:
		return &ir.Empty{Null: true, Typ: slot, Span: v.ExprSpan()}
	case vt == ir.Undefined:
		return &ir.Empty{Typ: slot, Span: v.ExprSpan()}
	case vt.Kind == ir.KindMaybe:
		return v
	case *slot.Elem == ir.Number && vt.IsNumeric() && vt != ir.Number:
		v = asNumber(v)
	}
	if vt := v.ExprType(); vt.Equal(*slot.Elem) || *slot.Elem == ir.String && vt == ir.Base64 {
		return &ir.Wrap{Value: v, Typ: slot, Span: v.ExprSpan()}
	}
	return v
}

// emptyOutside reports whether n is an argument to a parameter, of a function
// the program doesn't declare (a library's, node:http's or cakebear's own),
// that its own declaration lets be undefined or null: an optional one, as
// setTimeout's delay, listen's callback and slice's end are, or one such as
// clearTimeout's `number | undefined`. Lowering takes those arguments as the
// types the call needs, so they are no maybe. A parameter that is a maybe
// only once its call is typed, as push's and includes's `T` are for an array
// of maybes, takes its argument wrapped, as any other slot does.
func (l *lowerer) emptyOutside(n *ast.Node) bool {
	p := n.Parent
	for p != nil && p.Kind == ast.KindParenthesizedExpression {
		n, p = p, p.Parent
	}
	if p == nil || (p.Kind != ast.KindCallExpression && p.Kind != ast.KindNewExpression) {
		return false
	}
	i := slices.Index(p.Arguments(), n)
	sig := l.checker.GetResolvedSignature(p)
	if i < 0 || sig == nil || sig.Declaration() == nil || l.p.inProgram(sig.Declaration()) {
		return false
	}
	for sig.Target() != nil {
		sig = sig.Target() // the declaration's own, before T is filled in
	}
	params := sig.Parameters()
	if i >= len(params) {
		if !sig.HasRestParameter() || len(params) == 0 {
			return false
		}
		i = len(params) - 1
	}
	t := l.checker.GetTypeOfSymbol(params[i])
	return admitsUndefined(t) || admitsNull(t)
}

// nullishCompare lowers a comparison with null or undefined (D12):
// `x === undefined`, `x === null`, `x == null`, and their negations, as a
// test of x's state. A value that isn't a maybe is wrapped first, which
// makes the test false, as it is in JavaScript, with x still evaluated. It
// reports whether n was one.
func (l *lowerer) nullishCompare(n *ast.Node, op ast.Kind, left, right ir.Expr) (ir.Expr, bool) {
	loose := op == ast.KindEqualsEqualsToken || op == ast.KindExclamationEqualsToken
	negated := op == ast.KindExclamationEqualsEqualsToken || op == ast.KindExclamationEqualsToken
	if !loose && !negated && op != ast.KindEqualsEqualsEqualsToken {
		return nil, false
	}
	value, empty := left, right.ExprType()
	if lt := left.ExprType(); lt == ir.Null || lt == ir.Undefined {
		value, empty = right, lt
	}
	if empty != ir.Null && empty != ir.Undefined {
		return nil, false
	}
	vt := value.ExprType()
	if vt == ir.Null || vt == ir.Undefined {
		l.failf(n, "comparing null or undefined with null or undefined is not supported yet")
		return nil, true
	}
	if vt.Kind != ir.KindMaybe && vt != ir.Unknown { // an unknown is tested as it is (M6 plan D5)
		value = &ir.Wrap{Value: value, Typ: ir.MaybeOf(vt), Span: value.ExprSpan()}
	}
	var test ir.Expr = &ir.IsEmpty{Value: value, Undefined: loose || empty == ir.Undefined, Null: loose || empty == ir.Null, Span: l.span(n)}
	if negated {
		test = &ir.Unary{Op: ir.OpNot, Operand: test, Typ: ir.Boolean, Span: l.span(n)}
	}
	return test, true
}

// maybeOperands holds the operands of === and !== to one type when one of
// them is a maybe of the other's (D12): `m === 5` compares m with 5 wrapped,
// which is JavaScript's ===, since a maybe compares by state, then value.
func (l *lowerer) maybeOperands(n *ast.Node, op ir.BinaryOp, left, right ir.Expr) (ir.Expr, ir.Expr, bool) {
	if op != ir.OpEqual && op != ir.OpNotEqual {
		return left, right, true
	}
	lt, rt := left.ExprType(), right.ExprType()
	switch {
	case lt.Kind == ir.KindMaybe && rt.Kind != ir.KindMaybe && rt.Equal(*lt.Elem):
		right = &ir.Wrap{Value: right, Typ: lt, Span: right.ExprSpan()}
	case rt.Kind == ir.KindMaybe && lt.Kind != ir.KindMaybe && lt.Equal(*rt.Elem):
		left = &ir.Wrap{Value: left, Typ: rt, Span: left.ExprSpan()}
	}
	if t := left.ExprType(); t.Kind == ir.KindMaybe && (t.Elem.Kind == ir.KindFunc || t.Elem.Kind == ir.KindHost) {
		l.failf(n, "%s on a %s is not supported yet", op, t)
		return nil, nil, false
	}
	return l.unknownOperands(n, op, left, right) // errors.go
}

// undefinedReturns gives every bare `return` in body, and the end of it, the
// value undefined, when the function returns a maybe or an unknown (result,
// its element for an async function): TypeScript lets such a function end,
// or return nothing, where it returns undefined. Nested function literals
// have their own results, and their own returns.
func undefinedReturns(body []ir.Stmt, result ir.Type, span ir.Span) []ir.Stmt {
	if result.Kind != ir.KindMaybe && result != ir.Unknown {
		return body
	}
	undefined := func(span ir.Span) ir.Expr {
		if result == ir.Unknown {
			return &ir.Box{Value: &ir.UndefinedLit{Span: span}, Span: span}
		}
		return &ir.Empty{Typ: result, Span: span}
	}
	var fill func(stmts []ir.Stmt)
	fill = func(stmts []ir.Stmt) {
		for _, s := range stmts {
			switch s := s.(type) {
			case *ir.Return:
				if s.Value == nil {
					s.Value = undefined(s.Span)
				}
			case *ir.If:
				fill(s.Then)
				fill(s.Else)
			case *ir.While:
				fill(s.Body)
			case *ir.For:
				fill(s.Body)
			case *ir.ForOf:
				fill(s.Body)
			case *ir.Try:
				fill(s.Body)
				if s.Catch != nil {
					fill(s.Catch.Body)
				}
				fill(s.Finally)
			}
		}
	}
	fill(body)
	if len(body) > 0 {
		if _, ok := body[len(body)-1].(*ir.Return); ok {
			return body
		}
	}
	return append(body, &ir.Return{Value: undefined(span), Span: span})
}

// returnedType is what a function's returns carry: its result, or for an
// async function the element of its promise.
func returnedType(result ir.Type, async bool) ir.Type {
	if async && result.Kind == ir.KindPromise && result.Elem != nil {
		return *result.Elem
	}
	return result
}

// elemOfMaybe is what a maybe holds, or t when it is none.
func elemOfMaybe(t ir.Type) ir.Type {
	if t.Kind == ir.KindMaybe && t.Elem != nil {
		return *t.Elem
	}
	return t
}

// stringish reports whether t is a string at run time: a string, or
// cakebear's base64, which is one.
func stringish(t ir.Type) bool { return t == ir.String || t == ir.Base64 }

// admitsUndefined reports whether a value of type t may be undefined: the
// one value JSON.stringify gives no string for.
func admitsUndefined(t *checker.Type) bool { return admits(t, checker.TypeFlagsUndefined) }

// admitsNull reports whether a value of type t may be null.
func admitsNull(t *checker.Type) bool { return admits(t, checker.TypeFlagsNull) }

// admits reports whether t, or a member of it, has the flag empty.
func admits(t *checker.Type, empty checker.TypeFlags) bool {
	if t.Flags()&empty != 0 {
		return true
	}
	if t.Flags()&checker.TypeFlagsUnion != 0 {
		for _, m := range t.Types() {
			if m.Flags()&empty != 0 {
				return true
			}
		}
	}
	return false
}
