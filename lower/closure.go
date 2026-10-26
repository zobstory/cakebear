package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// maxFuncTypeDepth bounds how deeply function types may nest. A recursive type
// such as `type F = (next: F) => void` would otherwise recurse forever, and no
// real signature comes close to this.
const maxFuncTypeDepth = 16

// funcType maps a checked function type onto the IR, or returns ir.Invalid.
//
// Only a plain function type is representable: exactly one call signature, no
// construct signatures, no properties, no type parameters, no `this`, and no
// rest parameters. Anything callable with more to it (an overloaded function,
// a callable object, a generic) has no single Go func type. A parameter a
// caller may leave out, optional or with a default, is a maybe (params.go).
func (l *lowerer) funcType(t *checker.Type) ir.Type {
	if t.Flags()&checker.TypeFlagsObject == 0 || l.funcDepth >= maxFuncTypeDepth {
		return ir.Invalid
	}
	l.funcDepth++
	defer func() { l.funcDepth-- }()

	sigs := l.checker.GetSignaturesOfType(t, checker.SignatureKindCall)
	if len(sigs) != 1 ||
		len(l.checker.GetSignaturesOfType(t, checker.SignatureKindConstruct)) != 0 ||
		len(l.checker.GetPropertiesOfType(t)) != 0 {
		return ir.Invalid
	}
	sig := sigs[0]
	params := sig.Parameters()
	if len(sig.TypeParameters()) != 0 || sig.ThisParameter() != nil || sig.HasRestParameter() {
		return ir.Invalid
	}

	paramTypes := make([]ir.Type, 0, len(params))
	for i, p := range params {
		pt := l.typeFromType(l.checker.GetTypeOfSymbol(p))
		if !storable(pt) {
			return ir.Invalid
		}
		if i >= sig.MinArgumentCount() {
			pt = ir.OrUndefined(pt)
		}
		paramTypes = append(paramTypes, pt)
	}
	result := l.typeFromType(l.checker.GetReturnTypeOfSignature(sig))
	if result != ir.Void && !storable(result) {
		return ir.Invalid
	}
	return ir.FuncType(paramTypes, result)
}

// storable reports whether a value of type t can be held in a Go variable.
// null and undefined alone have no representation, only a maybe's states,
// and void is only a result.
func storable(t ir.Type) bool {
	switch t {
	case ir.Invalid, ir.Void, ir.Null, ir.Undefined:
		return false
	}
	return true
}

// closure lowers an arrow function or an anonymous function expression.
func (l *lowerer) closure(n *ast.Node) ir.Expr {
	if !l.closureShapeOK(n) {
		return nil
	}

	own := l.typeOf(n)
	if own.Kind != ir.KindFunc {
		l.failClosureType(n)
		return nil
	}

	// Prefer the signature the surrounding code expects. TypeScript accepts a
	// literal whose own signature merely fits the slot; Go needs it identical.
	var slot *ir.Signature
	if ctx := l.checker.GetContextualType(n, checker.ContextFlagsNone); ctx != nil {
		if s := l.typeFromType(ctx); s.Kind == ir.KindFunc {
			slot = s.Sig
		}
	}
	return l.closureFor(n, own.Sig, slot, false)
}

// closureFor lowers a function literal whose own signature is own, emitted
// with slot's when there is one. drop discards whatever the body returns, for
// a literal whose result nothing reads, such as a Promise executor.
func (l *lowerer) closureFor(n *ast.Node, own, slot *ir.Signature, drop bool) ir.Expr {
	sig, discard := own, drop
	if slot != nil {
		if !l.fitsSlot(n, own, slot) {
			return nil
		}
		if ctx := l.checker.GetContextualType(n, checker.ContextFlagsNone); ctx != nil {
			src := l.checker.GetTypeAtLocation(n)
			if why := l.readonlyFlow(src, ctx, nil); why != "" {
				l.failReadonlyFlow(n, src, ctx, why)
				return nil
			}
		}
		sig = slot
		discard = discard || (slot.Result == ir.Void && own.Result != ir.Void)
	}

	async := isAsync(n)
	lit := &ir.FuncLit{Result: sig.Result, Async: async, Typ: ir.FuncType(sig.Params, sig.Result), Span: l.span(n)}
	declared := n.Parameters()
	var prologues []func() []ir.Stmt // defaults and patterns (params.go)
	for i, pt := range sig.Params {
		param := ir.Param{Type: pt, Span: lit.Span}
		if i < len(declared) {
			name, open, ok := l.param(declared[i], pt)
			if !ok {
				return nil
			}
			param.Name = name
			param.Span = l.span(declared[i])
			param.Appended = open == nil && declared[i].Name().Kind == ast.KindIdentifier && l.appended(declared[i].Name())
			if open != nil {
				prologues = append(prologues, open)
			}
		}
		lit.Params = append(lit.Params, param)
	}
	boxedOK := true
	l.inBody(async, func() {
		lit.Body = append(opening(prologues), l.closureBody(n.Body(), bodyResult(sig.Result, async), discard)...)
		lit.Body = undefinedReturns(lit.Body, returnedType(sig.Result, async), lit.Span)
		if returnedType(sig.Result, async) == ir.Unknown && returnedType(own.Result, async) != ir.Unknown {
			// It returns its own type, `(n): number => …`, which the
			// slot's unknown takes boxed.
			eachReturn(lit.Body, func(r *ir.Return) {
				if r.Value = l.box(n, r.Value); r.Value == nil {
					boxedOK = false
				}
			})
		}
	})
	if !boxedOK {
		return nil
	}
	lit.Captures = l.captures(n)
	return lit
}

// closureShapeOK refuses the forms of function literal the backend cannot
// represent yet, each with its own diagnostic.
func (l *lowerer) closureShapeOK(n *ast.Node) bool {
	if ast.GetFunctionFlags(n)&ast.FunctionFlagsGenerator != 0 {
		l.failf(n, "generator functions are not supported yet")
		return false
	}
	if n.Kind == ast.KindFunctionExpression && n.Name() != nil {
		l.failf(n, "named function expressions are not supported yet")
		return false
	}
	if len(n.TypeParameters()) != 0 {
		l.failf(n, "generic functions are not supported yet")
		return false
	}
	ok := true
	for _, p := range n.Parameters() {
		param := p.AsParameterDeclaration()
		switch {
		case param.Name() == nil:
			l.invalidf(p, "a parameter with no name")
			ok = false
		case param.Name().Kind == ast.KindArrayBindingPattern:
			l.failf(p, "array destructuring is not supported yet")
			ok = false
		case param.Name().Kind == ast.KindIdentifier && param.Name().Text() == "this":
			l.failf(p, "a this parameter is not supported yet")
			ok = false
		case param.DotDotDotToken != nil:
			l.failf(p, "rest parameters are not supported yet")
			ok = false
		}
	}
	return ok
}

// failClosureType says which part of a literal's type is unrepresentable.
func (l *lowerer) failClosureType(n *ast.Node) {
	for _, p := range n.Parameters() {
		if !storable(l.typeOf(p)) {
			l.failf(p, "parameter %s has a type the backend cannot represent yet%s", bindingText(p.Name()), l.reason())
			return
		}
	}
	l.failf(n, "this function returns a type the backend cannot represent yet")
}

// fitsSlot checks that a literal can be emitted with the slot's signature.
//
// Fewer declared parameters is fine (the rest are ignored), and so is a value
// returned where the slot expects void (it is discarded). A parameter whose
// declared type differs from what the caller passes is not: TypeScript accepts
// `(x: number) => …` where `(x: i32) => …` is expected, but the body would then
// treat an int32 as a float64.
func (l *lowerer) fitsSlot(n *ast.Node, own, slot *ir.Signature) bool {
	declared := n.Parameters()
	if len(own.Params) > len(slot.Params) {
		l.failf(n, "this function takes %d parameters where %d are passed", len(own.Params), len(slot.Params))
		return false
	}
	for i, pt := range own.Params {
		if !pt.Equal(slot.Params[i]) {
			l.failf(declared[i], "parameter %s is %s here but the caller passes %s; converting between them is not supported yet",
				bindingText(declared[i].Name()), pt, slot.Params[i])
			return false
		}
	}
	// An unknown takes whatever it returns, boxed (errors.go), as void takes
	// it dropped; one ending with nothing to return has no undefined yet.
	if slot.Result == ir.Unknown && own.Result == ir.Void {
		l.failf(n, "this function returns nothing where unknown is expected, which is not supported yet; return a value")
		return false
	}
	if slot.Result != ir.Void && slot.Result != ir.Unknown && !own.Result.Equal(slot.Result) {
		l.failf(n, "this function returns %s where %s is expected; converting between them is not supported yet",
			own.Result, slot.Result)
		return false
	}
	return true
}

// closureBody lowers a block body, or a concise one such as `x => x + 1`.
// result is what a return statement in it carries, and discard drops returned
// values, for a literal standing in for a void slot.
func (l *lowerer) closureBody(body *ast.Node, result ir.Type, discard bool) []ir.Stmt {
	saved := l.discard
	l.discard = discard
	defer func() { l.discard = saved }()

	if body.Kind == ast.KindBlock {
		return l.block(body)
	}
	value := l.expr(body)
	if value == nil {
		return nil
	}
	if result == ir.Void || l.never(body, value) {
		return []ir.Stmt{&ir.ExprStmt{Expr: value, Span: value.ExprSpan()}}
	}
	return []ir.Stmt{&ir.Return{Value: value, Span: value.ExprSpan()}}
}

// captures lists the variables and parameters declared outside fn that its
// body refers to, in first-use order.
//
// Each identifier is resolved to its symbol, so shadowing is handled by the
// checker rather than by matching names. Top-level functions are not captures:
// they are package-level in the emitted Go.
func (l *lowerer) captures(fn *ast.Node) []ir.Capture {
	var out []ir.Capture
	seen := map[*ast.Symbol]bool{}
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n.Kind == ast.KindIdentifier && !isMemberName(n) {
			if sym := l.valueSymbol(n); sym != nil && !seen[sym] {
				if c, ok := l.capture(fn, n, sym); ok {
					seen[sym] = true
					out = append(out, c)
				}
			}
		}
		n.ForEachChild(visit)
		return false
	}
	visit(fn.Body())
	return out
}

func (l *lowerer) capture(fn, id *ast.Node, sym *ast.Symbol) (ir.Capture, bool) {
	decl := sym.ValueDeclaration()
	if sym.Flags()&ast.SymbolFlagsVariable == 0 || decl == nil || ast.GetSourceFileOfNode(decl) != l.file {
		return ir.Capture{}, false
	}
	if within(decl, fn) {
		return ir.Capture{}, false // declared inside the literal
	}
	return ir.Capture{Name: id.Text(), Module: topLevelModule(sym), Type: l.typeOf(id), Mutable: !isConstDecl(decl)}, true
}

// valueSymbol is the symbol of what the identifier n reads. That is the one
// GetSymbolAtLocation gives, but for the name of a shorthand property, `{ x }`,
// which it resolves to the property: the value read is the variable x.
func (l *lowerer) valueSymbol(n *ast.Node) *ast.Symbol {
	if p := n.Parent; p != nil && p.Kind == ast.KindShorthandPropertyAssignment && p.Name() == n {
		return l.checker.GetShorthandAssignmentValueSymbol(p)
	}
	return l.checker.GetSymbolAtLocation(n)
}

// isMemberName reports whether id is the `name` in `x.name`, which names a
// member rather than referring to a binding.
func isMemberName(id *ast.Node) bool {
	p := id.Parent
	return p != nil && p.Kind == ast.KindPropertyAccessExpression && p.Name() == id
}
