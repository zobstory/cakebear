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
// optional or rest parameters. Anything callable with more to it (an overloaded
// function, a callable object, a generic) has no single Go func type.
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
	if len(sig.TypeParameters()) != 0 || sig.ThisParameter() != nil ||
		sig.HasRestParameter() || sig.MinArgumentCount() != len(params) {
		return ir.Invalid
	}

	paramTypes := make([]ir.Type, 0, len(params))
	for _, p := range params {
		pt := l.typeFromType(l.checker.GetTypeOfSymbol(p))
		if !storable(pt) {
			return ir.Invalid
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
// null and undefined have no representation yet, and void is only a result.
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
	sig, discard := own.Sig, false
	if ctx := l.checker.GetContextualType(n, checker.ContextFlagsNone); ctx != nil {
		if slot := l.typeFromType(ctx); slot.Kind == ir.KindFunc {
			if !l.fitsSlot(n, own.Sig, slot.Sig) {
				return nil
			}
			sig = slot.Sig
			discard = slot.Sig.Result == ir.Void && own.Sig.Result != ir.Void
		}
	}

	lit := &ir.FuncLit{Result: sig.Result, Typ: ir.FuncType(sig.Params, sig.Result), Span: l.span(n)}
	declared := n.Parameters()
	for i, pt := range sig.Params {
		param := ir.Param{Type: pt, Span: lit.Span}
		if i < len(declared) {
			param.Name = declared[i].Name().Text()
			param.Span = l.span(declared[i])
		}
		lit.Params = append(lit.Params, param)
	}
	lit.Body = l.closureBody(n.Body(), sig.Result, discard)
	lit.Captures = l.captures(n)
	return lit
}

// closureShapeOK refuses the forms of function literal the backend cannot
// represent yet, each with its own diagnostic.
func (l *lowerer) closureShapeOK(n *ast.Node) bool {
	switch flags := ast.GetFunctionFlags(n); {
	case flags&ast.FunctionFlagsAsync != 0:
		l.fail(n, "async functions are not supported yet")
		return false
	case flags&ast.FunctionFlagsGenerator != 0:
		l.fail(n, "generator functions are not supported yet")
		return false
	}
	if n.Kind == ast.KindFunctionExpression && n.Name() != nil {
		l.fail(n, "named function expressions are not supported yet")
		return false
	}
	if len(n.TypeParameters()) != 0 {
		l.fail(n, "generic functions are not supported yet")
		return false
	}
	ok := true
	for _, p := range n.Parameters() {
		param := p.AsParameterDeclaration()
		switch {
		case param.Name() == nil || param.Name().Kind != ast.KindIdentifier:
			l.fail(p, "destructured parameters are not supported yet")
			ok = false
		case param.Name().Text() == "this":
			l.fail(p, "a this parameter is not supported yet")
			ok = false
		case param.DotDotDotToken != nil:
			l.fail(p, "rest parameters are not supported yet")
			ok = false
		case param.QuestionToken != nil || param.Initializer != nil:
			l.fail(p, "optional and default parameters are not supported yet")
			ok = false
		}
	}
	return ok
}

// failClosureType says which part of a literal's type is unrepresentable.
func (l *lowerer) failClosureType(n *ast.Node) {
	for _, p := range n.Parameters() {
		if !storable(l.typeOf(p)) {
			l.fail(p, "parameter %s has a type the backend cannot represent yet", p.Name().Text())
			return
		}
	}
	l.fail(n, "this function returns a type the backend cannot represent yet")
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
		l.fail(n, "this function takes %d parameters where %d are passed", len(own.Params), len(slot.Params))
		return false
	}
	for i, pt := range own.Params {
		if !pt.Equal(slot.Params[i]) {
			l.fail(declared[i], "parameter %s is %s here but the caller passes %s; converting between them is not supported yet",
				declared[i].Name().Text(), pt, slot.Params[i])
			return false
		}
	}
	if slot.Result != ir.Void && !own.Result.Equal(slot.Result) {
		l.fail(n, "this function returns %s where %s is expected; converting between them is not supported yet",
			own.Result, slot.Result)
		return false
	}
	return true
}

// closureBody lowers a block body, or a concise one such as `x => x + 1`.
// discard drops returned values, for a literal standing in for a void slot.
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
	if result == ir.Void {
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
			if sym := l.checker.GetSymbolAtLocation(n); sym != nil && !seen[sym] {
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
	decl := sym.ValueDeclaration
	if sym.Flags&ast.SymbolFlagsVariable == 0 || decl == nil || ast.GetSourceFileOfNode(decl) != l.file {
		return ir.Capture{}, false
	}
	if decl.Pos() >= fn.Pos() && decl.End() <= fn.End() {
		return ir.Capture{}, false // declared inside the literal
	}
	mutable := true
	if decl.Kind == ast.KindVariableDeclaration && decl.Parent != nil && decl.Parent.Flags&ast.NodeFlagsConst != 0 {
		mutable = false
	}
	return ir.Capture{Name: id.Text(), Type: l.typeOf(id), Mutable: mutable}, true
}

// isMemberName reports whether id is the `name` in `x.name`, which names a
// member rather than referring to a binding.
func isMemberName(id *ast.Node) bool {
	p := id.Parent
	return p != nil && p.Kind == ast.KindPropertyAccessExpression && p.Name() == id
}
