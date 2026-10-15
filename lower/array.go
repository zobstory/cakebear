package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// Arrays (M4 plan D7–D9, D13): T[] and ReadonlyArray<T> are ir.ArrayOf(T),
// a reference to their elements, so every alias of one sees a push. Their
// readonliness is TypeScript's to check; at run time the two are one.
//
// Reading past the end, which Node answers with undefined, and writing past
// it or making a hole, which Node allows, panic at run time (D8, D9). The
// members recognised here are length, push and pop; the iteration methods
// are step 7's.

// arrayType maps T[] onto the IR, or returns ir.Invalid with l.why saying
// what about its elements can't be represented.
func (l *lowerer) arrayType(t *checker.Type) ir.Type {
	elem := l.typeFromType(l.checker.GetElementTypeOfArrayType(t))
	if !storable(elem) {
		if l.why == "" {
			l.explain("its elements have a type the backend cannot represent yet")
		} else {
			l.explain("its elements: " + l.why)
		}
		return ir.Invalid
	}
	return ir.ArrayOf(elem)
}

// arrayLit lowers an array literal. Its type is its slot's, as an object
// literal's is: TypeScript types `[]` alone as never[], so an empty literal
// can only take its element type from where it goes.
func (l *lowerer) arrayLit(n *ast.Node) ir.Expr {
	t := ir.Invalid
	if ct := l.checker.GetContextualType(n, checker.ContextFlagsNone); ct != nil {
		t = l.typeFromType(ct)
	}
	if t.Kind != ir.KindArray {
		t = l.typeOf(n)
	}
	elems := n.AsArrayLiteralExpression().Elements.Nodes
	if t.Kind != ir.KindArray {
		if len(elems) == 0 {
			l.failf(n, "an empty array literal needs its type from its slot, such as a variable's annotation")
		} else {
			l.failf(n, "this array literal has a type the backend cannot represent yet%s", l.reason())
		}
		return nil
	}
	out := &ir.ArrayLit{Typ: t, Span: l.span(n)}
	for _, e := range elems {
		switch e.Kind {
		case ast.KindSpreadElement:
			l.failf(e, "spreading into an array literal is not supported yet")
			return nil
		case ast.KindOmittedExpression:
			l.failf(n, "an array literal with a hole is not supported")
			return nil
		}
		v := l.expr(e)
		if v == nil {
			return nil
		}
		if !v.ExprType().Equal(*t.Elem) {
			l.failf(e, "an element of a %s can't be a %s", t, v.ExprType())
			return nil
		}
		out.Elems = append(out.Elems, v)
	}
	return out
}

// elementGet lowers `xs[i]`, an array's element.
func (l *lowerer) elementGet(n *ast.Node) ir.Expr {
	access := n.AsElementAccessExpression()
	arr, index, ok := l.element(n, access)
	if !ok {
		return nil
	}
	return &ir.IndexGet{Array: arr, Index: index, Typ: *arr.ExprType().Elem, Span: l.span(n)}
}

// element lowers an element access's array and index, refusing one that
// isn't an array indexed by a number.
func (l *lowerer) element(n *ast.Node, access *ast.ElementAccessExpression) (arr, index ir.Expr, ok bool) {
	if access.QuestionDotToken != nil {
		l.failf(n, "optional chaining (?.) is not supported yet")
		return nil, nil, false
	}
	if t := l.typeOf(access.Expression); t.Kind != ir.KindArray {
		l.failf(n, "an element access on a %s is not supported yet; an object's property is read by its name", t)
		return nil, nil, false
	}
	if arr = l.expr(access.Expression); arr == nil {
		return nil, nil, false
	}
	if index = l.expr(access.ArgumentExpression); index == nil {
		return nil, nil, false
	}
	if index.ExprType() != ir.Number {
		l.failf(access.ArgumentExpression, "an array's index must be a number, not a %s", index.ExprType())
		return nil, nil, false
	}
	return arr, index, true
}

// arrayMember resolves `xs.name` when xs is an array: ok reports whether it
// is, and name is the member, one TypeScript's libs declare. A member a
// program adds to Array through declaration merging has nothing behind it
// at run time; it is refused here, and name is "".
func (l *lowerer) arrayMember(access *ast.PropertyAccessExpression) (name string, ok bool) {
	if l.typeOf(access.Expression).Kind != ir.KindArray {
		return "", false
	}
	if sym := l.checker.GetSymbolAtLocation(access.Name()); !onlyInBundledLib(sym, "") {
		l.failf(access.Name(), "%s is added to Array outside TypeScript's libs, which is not supported", access.Name().Text())
		return "", true
	}
	return access.Name().Text(), true
}

// arrayProperty lowers `xs.length`, and reports whether n was a member of an
// array.
func (l *lowerer) arrayProperty(n *ast.Node) (ir.Expr, bool) {
	access := n.AsPropertyAccessExpression()
	name, ok := l.arrayMember(access)
	switch {
	case !ok:
		return nil, false
	case name == "":
		return nil, true
	case name != "length":
		l.failf(n, "an array's %s can only be called", name)
		return nil, true
	}
	arr := l.expr(access.Expression)
	if arr == nil {
		return nil, true
	}
	return &ir.ArrayOp{Method: ir.ArrayLength, Array: arr, Typ: ir.Number, Span: l.span(n)}, true
}

// arrayCall lowers a call of an array's member, `xs.push(a, b)`, as a value,
// and reports whether n was one. pop is a statement only (popStatement).
func (l *lowerer) arrayCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	access := c.Expression.AsPropertyAccessExpression()
	name, ok := l.arrayMember(access)
	if !ok {
		return nil, false
	}
	switch name {
	case "":
		return nil, true
	case "push":
	case "pop":
		l.failf(n, "pop's value is an element or undefined, which waits for M5; call it as a statement of its own")
		return nil, true
	default:
		if m, ok := arrayMethods[name]; ok {
			return l.arrayMethod(n, c, access, m), true
		}
		if why, ok := refusedArrayMembers[name]; ok && why != "" {
			l.failf(n, "%s", why)
		} else {
			l.failf(n, "the array method %s is not supported yet", name)
		}
		return nil, true
	}
	arr := l.expr(access.Expression)
	if arr == nil {
		return nil, true
	}
	op := &ir.ArrayOp{Method: ir.ArrayPush, Array: arr, Typ: ir.Number, Span: l.span(n)}
	for _, a := range c.Arguments.Nodes {
		if a.Kind == ast.KindSpreadElement {
			l.failf(a, "spreading arguments into push is not supported yet")
			return nil, true
		}
		v := l.expr(a)
		if v == nil {
			return nil, true
		}
		op.Args = append(op.Args, v)
	}
	if len(op.Args) == 0 {
		l.failf(n, "push of nothing is not supported; it only reads the length")
		return nil, true
	}
	return op, true
}

// popStatement lowers `xs.pop();` as a statement, and reports whether expr
// was one.
func (l *lowerer) popStatement(stmt, expr *ast.Node) ([]ir.Stmt, bool) {
	if expr.Kind != ast.KindCallExpression {
		return nil, false
	}
	c := expr.AsCallExpression()
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	access := c.Expression.AsPropertyAccessExpression()
	if name, ok := l.arrayMember(access); !ok || (name != "pop" && name != "") {
		return nil, false
	} else if name == "" {
		return nil, true
	}
	arr := l.expr(access.Expression)
	if arr == nil {
		return nil, true
	}
	op := &ir.ArrayOp{Method: ir.ArrayPop, Array: arr, Typ: ir.Void, Span: l.span(expr)}
	return []ir.Stmt{&ir.ExprStmt{Expr: op, Span: l.span(stmt)}}, true
}

// arrayAssign lowers `xs[i] = v` and `xs.length = n`, and reports whether
// target was an array's element or length.
func (l *lowerer) arrayAssign(stmt, target, value *ast.Node) ([]ir.Stmt, bool) {
	switch target.Kind {
	case ast.KindElementAccessExpression:
		arr, index, ok := l.element(target, target.AsElementAccessExpression())
		if !ok {
			return nil, true
		}
		v := l.expr(value)
		if v == nil {
			return nil, true
		}
		return []ir.Stmt{&ir.IndexSet{Array: arr, Index: index, Value: v, Span: l.span(stmt)}}, true
	case ast.KindPropertyAccessExpression:
		access := target.AsPropertyAccessExpression()
		if name, ok := l.arrayMember(access); !ok || (name != "length" && name != "") {
			return nil, false
		} else if name == "" {
			return nil, true
		}
		arr := l.expr(access.Expression)
		v := l.expr(value)
		if arr == nil || v == nil {
			return nil, true
		}
		op := &ir.ArrayOp{Method: ir.ArraySetLength, Array: arr, Args: []ir.Expr{v}, Typ: ir.Void, Span: l.span(stmt)}
		return []ir.Stmt{&ir.ExprStmt{Expr: op, Span: l.span(stmt)}}, true
	}
	return nil, false
}

// forOf lowers `for (const x of xs) body` over an array (D13). The loop
// variable is a binding per iteration, as in JavaScript, and its type is the
// array's element: TypeScript allows no annotation there.
func (l *lowerer) forOf(n *ast.Node) []ir.Stmt {
	s := n.AsForInOrOfStatement()
	if s.AwaitModifier != nil {
		l.failf(n, "for await is not supported yet")
		return nil
	}
	init := s.Initializer
	if init.Kind != ast.KindVariableDeclarationList {
		l.failf(init, "a for…of loop needs its own const or let; assigning to an existing variable is not supported yet")
		return nil
	}
	list := init.AsVariableDeclarationList()
	if list.Flags&(ast.NodeFlagsConst|ast.NodeFlagsLet) == 0 {
		l.failf(init, "`var` is not supported; use `const` or `let`")
		return nil
	}
	decl := list.Declarations.Nodes[0]
	t := l.typeOf(s.Expression)
	if t.Kind != ir.KindArray {
		l.failf(s.Expression, "for…of over a %s is not supported yet; only over an array", t)
		return nil
	}
	arr := l.expr(s.Expression)
	if arr == nil {
		return nil
	}
	mutable := list.Flags&ast.NodeFlagsConst == 0
	name := bindingText(decl.Name())
	var prologue []ir.Stmt
	if decl.Name().Kind != ast.KindIdentifier {
		// `for (const { a, b } of xs)`: the element gets a name of its own,
		// and the body opens by binding the pattern's names from it.
		name = l.tempName("element")
		source := &ir.Ident{Name: name, Stable: true, Typ: *t.Elem, Span: l.span(decl)}
		if prologue = l.bindPattern(decl.Name(), source, mutable); prologue == nil {
			return nil
		}
	}
	body := append(prologue, l.statementBody(s.Statement)...)
	return []ir.Stmt{&ir.ForOf{
		Name: name, Type: *t.Elem, Mutable: mutable, Array: arr, Body: body, Span: l.span(n),
	}}
}
