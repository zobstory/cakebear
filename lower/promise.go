package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// Promise's statics and adoption (S1 plan D5): Promise.all over an array,
// and `const [a, b] = await Promise.all([fa(), fb()])` as a form of its
// own, since TypeScript types that a tuple the IR doesn't hold;
// Promise.race; Promise.resolve and reject; and an async function's
// `return p` and an executor's `resolve(p)` of a promise, which adopt it.
// They run in Node's order (runtime/promise_all.go, promise_adopt.go).
// allSettled, any, then, catch and finally stay refused (D1).

// promiseStatic reports whether c calls a static of TypeScript's own
// Promise, and which.
func (l *lowerer) promiseStatic(c *ast.CallExpression) (string, bool) {
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return "", false
	}
	recv := c.Expression.Expression()
	if recv.Kind != ast.KindIdentifier || recv.Text() != "Promise" || !onlyInBundledLib(l.checker.GetSymbolAtLocation(recv), "") {
		return "", false
	}
	return c.Expression.Name().Text(), true
}

// promiseCall lowers Promise.all, race, resolve and reject; handled is
// false for anything else.
func (l *lowerer) promiseCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	name, ok := l.promiseStatic(c)
	if !ok {
		return nil, false
	}
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}
	if name != "all" && name != "race" && name != "resolve" && name != "reject" {
		l.failf(n, "Promise.%s is not supported yet", name)
		return nil, true
	}
	span := l.calleeCaret(l.span(n), n)
	if name == "resolve" && len(args) == 0 {
		return &ir.HostCall{Op: ir.HostPromiseResolve, Typ: ir.PromiseOf(ir.Void), Span: span}, true
	}
	if len(args) != 1 {
		l.failf(n, "Promise.%s takes 1 argument here, got %d", name, len(args))
		return nil, true
	}
	switch name {
	case "reject":
		reason := l.box(args[0], l.exprOf(args[0]))
		if reason == nil {
			return nil, true
		}
		t := l.typeOf(n)
		if t.Kind != ir.KindPromise {
			l.failf(n, "Promise.reject's promise has a type the backend cannot represent yet%s", l.reason())
			return nil, true
		}
		return &ir.HostCall{Op: ir.HostPromiseReject, Args: []ir.Expr{reason}, Typ: t, Span: span}, true
	case "resolve":
		v := l.exprOf(args[0])
		if v == nil {
			return nil, true
		}
		if v.ExprType().Kind == ir.KindPromise {
			return v, true // Promise.resolve(p) is p itself (R21)
		}
		// A promise of what its slot holds, `number | undefined` for
		// `const p: Promise<number | undefined> = Promise.resolve(1)`, where
		// TypeScript types the call Promise<number>.
		t := l.slotType(n)
		if t.Kind != ir.KindPromise {
			t = l.typeOf(n)
		}
		if t.Kind == ir.KindPromise && t.Elem != nil && *t.Elem != ir.Void {
			if v = l.fitField(args[0], v, *t.Elem); v == nil { // adapt.go
				return nil, true
			}
		}
		return &ir.HostCall{Op: ir.HostPromiseResolve, Args: []ir.Expr{v}, Typ: ir.PromiseOf(v.ExprType()), Span: span}, true
	}
	var ps ir.Expr
	if args[0].Kind == ast.KindArrayLiteralExpression && l.typeFromTypeQuiet(l.checker.GetTypeAtLocation(args[0])) == ir.Invalid {
		// TypeScript types the literal a tuple, which the IR doesn't hold.
		// Whose value is dropped, all's is a promise of nothing; otherwise
		// its elements are promises of one type, an array of them.
		if name == "all" && dropped(n) {
			return l.promiseAllOf(n, args[0], span)
		}
		if ps = l.promiseArray(n, args[0], name); ps == nil {
			return nil, true
		}
	} else if ps = l.exprOf(args[0]); ps == nil {
		return nil, true
	}
	t := ps.ExprType()
	if t.Kind != ir.KindArray || t.Elem == nil {
		l.failf(args[0], "Promise.%s takes an array here, not %s", name, aType(t))
		return nil, true
	}
	elem := *t.Elem
	switch {
	case elem.Kind == ir.KindPromise && name == "all":
		return &ir.HostCall{Op: ir.HostPromiseAll, Args: []ir.Expr{ps}, Typ: ir.PromiseOf(ir.ArrayOf(*elem.Elem)), Span: span}, true
	case elem.Kind == ir.KindPromise:
		return &ir.HostCall{Op: ir.HostPromiseRace, Args: []ir.Expr{ps}, Typ: elem, Span: span}, true
	case name == "all":
		return &ir.HostCall{Op: ir.HostPromiseAllValues, Args: []ir.Expr{ps}, Typ: ir.PromiseOf(t), Span: span}, true
	}
	l.failf(args[0], "Promise.race of values, not promises, is not supported yet")
	return nil, true
}

// dropped reports whether n's value, awaited or not, is no part of anything:
// `await Promise.all([…]);` as a statement.
func dropped(n *ast.Node) bool {
	p := outside(n) // errors.go
	if p != nil && p.Kind == ast.KindAwaitExpression {
		p = outside(p)
	}
	return p != nil && p.Kind == ast.KindExpressionStatement
}

// allElement reports whether n is an element of the literal of a
// Promise.all whose values are bound by an array pattern, or dropped: each
// a slot of its own, which TypeScript types by the tuple it infers.
func (l *lowerer) allElement(n *ast.Node) bool {
	lit := outside(n)
	if lit == nil || lit.Kind != ast.KindArrayLiteralExpression {
		return false
	}
	call := lit.Parent
	if call == nil || call.Kind != ast.KindCallExpression || call.AsCallExpression().Arguments == nil ||
		len(call.AsCallExpression().Arguments.Nodes) != 1 || call.AsCallExpression().Arguments.Nodes[0] != lit {
		return false
	}
	if name, ok := l.promiseStatic(call.AsCallExpression()); !ok || name != "all" {
		return false
	}
	if dropped(call) {
		return true
	}
	await := outside(call)
	if await == nil || await.Kind != ast.KindAwaitExpression || await.Parent == nil || await.Parent.Kind != ast.KindVariableDeclaration {
		return false
	}
	decl := await.Parent.AsVariableDeclaration()
	return decl.Initializer == await && decl.Name().Kind == ast.KindArrayBindingPattern
}

// inTry reports whether n is in a try block, or in a catch block with a
// finally after it, of the function it is in: where something of the
// function's would run after its return.
func inTry(n *ast.Node) bool {
	for child, p := n, n.Parent; p != nil; child, p = p, p.Parent {
		switch p.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration, ast.KindSourceFile:
			return false
		case ast.KindTryStatement:
			s := p.AsTryStatement()
			if child == s.TryBlock || child == s.CatchClause && s.FinallyBlock != nil {
				return true
			}
		}
	}
	return false
}

// promiseArray lowers literal, the array of Promise.all's or race's
// arguments TypeScript types a tuple, into an array of promises, each value
// one Promise.resolve makes. Their types must be one; an empty one's is
// what the slot of n, the call, has all's array hold.
func (l *lowerer) promiseArray(n, literal *ast.Node, name string) ir.Expr {
	out := &ir.ArrayLit{Span: l.span(literal)}
	for _, e := range literal.AsArrayLiteralExpression().Elements.Nodes {
		if e.Kind == ast.KindSpreadElement || e.Kind == ast.KindOmittedExpression {
			l.failf(e, "a spread or a hole in Promise.%s's array is not supported yet", name)
			return nil
		}
		v := l.exprOf(e)
		if v == nil {
			return nil
		}
		if v.ExprType().Kind != ir.KindPromise {
			v = &ir.HostCall{Op: ir.HostPromiseResolve, Args: []ir.Expr{v}, Typ: ir.PromiseOf(v.ExprType()), Span: v.ExprSpan()}
		}
		if len(out.Elems) > 0 && !v.ExprType().Equal(out.Elems[0].ExprType()) {
			if name == "all" {
				l.failf(literal, "Promise.all of promises of different types is a tuple, which is not supported yet: destructure it, `const [a, b] = await Promise.all([…])`, or await it as a statement")
			} else {
				l.failf(literal, "Promise.race of promises of different types is not supported yet: what it settles with would be a union")
			}
			return nil
		}
		out.Elems = append(out.Elems, v)
	}
	if len(out.Elems) == 0 {
		slot := l.slotType(n)
		if slot.Kind == ir.KindPromise && slot.Elem != nil {
			slot = *slot.Elem // an await's operand has the await's slot
		}
		if name == "all" && slot.Kind == ir.KindArray && slot.Elem != nil {
			out.Typ = ir.ArrayOf(ir.PromiseOf(*slot.Elem))
			return out
		}
		l.failf(literal, "Promise.%s of an empty array literal is not supported here yet: give it a type", name)
		return nil
	}
	if elem := out.Elems[0].ExprType(); *elem.Elem == ir.Void {
		l.failf(literal, "Promise.%s of promises of nothing as a value is not supported yet: await it as a statement", name)
		return nil
	}
	out.Typ = ir.ArrayOf(out.Elems[0].ExprType())
	return out
}

// promiseAllOf lowers `Promise.all([…])` of a literal of promises of
// different types: each element a promise, a value one Promise.resolve
// makes, and a promise of nothing that settles as Promise.all would.
// Lowering the destructured form reads each value after (allDestructured).
func (l *lowerer) promiseAllOf(n, literal *ast.Node, span ir.Span) (ir.Expr, bool) {
	call := &ir.HostCall{Op: ir.HostPromiseAllOf, Typ: ir.PromiseOf(ir.Void), Span: span}
	for _, e := range literal.AsArrayLiteralExpression().Elements.Nodes {
		if e.Kind == ast.KindSpreadElement || e.Kind == ast.KindOmittedExpression {
			l.failf(e, "a spread or a hole in Promise.all's array is not supported yet")
			return nil, true
		}
		v := l.exprOf(e)
		if v == nil {
			return nil, true
		}
		if v.ExprType().Kind != ir.KindPromise {
			v = &ir.HostCall{Op: ir.HostPromiseResolve, Args: []ir.Expr{v}, Typ: ir.PromiseOf(v.ExprType()), Span: v.ExprSpan()}
		}
		call.Args = append(call.Args, v)
	}
	return call, true
}

// allDestructured lowers `const [a, b] = await Promise.all([fa(), fb()])`:
// each element held, the promise of them all awaited, and each name bound
// to its element's value. ok is false for any other declaration.
func (l *lowerer) allDestructured(pattern, init *ast.Node, mutable, topLevel bool) ([]ir.Stmt, bool) {
	if pattern.Kind != ast.KindArrayBindingPattern || init.Kind != ast.KindAwaitExpression {
		return nil, false
	}
	operand := init.AsAwaitExpression().Expression
	if operand.Kind != ast.KindCallExpression {
		return nil, false
	}
	c := operand.AsCallExpression()
	if name, ok := l.promiseStatic(c); !ok || name != "all" || c.Arguments == nil || len(c.Arguments.Nodes) != 1 ||
		c.Arguments.Nodes[0].Kind != ast.KindArrayLiteralExpression {
		return nil, false
	}
	if !l.async {
		l.failf(init, "await is allowed only in an async function or at the top level of a module")
		return nil, true
	}
	all, _ := l.promiseAllOf(operand, c.Arguments.Nodes[0], l.calleeCaret(l.span(operand), operand))
	if all == nil {
		return nil, true
	}
	call := all.(*ir.HostCall)
	var out []ir.Stmt
	held := make([]*ir.Ident, len(call.Args))
	for i, p := range call.Args {
		decl := &ir.VarDecl{Name: l.tempName("all"), Type: p.ExprType(), Init: p, Span: p.ExprSpan()}
		out = append(out, decl)
		held[i] = &ir.Ident{Name: decl.Name, Stable: true, Typ: decl.Type, Span: decl.Span}
		if topLevel {
			held[i].Module = string(l.file.FileName())
		}
		call.Args[i] = held[i]
	}
	out = append(out, &ir.ExprStmt{Expr: &ir.Await{Value: call, Typ: ir.Void, Span: l.span(init)}, Span: l.span(init)})
	for i, e := range pattern.AsBindingPattern().Elements.Nodes {
		be := e.AsBindingElement()
		if be.Name() == nil {
			continue // a hole, `[a, , c]`
		}
		if be.DotDotDotToken != nil || be.Name().Kind != ast.KindIdentifier || be.Initializer != nil {
			l.failf(e, "only a name is supported here, for each of Promise.all's values")
			return nil, true
		}
		if i >= len(held) {
			l.failf(e, "%s names no element of Promise.all's array", be.Name().Text())
			return nil, true
		}
		elem := *held[i].Typ.Elem
		if elem == ir.Void {
			l.failf(e, "%s would be undefined, the value of a promise of nothing, which is not supported yet: leave a hole in its place, `[a, , c]`", be.Name().Text())
			return nil, true
		}
		var v ir.Expr = &ir.HostCall{Op: ir.HostPromiseValue, Recv: held[i], Typ: elem, Span: l.span(e)}
		want := l.typeOf(be.Name())
		switch {
		case want.Equal(elem):
		case elem == ir.Unknown && want != ir.Invalid && ir.Decodable(want) == "":
			v = &ir.Decode{Value: v, Typ: want, Cast: l.checker.TypeToString(l.checker.GetTypeAtLocation(be.Name())), Span: l.span(e)}
		case want.Kind == ir.KindMaybe && intoMaybe(v, want).ExprType().Equal(want):
			v = intoMaybe(v, want)
		default:
			l.failf(e, "%s has a type the backend cannot represent yet%s", be.Name().Text(), l.reason())
			return nil, true
		}
		out = append(out, &ir.VarDecl{Name: be.Name().Text(), Type: want, Init: v, Mutable: mutable, Span: l.span(e)})
	}
	return out, true
}

// adopted is an async function's `return v` of a promise: an await that
// adopts it, decoded into the function's result where it is the `any` of
// res.json() (cast.go). ok is false for a v that is no promise; with ok,
// the expression is nil once it has refused it.
func (l *lowerer) adopted(n *ast.Node, v ir.Expr) (ir.Expr, bool) {
	t := v.ExprType()
	if !l.async || l.discard || t.Kind != ir.KindPromise || t.Elem == nil {
		return v, false
	}
	if inTry(n) {
		// Node adopts p once the function has returned, after its finally
		// and past its catch; an await in place would run both after p.
		l.failf(n, "returning a promise from inside a try is not supported yet: Node adopts it once the try has run, so neither its catch nor its finally sees it settle; return await it, or return it after the try")
		return nil, true
	}
	var out ir.Expr = &ir.Await{Value: v, Typ: *t.Elem, Span: v.ExprSpan(), Adopt: true}
	if slot := l.slotType(n); *t.Elem == ir.Unknown && slot != ir.Unknown && slot != ir.Invalid && ir.Decodable(slot) == "" {
		out = &ir.Decode{Value: out, Typ: slot, Cast: l.checker.TypeToString(l.contextualType(n)), Span: v.ExprSpan()}
	}
	return out, true
}

// adoptingExecutor lowers new Promise's executor where it calls resolve
// with a promise: of ir.ExecutorAdoptType, its third parameter the adopt
// that adoptCall makes each such call one of. ok is false for an executor
// that doesn't, and with ok the expression is nil once it has refused.
func (l *lowerer) adoptingExecutor(n, exec *ast.Node, params []*ast.Node, typ ir.Type) (ir.Expr, bool) {
	if len(params) == 0 || params[0].Name().Kind != ast.KindIdentifier {
		return nil, false
	}
	resolve := l.checker.GetSymbolAtLocation(params[0].Name())
	if resolve == nil || !l.resolvesWithAPromise(exec, resolve) {
		return nil, false
	}
	name := l.tempName("adopt")
	if l.adopters == nil {
		l.adopters = map[*ast.Symbol]string{}
	}
	l.adopters[resolve] = name
	executor := l.closureFor(exec, ir.ExecutorAdoptType(*typ.Elem).Sig, nil, true)
	lit, ok := executor.(*ir.FuncLit)
	if !ok {
		return nil, true
	}
	lit.Params[2].Name = name
	return &ir.NewPromise{Executor: lit, Typ: typ, Span: l.span(n), Adopting: true}, true
}

// resolvesWithAPromise reports whether exec calls resolve, anywhere in it,
// with an argument the checker types a promise.
func (l *lowerer) resolvesWithAPromise(exec *ast.Node, resolve *ast.Symbol) bool {
	found := false
	var visit func(*ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n.Kind == ast.KindCallExpression {
			c := n.AsCallExpression()
			if c.Expression.Kind == ast.KindIdentifier && l.checker.GetSymbolAtLocation(c.Expression) == resolve &&
				c.Arguments != nil && len(c.Arguments.Nodes) == 1 {
				if _, ok := l.promiseType(l.checker.GetTypeAtLocation(c.Arguments.Nodes[0])); ok {
					found = true
				}
			}
		}
		return found || n.ForEachChild(visit)
	}
	visit(exec.Body()) // an arrow's body may be the call itself
	return found
}

// adoptCall lowers resolve(p) of a promise p, in an adopting executor, as
// a call of its adopt. ok is false for any other call.
func (l *lowerer) adoptCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	if len(l.adopters) == 0 || c.Arguments == nil || len(c.Arguments.Nodes) != 1 {
		return nil, false
	}
	adopt, ok := l.adopters[l.checker.GetSymbolAtLocation(c.Expression)]
	if !ok {
		return nil, false
	}
	if _, isPromise := l.promiseType(l.checker.GetTypeAtLocation(c.Arguments.Nodes[0])); !isPromise {
		return nil, false
	}
	p := l.exprOf(c.Arguments.Nodes[0])
	if p == nil {
		return nil, true
	}
	return &ir.Call{Callee: adopt, Args: []ir.Expr{p}, Typ: ir.Void, Span: l.span(n)}, true
}
