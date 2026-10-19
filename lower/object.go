package lower

import (
	"strconv"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// Objects (M4 plan D2–D6): an object type is a shape, interned program-wide,
// so `interface Point { x: number; y: number }` and `type Vec = { y: number;
// x: number }` are one shape and one Go struct. Shapes are exact: an object
// has the fields of its type and no others (D5), and what M4 can't hold
// exactly is refused by name (D6).

// objectResult is what objectType made of a checker type, kept so each type
// is classified once.
type objectResult struct {
	typ ir.Type
	why string // why typ is ir.Invalid
}

// isObjectType reports whether t is an object type that isn't callable: a
// candidate for a shape rather than a function type.
func (l *lowerer) isObjectType(t *checker.Type) bool {
	return t.Flags()&checker.TypeFlagsObject != 0 &&
		len(l.checker.GetSignaturesOfType(t, checker.SignatureKindCall)) == 0 &&
		len(l.checker.GetSignaturesOfType(t, checker.SignatureKindConstruct)) == 0
}

// objectType maps an object type onto its shape, or returns ir.Invalid with
// l.why saying what about it M4 can't hold.
func (l *lowerer) objectType(t *checker.Type) ir.Type {
	p := l.p
	if r, ok := p.objects[t]; ok {
		l.explain(r.why)
		return r.typ
	}
	if s, ok := p.pending[t]; ok {
		// A type that refers to itself: reserve its shape now, for the
		// fields under construction to point at (ir.Shapes.Reserve).
		if s == nil {
			s = p.shapes.Reserve(typeName(t))
			p.pending[t] = s
		}
		return ir.ObjectOf(s)
	}
	p.pending[t] = nil
	logged := len(p.objectLog)
	typ, why := l.shapeOf(t)
	reserved := p.pending[t]
	delete(p.pending, t)
	if typ != ir.Invalid && reserved != nil {
		if same := p.shapes.Define(reserved, typ.Shape.Fields); same != nil {
			typ, why = ir.Invalid, "it is a recursive type with the same fields as "+same.String()+", which cakebear can't tell apart yet"
		} else {
			typ = ir.ObjectOf(reserved)
		}
	}
	if typ == ir.Invalid && reserved != nil {
		// What was classified meanwhile may point at the reserved shape,
		// which now never gets fields: classify it again, knowing t fails.
		for _, u := range p.objectLog[logged:] {
			delete(p.objects, u)
		}
		p.objectLog = p.objectLog[:logged]
	}
	p.objects[t] = objectResult{typ, why}
	p.objectLog = append(p.objectLog, t)
	l.explain(why)
	return typ
}

// shapeOf classifies t's properties. The shape it returns is interned, or,
// for a type that turns out to refer to itself, holds the fields the
// reserved shape is to be defined with.
func (l *lowerer) shapeOf(t *checker.Type) (ir.Type, string) {
	c := l.checker
	switch {
	case t.ObjectFlags()&checker.ObjectFlagsClass != 0:
		return ir.Invalid, "classes are not supported yet"
	case len(c.GetIndexInfosOfType(t)) > 0:
		return ir.Invalid, "index signatures, and Record, are not supported yet"
	}
	props := c.GetPropertiesOfType(t)
	if len(props) == 0 {
		return ir.Invalid, "an object type with no properties is not supported"
	}
	fields := make([]ir.Field, 0, len(props))
	for _, prop := range props {
		name := prop.Name()
		flags := prop.Flags()
		switch {
		case flags&ast.SymbolFlagsOptional != 0:
			return ir.Invalid, "optional property `" + name + "` is not supported yet"
		case flags&ast.SymbolFlagsMethod != 0:
			return ir.Invalid, "method `" + name + "` is not supported yet; a property holding an arrow function is"
		case flags&ast.SymbolFlagsAccessor != 0:
			return ir.Invalid, "accessor `" + name + "` is not supported yet"
		}
		if why := keyRefusal(name); why != "" {
			return ir.Invalid, why
		}
		ft := l.typeFromType(c.GetTypeOfSymbol(prop))
		if !storable(ft) {
			why := "property `" + name + "` has a type the backend cannot represent yet"
			if l.why != "" {
				why = "property `" + name + "`: " + l.why
			}
			return ir.Invalid, why
		}
		fields = append(fields, ir.Field{Name: name, Type: ft})
	}
	if l.p.pending[t] != nil {
		// Defined by objectType once every field is known.
		return ir.ObjectOf(&ir.Shape{Fields: fields}), ""
	}
	return ir.ObjectOf(l.p.shapes.Intern(typeName(t), fields)), ""
}

// keyRefusal says why name can't be a property's name in M4, or returns "".
// Node prints integer-like keys first, in numeric order, whatever order they
// were written in (R6), which a shape's static key table can't follow; and
// `__proto__` sets the prototype rather than a property.
func keyRefusal(name string) string {
	switch {
	case name == "__proto__":
		return "a property named `__proto__` is not supported"
	case isIndexKey(name):
		return "an integer-like property name (`" + name + "`) is not supported yet"
	case strings.HasPrefix(name, ast.InternalSymbolNamePrefix):
		return "a symbol or computed property name is not supported yet"
	}
	return ""
}

// isIndexKey reports whether name is an array index as a string, "0" to
// "4294967294" with no leading zero: the keys an object lists first.
func isIndexKey(name string) bool {
	if name == "" || (len(name) > 1 && name[0] == '0') {
		return false
	}
	n, err := strconv.ParseUint(name, 10, 64)
	return err == nil && n <= 4294967294
}

// typeName is the interface or type alias that names t, for messages, or "".
func typeName(t *checker.Type) string {
	if a := t.Alias(); a != nil && a.Symbol() != nil {
		return a.Symbol().Name()
	}
	if s := t.Symbol(); s != nil && s.Flags()&(ast.SymbolFlagsInterface|ast.SymbolFlagsTypeAlias) != 0 {
		return s.Name()
	}
	return ""
}

// objectLit lowers an object literal. Its shape is its slot's, the type the
// checker gives it from context, such as a variable's annotation, a
// parameter's type or an enclosing literal's field; with no slot, such as an
// argument of console.log, it is the literal's own. TypeScript has already
// checked the literal sets exactly its slot's properties.
func (l *lowerer) objectLit(n *ast.Node) ir.Expr {
	t := ir.Invalid
	if ct := l.checker.GetContextualType(n, checker.ContextFlagsNone); ct != nil {
		t = elemOfMaybe(l.typeFromType(ct)) // a maybe's slot takes it wrapped
	}
	if t.Kind != ir.KindObject {
		t = l.typeOf(n)
	}
	if t.Kind != ir.KindObject {
		l.failf(n, "this object literal has a type the backend cannot represent yet%s", l.reason())
		return nil
	}
	shape := t.Shape
	out := &ir.ObjectLit{Typ: t, Span: l.span(n)}
	for _, p := range n.AsObjectLiteralExpression().Properties.Nodes {
		switch p.Kind {
		case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment:
		case ast.KindSpreadAssignment:
			l.failf(p, "spreading into an object literal is not supported yet")
			return nil
		case ast.KindMethodDeclaration:
			l.failf(p, "a method in an object literal is not supported yet; use a property holding an arrow function")
			return nil
		default:
			l.failf(p, "%s in an object literal is not supported yet", describeKind(p.Kind))
			return nil
		}
		name, ok := l.literalKey(p.Name())
		if !ok {
			return nil
		}
		i := shape.Index(name)
		if i < 0 {
			l.invalidf(p, "%s has no property %s", shape, name)
			return nil
		}
		var value ir.Expr
		if p.Kind == ast.KindShorthandPropertyAssignment {
			value = l.fitField(p, l.shorthand(p), shape.Fields[i].Type)
		} else {
			value = l.expr(p.AsPropertyAssignment().Initializer)
		}
		if value == nil {
			return nil
		}
		out.Fields = append(out.Fields, ir.FieldInit{Field: i, Value: value, Span: l.span(p)})
	}
	if len(out.Fields) != len(shape.Fields) {
		l.invalidf(n, "this literal sets %d of %s's %d properties", len(out.Fields), shape, len(shape.Fields))
		return nil
	}
	return out
}

// literalKey is the property name a literal writes, refusing the names D6
// does: computed and numeric ones, integer-like strings and `__proto__`.
func (l *lowerer) literalKey(name *ast.Node) (string, bool) {
	var key string
	switch name.Kind {
	case ast.KindIdentifier, ast.KindStringLiteral:
		key = name.Text()
	case ast.KindNumericLiteral:
		l.failf(name, "a numeric property name is not supported yet")
		return "", false
	default:
		l.failf(name, "a computed property name is not supported yet")
		return "", false
	}
	if why := keyRefusal(key); why != "" {
		l.failf(name, "%s", why)
		return "", false
	}
	return key, true
}

// field resolves `obj.f` when obj is an object: its lowered receiver and the
// field's index in its shape. ok is false when obj isn't an object, and
// lowered is nil if the receiver failed to lower.
func (l *lowerer) field(access *ast.PropertyAccessExpression) (lowered ir.Expr, index int, ok bool) {
	t := l.typeOf(access.Expression)
	if t.Kind != ir.KindObject {
		return nil, 0, false
	}
	index = t.Shape.Index(access.Name().Text())
	if index < 0 {
		return nil, 0, false
	}
	return l.expr(access.Expression), index, true
}

// fieldGet lowers `obj.f` for an object obj, and reports whether n was one.
func (l *lowerer) fieldGet(n *ast.Node) (ir.Expr, bool) {
	access := n.AsPropertyAccessExpression()
	recv, i, ok := l.field(access)
	if !ok {
		return nil, false
	}
	if access.QuestionDotToken != nil {
		l.failf(n, "optional chaining (?.) is not supported yet")
		return nil, true
	}
	if recv == nil {
		return nil, true
	}
	shape := recv.ExprType().Shape
	get := &ir.FieldGet{Object: recv, Field: i, Typ: shape.Fields[i].Type, Span: l.span(n)}
	return narrowed(get, l.typeOf(n), l.span(n)), true
}

// fieldSet lowers `obj.f = v`, and reports whether target was an object's
// field.
func (l *lowerer) fieldSet(stmt, target, value *ast.Node) ([]ir.Stmt, bool) {
	if target.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	recv, i, ok := l.field(target.AsPropertyAccessExpression())
	if !ok {
		return nil, false
	}
	v := l.expr(value)
	if recv == nil || v == nil {
		return nil, true
	}
	return []ir.Stmt{&ir.FieldSet{Object: recv, Field: i, Value: v, Span: l.span(stmt)}}, true
}

// exact holds a value of an object type to the slot it fills (D5): the
// shape of the variable, parameter, field or result it flows into, which
// is the contextual type the checker gives the expression there. TypeScript
// lets an object with more fields stand in for one with fewer; cakebear's
// objects have exactly their own fields, and a copy would lose identity, so
// that is refused.
func (l *lowerer) exact(n *ast.Node, v ir.Expr) bool {
	t := v.ExprType()
	if !holdsObject(t, nil) {
		return true
	}
	ct := l.checker.GetContextualType(n, checker.ContextFlagsNone)
	if ct == nil {
		return true
	}
	slot := l.typeFromType(ct)
	if slot == ir.Invalid {
		return true
	}
	if slot.Equal(t) {
		return true
	}
	if t.Kind == ir.KindObject && slot.Kind == ir.KindObject {
		l.failf(n, "a %s can't be used as a %s: an object has exactly the fields of its type in cakebear%s", t, slot, extraFields(t.Shape, slot.Shape))
	} else {
		l.failf(n, "a %s can't be used as a %s: their objects differ, and cakebear's objects have exactly the fields of their type", t, slot)
	}
	return false
}

// extraFields names what has but want lacks, for exact's message.
func extraFields(has, want *ir.Shape) string {
	var extra []string
	for _, f := range has.Fields {
		if want.Index(f.Name) < 0 {
			extra = append(extra, "`"+f.Name+"`")
		}
	}
	if len(extra) == 0 {
		return ""
	}
	return ", and it has " + strings.Join(extra, ", ") + " too"
}

// holdsObject reports whether a value of type t is, or holds, an object:
// where an exact flow matters.
func holdsObject(t ir.Type, seen map[*ir.Shape]bool) bool {
	switch t.Kind {
	case ir.KindObject:
		return true
	case ir.KindArray, ir.KindPromise, ir.KindMaybe:
		return t.Elem != nil && holdsObject(*t.Elem, seen)
	case ir.KindFunc:
		if t.Sig == nil {
			return false
		}
		for _, p := range t.Sig.Params {
			if holdsObject(p, seen) {
				return true
			}
		}
		return holdsObject(t.Sig.Result, seen)
	}
	return false
}

// because records why the type being mapped can't be represented, unless a
// reason is already recorded: the first one found is the innermost.
func (l *lowerer) because(why string) {
	if why != "" && l.why == "" {
		l.why = why
	}
}

// explain records why an object type can't be represented. It replaces a
// reason recorded for one of the type's parts, which it already includes.
func (l *lowerer) explain(why string) {
	if why != "" {
		l.why = why
	}
}

// reason is the recorded why, ready to follow a "can't represent" message.
func (l *lowerer) reason() string {
	if l.why == "" {
		return ""
	}
	return ": " + l.why
}
