package lower

import (
	"strconv"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/scanner"
	"github.com/zobstory/cakebear/ir"
)

// Shorthand properties and object destructuring (M4 plan D14). Both are
// spellings of field reads and writes the IR has already: `{ x }` is
// `{ x: x }`, and `const { x, y: b } = p` evaluates p once, into a binding of
// its own, and reads x and y from it. Array destructuring is tuple-centric
// and gives undefined past the end, so it waits; defaults, rest elements and
// computed keys are refused too.

// tempName is a fresh name for a value lowering holds on to, such as the
// object a pattern destructures. A space is in no TypeScript identifier, so
// it meets no name of the program's.
func (l *lowerer) tempName(what string) string {
	l.temps++
	return "ts " + what + strconv.Itoa(l.temps)
}

// shorthand lowers `{ x }`'s value, the variable x. The name of a shorthand
// property is its property; the checker resolves the value it reads
// separately.
func (l *lowerer) shorthand(p *ast.Node) ir.Expr {
	sp := p.AsShorthandPropertyAssignment()
	if sp.ObjectAssignmentInitializer != nil {
		l.failf(p, "a default in an object literal is only valid in a destructuring pattern")
		return nil
	}
	sym := l.p.resolve(l.checker.GetShorthandAssignmentValueSymbol(p))
	if sym == nil || len(sym.Declarations()) == 0 {
		l.invalidf(p, "the shorthand %s names nothing", p.Name().Text())
		return nil
	}
	if m, found, _ := l.hostMemberOf(sym); found {
		l.failHostValue(p, m)
		return nil
	}
	for _, d := range sym.Declarations() {
		if !l.p.inProgram(d) {
			l.failf(p, "%s is a library global cakebear does not implement yet", p.Name().Text())
			return nil
		}
	}
	t := l.typeFromType(l.checker.GetTypeOfSymbol(sym))
	if t == ir.Invalid {
		l.failf(p, "%s has a type the backend cannot represent yet%s", p.Name().Text(), l.reason())
		return nil
	}
	name, module := sym.Name(), topLevelModule(sym)
	if module == "" {
		name = p.Name().Text()
	}
	return &ir.Ident{Name: name, Module: module, Stable: !l.p.assigned[sym], Typ: t, Span: l.span(p)}
}

// destructure lowers `const { … } = init` (or let), declared at n: init into
// a binding of its own, then a binding per name the pattern gives, read from
// it. topLevel says the declaration is at its module's top level, where a
// reference to the held value names the module, as one to any top-level
// binding does.
func (l *lowerer) destructure(n, pattern *ast.Node, init ir.Expr, mutable, topLevel bool) []ir.Stmt {
	t := init.ExprType()
	held := &ir.VarDecl{Name: l.tempName("destructured"), Type: t, Init: init, Span: l.span(n)}
	source := &ir.Ident{Name: held.Name, Stable: true, Typ: t, Span: held.Span}
	if topLevel {
		source.Module = string(l.file.FileName())
	}
	bound := l.bindPattern(pattern, source, mutable)
	if bound == nil {
		return nil
	}
	return append([]ir.Stmt{held}, bound...)
}

// bindPattern declares each name of an object binding pattern, reading it
// from source, the value destructured: `{ a, b: c, d: { e } }`. It returns
// nil once it has refused something.
func (l *lowerer) bindPattern(pattern *ast.Node, source ir.Expr, mutable bool) []ir.Stmt {
	switch pattern.Kind {
	case ast.KindArrayBindingPattern:
		l.failf(pattern, "array destructuring is not supported yet")
		return nil
	case ast.KindObjectBindingPattern:
	default:
		l.invalidf(pattern, "%s is not a binding pattern", describeKind(pattern.Kind))
		return nil
	}
	switch t := source.ExprType(); t.Kind {
	case ir.KindObject:
	case ir.KindHost:
		l.failf(pattern, "destructuring %s, a node:http value, is not supported yet; read its members instead, as in `req.url`", t)
		return nil
	default:
		l.failf(pattern, "destructuring a %s is not supported yet; only an object", t)
		return nil
	}
	shape := source.ExprType().Shape
	var out []ir.Stmt
	for _, e := range pattern.AsBindingPattern().Elements.Nodes {
		el := e.AsBindingElement()
		switch {
		case el.DotDotDotToken != nil:
			l.failf(e, "a rest element in destructuring is not supported yet")
			return nil
		}
		keyNode := el.PropertyName
		if keyNode == nil {
			keyNode = el.Name()
		}
		key, ok := l.literalKey(keyNode)
		if !ok {
			return nil
		}
		i := shape.Index(key)
		if i < 0 {
			l.invalidf(e, "%s has no property %s", shape, key)
			return nil
		}
		var value ir.Expr = &ir.FieldGet{Object: source, Field: i, Typ: shape.Fields[i].Type, Span: l.span(e)}
		if el.Initializer != nil {
			// `{ a = d }`: d where the field is undefined (params.go).
			if value = l.defaulted(value, el.Initializer, l.typeOf(e)); value == nil {
				return nil
			}
		}
		if name := el.Name(); name.Kind == ast.KindIdentifier {
			out = append(out, &ir.VarDecl{Name: name.Text(), Type: value.ExprType(), Init: value, Mutable: mutable, Span: l.span(e)})
			continue
		}
		if el.Initializer != nil {
			// A nested pattern reads its value once per name: hold it.
			held := &ir.VarDecl{Name: l.tempName("destructured"), Type: value.ExprType(), Init: value, Span: l.span(e)}
			out = append(out, held)
			value = &ir.Ident{Name: held.Name, Stable: true, Typ: held.Type, Span: held.Span}
		}
		nested := l.bindPattern(el.Name(), value, mutable)
		if nested == nil {
			return nil
		}
		out = append(out, nested...)
	}
	return out
}

// bindingText is a binding name as the source spells it: an identifier, or a
// pattern such as `{ x, y }`.
func bindingText(name *ast.Node) string {
	if name.Kind == ast.KindIdentifier {
		return name.Text()
	}
	return scanner.GetTextOfNode(name)
}

// bindingRoot is the declaration a binding element belongs to: the variable
// declaration or parameter whose pattern holds it, through any nesting. For
// any other declaration it is decl itself.
func bindingRoot(decl *ast.Node) *ast.Node {
	for decl != nil && decl.Kind == ast.KindBindingElement {
		decl = decl.Parent // the pattern
		if decl != nil {
			decl = decl.Parent // a binding element, a variable declaration or a parameter
		}
	}
	return decl
}

// isConstDecl reports whether decl, or the declaration its binding pattern
// belongs to, is a const.
func isConstDecl(decl *ast.Node) bool {
	root := bindingRoot(decl)
	return root != nil && root.Kind == ast.KindVariableDeclaration && root.Parent != nil &&
		root.Parent.Flags&ast.NodeFlagsConst != 0
}
