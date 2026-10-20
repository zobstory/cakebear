package lower

import (
	"slices"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// Conditional spread (M5 plan D7, §2c), the idiom real code builds an object
// with optional properties by: `{ a, ...(c && { k: v }) }`, or
// `...(c ? { k: v } : {})`. The object has k when c is truthy. It lowers to a
// literal for each way the spreads can go, chosen by a Cond on each test, so
// every literal keeps its keys in source order, the order Node gives them.

// maxSpreads bounds the conditional spreads in one literal: each doubles the
// literals it lowers to.
const maxSpreads = 4

// literalItem is one entry of a literal with spreads: a property, or a
// conditional spread, whose test chooses then's properties or els's.
type literalItem struct {
	field     ir.FieldInit
	test      ir.Expr
	then, els []ir.FieldInit
}

// spreadLit lowers n, an object literal of type t holding conditional
// spreads. JavaScript evaluates its entries in order: a property's value,
// then a spread's test and the properties it chose, then the next. So a value
// before a later test is held, in a Let, where reading it again would run
// code, and the literals the Conds choose read it there.
func (l *lowerer) spreadLit(n *ast.Node, t ir.Type, props []*ast.Node) ir.Expr {
	shape := t.Shape
	var items []literalItem
	for _, p := range props {
		if p.Kind != ast.KindSpreadAssignment {
			f, ok := l.literalField(p, shape)
			if !ok {
				return nil
			}
			items = append(items, literalItem{field: f})
			continue
		}
		item, ok := l.conditionalSpread(p, shape)
		if !ok {
			return nil
		}
		items = append(items, item)
	}
	spreads := 0
	set := map[int]int{}
	for _, it := range items {
		if it.test == nil {
			set[it.field.Field]++
			continue
		}
		spreads++
		for _, f := range uniqueFields(it.then, it.els) {
			set[f]++
		}
	}
	if spreads > maxSpreads {
		l.failf(n, "an object literal with more than %d conditional spreads is not supported yet", maxSpreads)
		return nil
	}
	for i, f := range shape.Fields {
		switch {
		case set[i] > 1:
			l.failf(n, "a property set by a spread and elsewhere in the same literal, %s, is not supported yet", f.Name)
			return nil
		case !f.Optional && !alwaysSet(items, i):
			// TypeScript types a spread's key as required only where it
			// knows the test always holds, as `true && { k }`.
			l.failf(n, "a spread whose test always holds, setting %s, a required property of %s, is not supported: write %s in the literal itself", f.Name, shape, f.Name)
			return nil
		}
	}
	return l.spreadFrom(items, 0, nil, t, l.span(n), lastSpread(items))
}

// lastSpread is the index of the last spread among items.
func lastSpread(items []literalItem) int {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].test != nil {
			return i
		}
	}
	return -1
}

// uniqueFields lists the fields either branch of a spread sets, each once.
func uniqueFields(then, els []ir.FieldInit) []int {
	var out []int
	for _, f := range append(slices.Clone(then), els...) {
		if !slices.Contains(out, f.Field) {
			out = append(out, f.Field)
		}
	}
	return out
}

// alwaysSet reports whether field i is set however the spreads go: by a
// property, or by both branches of a spread.
func alwaysSet(items []literalItem, i int) bool {
	sets := func(fs []ir.FieldInit) bool {
		return slices.ContainsFunc(fs, func(f ir.FieldInit) bool { return f.Field == i })
	}
	for _, it := range items {
		if it.test == nil && it.field.Field == i || it.test != nil && sets(it.then) && sets(it.els) {
			return true
		}
	}
	return false
}

// conditionalSpread lowers p, `...(c && { … })` or `...(c ? { … } : { … })`,
// whose literals' properties are the outer literal's, of shape.
func (l *lowerer) conditionalSpread(p *ast.Node, shape *ir.Shape) (literalItem, bool) {
	e := ast.SkipParentheses(p.AsSpreadAssignment().Expression)
	var test, then, els *ast.Node
	switch {
	case e.Kind == ast.KindBinaryExpression && e.AsBinaryExpression().OperatorToken.Kind == ast.KindAmpersandAmpersandToken:
		test, then = e.AsBinaryExpression().Left, ast.SkipParentheses(e.AsBinaryExpression().Right)
	case e.Kind == ast.KindConditionalExpression:
		c := e.AsConditionalExpression()
		test, then, els = c.Condition, ast.SkipParentheses(c.WhenTrue), ast.SkipParentheses(c.WhenFalse)
	}
	literal := func(n *ast.Node) bool { return n == nil || n.Kind == ast.KindObjectLiteralExpression }
	if test == nil || !literal(then) || !literal(els) {
		l.failf(p, "spreading into an object literal is not supported yet, but for a conditional spread of literals: ...(c && { k: v }) or ...(c ? { k: v } : {})")
		return literalItem{}, false
	}
	item := literalItem{test: l.truthy(test)}
	if item.test == nil {
		return literalItem{}, false
	}
	var ok bool
	if item.then, ok = l.spreadFields(then, shape); !ok {
		return literalItem{}, false
	}
	if item.els, ok = l.spreadFields(els, shape); !ok {
		return literalItem{}, false
	}
	return item, true
}

// spreadFields lowers the properties of a spread's literal n, nil for none,
// as properties of the outer literal's shape.
func (l *lowerer) spreadFields(n *ast.Node, shape *ir.Shape) ([]ir.FieldInit, bool) {
	if n == nil {
		return nil, true
	}
	var out []ir.FieldInit
	for _, p := range n.AsObjectLiteralExpression().Properties.Nodes {
		if p.Kind == ast.KindSpreadAssignment {
			l.failf(p, "a spread inside a spread literal is not supported yet")
			return nil, false
		}
		f, ok := l.literalField(p, shape)
		if !ok {
			return nil, false
		}
		f.Value = into(f.Value, shape.Fields[f.Field].Type)
		if slices.ContainsFunc(out, func(g ir.FieldInit) bool { return g.Field == f.Field }) {
			l.failf(p, "a spread literal setting %s twice is not supported", shape.Fields[f.Field].Name)
			return nil, false
		}
		out = append(out, f)
	}
	return out, true
}

// spreadFrom builds the literal from items[i] on, fields being what the
// items before it set. A value before the last spread is held, so it is read
// in order before the tests after it; the branches each build the rest, and
// so share the nodes of the values after a spread: only one branch runs.
func (l *lowerer) spreadFrom(items []literalItem, i int, fields []ir.FieldInit, t ir.Type, span ir.Span, last int) ir.Expr {
	if i == len(items) {
		return &ir.ObjectLit{Fields: slices.Clone(fields), Typ: t, Span: span}
	}
	it := items[i]
	if it.test == nil {
		return l.withField(it.field, items, i, fields, t, span, last)
	}
	branch := func(set []ir.FieldInit) ir.Expr {
		return l.withFields(set, items, i, fields, t, span, last)
	}
	then, els := branch(it.then), branch(it.els)
	if then == nil || els == nil {
		return nil
	}
	return &ir.Cond{Test: it.test, Then: then, Else: els, Typ: t, Span: span}
}

// withField sets f, held where a later test must see it evaluated first, and
// builds the rest from items[i+1].
func (l *lowerer) withField(f ir.FieldInit, items []literalItem, i int, fields []ir.FieldInit, t ir.Type, span ir.Span, last int) ir.Expr {
	if i > last {
		return l.spreadFrom(items, i+1, append(slices.Clone(fields), f), t, span, last)
	}
	return l.hold(f.Value, func(held func() ir.Expr) ir.Expr {
		f.Value = held()
		return l.spreadFrom(items, i+1, append(slices.Clone(fields), f), t, span, last)
	})
}

// withFields sets a spread's chosen fields, in order, held as withField
// holds one, and builds the rest from items[i+1].
func (l *lowerer) withFields(set []ir.FieldInit, items []literalItem, i int, fields []ir.FieldInit, t ir.Type, span ir.Span, last int) ir.Expr {
	if len(set) == 0 {
		return l.spreadFrom(items, i+1, fields, t, span, last)
	}
	f := set[0]
	if i >= last {
		return l.withFields(set[1:], items, i, append(slices.Clone(fields), f), t, span, last)
	}
	return l.hold(f.Value, func(held func() ir.Expr) ir.Expr {
		f.Value = held()
		return l.withFields(set[1:], items, i, append(slices.Clone(fields), f), t, span, last)
	})
}
