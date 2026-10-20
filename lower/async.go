package lower

import (
	"path"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/bundled"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// Lowering async code (M2 plan, D2 and D3): async functions and arrows,
// `await`, `new Promise` with a resolve function, and the DOM's setTimeout and
// clearTimeout.
//
// Promise is TypeScript's own type, recognised by its declarations: every one
// must be in a bundled lib. The interface is spread across five of them, and a
// program that augments it has added members with nothing behind them.
//
// What M2 leaves out is refused by name: reject and errors (M3), adopting one
// promise into another, .then and the other promise methods, top-level await,
// and a timer callback that is anything but `() => void`.

// firstTopLevelAwait returns file's first await outside any function, or nil.
// An ES module's top-level code may await, and then runs as an async body (M2
// step 7). TypeScript allows it only in a module, a file with an import or an
// export, which is why an empty `export {}` is accepted (exportDecl).
func firstTopLevelAwait(file *ast.SourceFile) *ast.Node {
	var found *ast.Node
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		switch {
		case found != nil:
			return true
		case n.Kind == ast.KindAwaitExpression:
			found = n
			return true
		case ast.IsFunctionLike(n) || n.Kind == ast.KindClassDeclaration || n.Kind == ast.KindClassExpression:
			return false // its own scope: an await there is the function's
		}
		n.ForEachChild(visit)
		return false
	}
	file.AsNode().ForEachChild(visit)
	return found
}

// isAsync reports whether a function declaration or literal is async.
func isAsync(n *ast.Node) bool { return ast.GetFunctionFlags(n)&ast.FunctionFlagsAsync != 0 }

// inBody runs f with l.async set for a function body, restoring it after, so
// the body of a plain function nested in an async one is not async.
func (l *lowerer) inBody(async bool, f func()) {
	saved := l.async
	l.async = async
	defer func() { l.async = saved }()
	f()
}

// bodyResult is what a return statement in a function body carries: the
// function's result, or for an async function the promise's element.
func bodyResult(result ir.Type, async bool) ir.Type {
	if async && result.Kind == ir.KindPromise && result.Elem != nil {
		return *result.Elem
	}
	return result
}

func inBundledLib(decl *ast.Node) bool {
	f := ast.GetSourceFileOfNode(decl)
	return f != nil && bundled.IsBundled(string(f.FileName()))
}

// onlyInBundledLib reports whether every declaration of sym is in one of
// TypeScript's bundled libs, and, when file is not "", in that one.
func onlyInBundledLib(sym *ast.Symbol, file string) bool {
	if sym == nil || len(sym.Declarations()) == 0 {
		return false
	}
	for _, d := range sym.Declarations() {
		if !inBundledLib(d) || (file != "" && path.Base(string(ast.GetSourceFileOfNode(d).FileName())) != file) {
			return false
		}
	}
	return true
}

// promiseType maps TypeScript's Promise<T> onto the IR. ok reports whether t
// is that Promise at all; the type is ir.Invalid when T is not representable.
func (l *lowerer) promiseType(t *checker.Type) (ir.Type, bool) {
	sym := t.Symbol()
	if sym == nil || sym.Name() != "Promise" || sym.Flags()&ast.SymbolFlagsInterface == 0 || !onlyInBundledLib(sym, "") {
		return ir.Invalid, false
	}
	elem := l.typeFromType(l.checker.GetPromisedTypeOfPromise(t))
	if elem != ir.Void && !storable(elem) {
		return ir.Invalid, true
	}
	return ir.PromiseOf(elem), true
}

// asyncResult settles an async function's result. Without an annotation it is
// the promise the checker inferred.
func (l *lowerer) asyncResult(n *ast.Node, fn *ir.Func) {
	decl := n.AsFunctionDeclaration()
	if decl.Type == nil {
		sigs := l.checker.GetSignaturesOfType(l.checker.GetTypeAtLocation(decl.Name()), checker.SignatureKindCall)
		if len(sigs) == 1 {
			fn.Result = l.typeFromType(l.checker.GetReturnTypeOfSignature(sigs[0]))
		}
	}
	switch {
	case fn.Result == ir.Invalid && decl.Type == nil:
		// Inferred, as Promise<null> is from `return null`: refused here, as
		// a function's is (result.go), or nothing would.
		l.failf(n, "async function %s returns a type the backend cannot represent yet%s; annotating its result may help", fn.Name, l.reason())
	case fn.Result.Kind != ir.KindPromise && fn.Result != ir.Invalid:
		l.failf(n, "async function %s returns a type the backend cannot represent yet", fn.Name)
	}
}

// returnable refuses `return p` in an async function when p is itself a
// promise: the function's promise would adopt p's state, which M2 does not
// model. Awaiting it first is the supported spelling.
func (l *lowerer) returnable(n *ast.Node, v ir.Expr) bool {
	if l.async && !l.discard && v.ExprType().Kind == ir.KindPromise {
		l.failf(n, "an async function returning a promise is not supported yet; return await it instead")
		return false
	}
	return true
}

func (l *lowerer) await(n *ast.Node) ir.Expr {
	if !l.async {
		// TypeScript rejects this first; lowering is the backstop.
		l.failf(n, "await is allowed only in an async function or at the top level of a module")
		return nil
	}
	value := l.expr(n.AsAwaitExpression().Expression)
	if value == nil {
		return nil
	}
	t := value.ExprType()
	if t.Kind != ir.KindPromise || t.Elem == nil {
		l.failf(n, "await needs a promise; awaiting a %s is not supported yet", t)
		return nil
	}
	return &ir.Await{Value: value, Typ: *t.Elem, Span: l.span(n)}
}

// newExpr lowers `new Promise((resolve) => …)`, the only constructor so far.
func (l *lowerer) newExpr(n *ast.Node) ir.Expr {
	ne := n.AsNewExpression()
	callee := ne.Expression
	if callee.Kind != ast.KindIdentifier || callee.Text() != "Promise" ||
		!onlyInBundledLib(l.checker.GetSymbolAtLocation(callee), "") {
		if callee.Kind == ast.KindIdentifier {
			l.failf(n, "new %s is not supported yet", callee.Text())
		} else {
			l.failf(n, "new expression is not supported yet")
		}
		return nil
	}
	typ := l.typeOf(n)
	if typ.Kind != ir.KindPromise || typ.Elem == nil {
		l.failf(n, "this promise holds a type the backend cannot represent yet")
		return nil
	}
	var args []*ast.Node
	if ne.Arguments != nil {
		args = ne.Arguments.Nodes
	}
	if len(args) != 1 {
		l.failf(n, "new Promise takes 1 argument, its executor")
		return nil
	}
	exec := args[0]
	if exec.Kind != ast.KindArrowFunction && exec.Kind != ast.KindFunctionExpression {
		l.failf(exec, "new Promise needs its executor written inline, as (resolve) => { … }")
		return nil
	}
	if isAsync(exec) {
		l.failf(exec, "an async executor is not supported; the executor runs at once, so await inside an async function instead")
		return nil
	}
	params := exec.Parameters()
	if len(params) > 1 {
		l.failf(params[1], "reject is not supported yet; cakebear has no errors until M3")
		return nil
	}
	if !l.closureShapeOK(exec) {
		return nil
	}

	// resolve is typed `(value: T | PromiseLike<T>) => void`. The union is
	// there for adopting another promise, which resolvesWithPromise refuses,
	// so what is left is a plain function (ir.ResolveType).
	elem := *typ.Elem
	if len(params) == 1 {
		if sym := l.checker.GetSymbolAtLocation(params[0].Name()); sym != nil {
			if l.resolvers == nil {
				l.resolvers = map[*ast.Symbol]ir.Type{}
			}
			l.resolvers[sym] = ir.ResolveType(elem)
		}
	}
	executor := l.closureFor(exec, ir.ExecutorType(elem).Sig, nil, true)
	if executor == nil {
		return nil
	}
	return &ir.NewPromise{Executor: executor, Typ: typ, Span: l.span(n)}
}

// resolverType is the type of a reference to an executor's resolve.
func (l *lowerer) resolverType(n *ast.Node) (ir.Type, bool) {
	if len(l.resolvers) == 0 || n.Kind != ast.KindIdentifier {
		return ir.Invalid, false
	}
	t, ok := l.resolvers[l.checker.GetSymbolAtLocation(n)]
	return t, ok
}

// resolvesWithPromise refuses resolve(p) for a promise p, which would adopt
// p's state rather than resolve with a value.
func (l *lowerer) resolvesWithPromise(n *ast.Node, call *ir.Call) bool {
	if _, ok := l.resolverType(n.AsCallExpression().Expression); !ok {
		return false
	}
	for _, a := range call.Args {
		if a.ExprType().Kind == ir.KindPromise {
			l.failf(n, "resolving a promise with another promise is not supported yet; await it, then resolve with its value")
			return true
		}
	}
	return false
}

// voidCallback is the only callback type setTimeout accepts here.
var voidCallback = ir.FuncType(nil, ir.Void)

// timerCall lowers setTimeout and clearTimeout. They are the DOM's, recognised
// by being declared only in lib.dom.d.ts, so a program's own function of the
// same name is an ordinary call (D3).
func (l *lowerer) timerCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	callee := c.Expression
	if callee.Kind != ast.KindIdentifier || (callee.Text() != "setTimeout" && callee.Text() != "clearTimeout") ||
		!onlyInBundledLib(l.checker.GetSymbolAtLocation(callee), "lib.dom.d.ts") {
		return nil, false
	}
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}

	if callee.Text() == "clearTimeout" {
		if len(args) != 1 {
			l.failf(n, "clearTimeout takes 1 argument, got %d", len(args))
			return nil, true
		}
		id := l.expr(args[0])
		if id == nil {
			return nil, true
		}
		if id.ExprType() != ir.Number {
			l.failf(args[0], "clearTimeout takes the number setTimeout returned, not a %s", id.ExprType())
			return nil, true
		}
		return &ir.HostCall{Op: ir.HostClearTimeout, Args: []ir.Expr{id}, Typ: ir.Void, Span: l.span(n)}, true
	}

	switch {
	case len(args) < 2:
		l.failf(n, "setTimeout without a delay is not supported yet; pass one, such as 0")
		return nil, true
	case len(args) > 2:
		l.failf(args[2], "setTimeout's extra arguments are not supported yet; pass values to the callback through a closure")
		return nil, true
	}
	cb := l.timerCallback(args[0])
	delay := l.expr(args[1])
	if cb == nil || delay == nil {
		return nil, true
	}
	if delay.ExprType() != ir.Number {
		l.failf(args[1], "setTimeout's delay must be a number, not a %s", delay.ExprType())
		return nil, true
	}
	return &ir.HostCall{Op: ir.HostSetTimeout, Args: []ir.Expr{cb, delay}, Typ: ir.Number, Span: l.span(n)}, true
}

// timerCallback lowers setTimeout's callback, which must be a function taking
// nothing. A literal is emitted as `() => void` whatever it returns, as a
// timer drops the result. A function value must already be one, which is
// what rules out resolve for anything but Promise<void>: a timer would call
// it with no value.
func (l *lowerer) timerCallback(arg *ast.Node) ir.Expr {
	if l.checker.GetTypeAtLocation(arg).Flags()&checker.TypeFlagsStringLike != 0 {
		l.failf(arg, "a string handler for setTimeout is not supported; pass a function")
		return nil
	}
	if arg.Kind == ast.KindArrowFunction || arg.Kind == ast.KindFunctionExpression {
		if !l.closureShapeOK(arg) {
			return nil
		}
		own := l.typeOf(arg)
		if own.Kind != ir.KindFunc {
			l.failClosureType(arg)
			return nil
		}
		return l.closureFor(arg, own.Sig, voidCallback.Sig, false)
	}
	cb := l.expr(arg)
	if cb == nil {
		return nil
	}
	if !cb.ExprType().Equal(voidCallback) {
		l.failf(arg, "setTimeout's callback must take no arguments, and this one is %s; wrap the call in an arrow function", cb.ExprType())
		return nil
	}
	return cb
}

// promiseMemberCall refuses the promise methods and Promise's statics by
// name, rather than as "only calls to a function by name". It reports whether
// it refused.
func (l *lowerer) promiseMemberCall(n *ast.Node, c *ast.CallExpression) bool {
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	recv, name := c.Expression.Expression(), c.Expression.Name().Text()
	if recv.Kind == ast.KindIdentifier && recv.Text() == "Promise" && onlyInBundledLib(l.checker.GetSymbolAtLocation(recv), "") {
		l.failf(n, "Promise.%s is not supported yet", name)
		return true
	}
	if l.typeOf(recv).Kind == ir.KindPromise {
		l.failf(n, "promise methods such as .%s are not supported yet; use await", name)
		return true
	}
	return false
}
