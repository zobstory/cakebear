package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// The array methods (M4 plan D16), recognised by declaration as every
// array member is (arrayMember). Each callback takes the signature its slot
// has in full, (value, index, array) => U, whatever the literal declares;
// TypeScript lets it declare fewer, and closureFor or adapt makes up the
// rest.

// arrayMethods are the members lowered as calls, besides push and pop.
var arrayMethods = map[string]ir.ArrayMethod{
	"map": ir.ArrayMap, "filter": ir.ArrayFilter, "join": ir.ArrayJoin, "includes": ir.ArrayIncludes,
	"indexOf": ir.ArrayIndexOf, "slice": ir.ArraySlice, "sort": ir.ArraySort, "some": ir.ArraySome,
	"every": ir.ArrayEvery, "forEach": ir.ArrayForEach, "reduce": ir.ArrayReduce, "findIndex": ir.ArrayFindIndex,
	"find": ir.ArrayFind, "findLast": ir.ArrayFindLast, "at": ir.ArrayAt,
}

// refusedArrayMembers names every other member of Array and ReadonlyArray
// TypeScript's libs declare, with the reason a refusal gives, or "" for the
// plain one. TestArrayMembersAreAccountedFor holds this table and
// arrayMethods to the libs, so a member a new lib adds is noticed.
var refusedArrayMembers = map[string]string{
	"findLastIndex": "", "unshift": "", "splice": "", "concat": "", "reverse": "", "flat": "", "flatMap": "",
	"fill": "", "copyWithin": "", "lastIndexOf": "", "reduceRight": "", "entries": "", "keys": "",
	"values": "", "toString": "", "toLocaleString": "", "toReversed": "", "toSorted": "", "toSpliced": "",
	"with": "",
}

// arrayMethod lowers a call of one of arrayMethods: the array, then its
// arguments in source order, each checked against what the method takes.
func (l *lowerer) arrayMethod(n *ast.Node, c *ast.CallExpression, access *ast.PropertyAccessExpression, m ir.ArrayMethod) ir.Expr {
	args := c.Arguments.Nodes
	for _, a := range args {
		if a.Kind == ast.KindSpreadElement {
			l.failf(a, "spreading arguments into %s is not supported yet", m)
			return nil
		}
	}
	arr := l.expr(access.Expression)
	if arr == nil {
		return nil
	}
	switch m {
	case ir.ArrayMap, ir.ArrayFilter, ir.ArraySome, ir.ArrayEvery, ir.ArrayFindIndex, ir.ArrayForEach, ir.ArrayFind, ir.ArrayFindLast:
		if len(args) == 0 {
			l.invalidf(n, "%s needs a callback", m)
			return nil
		}
		if len(args) > 1 {
			l.failf(args[1], "%s's thisArg is not supported", m)
			return nil
		}
	}
	t := arr.ExprType()
	elem := *t.Elem
	visitor := func(result ir.Type) ir.Type { return ir.FuncType([]ir.Type{elem, ir.Number, t}, result) }
	op := &ir.ArrayOp{Method: m, Array: arr, Span: l.span(n)}

	switch m {
	case ir.ArrayMap:
		result := l.callbackResult(args[0])
		if !storable(result) {
			l.failf(args[0], "map's callback must return a value; forEach is for its effects")
			return nil
		}
		op.Typ = ir.ArrayOf(result)
		return l.withCallback(op, args[0], visitor(result), false)
	case ir.ArrayFilter, ir.ArraySome, ir.ArrayEvery, ir.ArrayFindIndex, ir.ArrayFind, ir.ArrayFindLast:
		if r := l.callbackResult(args[0]); r != ir.Boolean && !l.libGlobal(args[0], "Boolean") {
			l.failf(args[0], "%s's callback must return a boolean; testing a %s for truthiness is not supported yet: return !!x, or pass Boolean", m, r)
			return nil
		}
		op.Typ = map[ir.ArrayMethod]ir.Type{ir.ArrayFilter: t, ir.ArraySome: ir.Boolean, ir.ArrayEvery: ir.Boolean, ir.ArrayFindIndex: ir.Number,
			ir.ArrayFind: ir.OrUndefined(elem), ir.ArrayFindLast: ir.OrUndefined(elem)}[m]
		if rt := l.typeOf(n); rt != ir.Invalid && !rt.Equal(op.Typ) {
			// A callback such as (x) => x !== undefined is a type predicate.
			l.failf(n, "%s narrowing a %s to a %s is not supported yet", m, t, rt)
			return nil
		}
		return l.withCallback(op, args[0], visitor(ir.Boolean), false)
	case ir.ArrayForEach:
		op.Typ = ir.Void
		return l.withCallback(op, args[0], visitor(ir.Void), true)
	case ir.ArraySort:
		if len(args) == 0 {
			l.failf(n, "sort without a comparator compares as strings, which cakebear doesn't do yet; pass one, such as (a, b) => a - b")
			return nil
		}
		if elem.Kind == ir.KindMaybe {
			l.failf(n, "sort of an array of %s is not supported yet: Node moves undefined to the end without calling the comparator", elem)
			return nil
		}
		op.Typ = t
		return l.withCallback(op, args[0], ir.FuncType([]ir.Type{elem, elem}, ir.Number), false)
	case ir.ArrayReduce:
		if len(args) != 2 {
			l.failf(n, "reduce without an initial value is not supported yet; pass one")
			return nil
		}
		cbNode, initNode := args[0], args[1]
		init := l.expr(initNode)
		if init == nil {
			return nil
		}
		acc := init.ExprType()
		cb := l.callback(cbNode, ir.FuncType([]ir.Type{acc, elem, ir.Number, t}, acc), false)
		if cb == nil {
			return nil
		}
		op.Args, op.Typ = []ir.Expr{cb, init}, acc
		return op
	case ir.ArrayIncludes, ir.ArrayIndexOf:
		if len(args) != 1 {
			l.failf(n, "%s's fromIndex is not supported yet", m)
			return nil
		}
		if elem.Kind == ir.KindFunc {
			l.failf(n, "%s on an array of functions is not supported", m)
			return nil
		}
		op.Typ = ir.Boolean
		if m == ir.ArrayIndexOf {
			op.Typ = ir.Number
		}
	case ir.ArrayJoin:
		if what, why := unjoinable(elem); what != "" {
			l.failf(n, "join of an array of %s is not supported%s", what, why)
			return nil
		}
		op.Typ = ir.String
	case ir.ArraySlice:
		op.Typ = t
	case ir.ArrayAt:
		op.Typ = ir.OrUndefined(elem) // undefined outside the array (D9)
	}
	for _, a := range args {
		v := l.expr(a)
		if v == nil {
			return nil
		}
		op.Args = append(op.Args, v)
	}
	return op
}

// withCallback lowers cb into op's only argument, in its slot's signature.
func (l *lowerer) withCallback(op *ir.ArrayOp, cb *ast.Node, slot ir.Type, drop bool) ir.Expr {
	v := l.callback(cb, slot, drop)
	if v == nil {
		return nil
	}
	op.Args = []ir.Expr{v}
	return op
}

// callback lowers a method's callback into slot: a literal written in place
// takes the slot's signature (closureFor), and a function value of another
// is adapted to it (adapt). drop discards a result the slot has no use for.
func (l *lowerer) callback(n *ast.Node, slot ir.Type, drop bool) ir.Expr {
	if l.libGlobal(n, "Boolean") {
		return l.booleanCallback(n, slot) // truthy.go
	}
	if n.Kind == ast.KindArrowFunction || n.Kind == ast.KindFunctionExpression {
		if !l.closureShapeOK(n) {
			return nil
		}
		own := l.typeOf(n)
		if own.Kind != ir.KindFunc {
			l.failClosureType(n)
			return nil
		}
		return l.closureFor(n, own.Sig, slot.Sig, drop)
	}
	v := l.expr(n)
	if v == nil || v.ExprType().Equal(slot) {
		return v
	}
	if v.ExprType().Kind != ir.KindFunc {
		l.failf(n, "a %s can't be a callback", v.ExprType())
		return nil
	}
	return l.adapt(n, v, slot)
}

// unjoinable names what in an element of type t join can't turn into a
// string as Node does, and why, or returns "": a function, whose string is
// its source text; a promise or a host value; and an object with its own
// toString or valueOf, which Node's ToString calls rather than print
// [object Object]; and, for now, a maybe. Arrays are joined in turn, so
// their elements are held to the same.
func unjoinable(t ir.Type) (what, why string) {
	switch t.Kind {
	case ir.KindFunc:
		return "functions", ": Node joins their source text"
	case ir.KindHost, ir.KindPromise:
		return t.String() + " values", ""
	case ir.KindMaybe:
		return "values that may be null or undefined", " yet: Node joins them as empty strings"
	case ir.KindArray:
		if t.Elem != nil {
			return unjoinable(*t.Elem)
		}
	case ir.KindObject:
		if t.Shape != nil && (t.Shape.Index("toString") >= 0 || t.Shape.Index("valueOf") >= 0) {
			return "objects with their own toString or valueOf", ": Node calls it"
		}
	}
	return "", ""
}

// callbackResult is what a callback returns, by its own type.
func (l *lowerer) callbackResult(n *ast.Node) ir.Type {
	if t := l.typeOf(n); t.Kind == ir.KindFunc && t.Sig != nil {
		return t.Sig.Result
	}
	return ir.Invalid
}
