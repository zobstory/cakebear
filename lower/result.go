package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// funcResult sets fn's result: its annotation's type, or what TypeScript
// infers for a function written without one, and for an async function the
// promise of either. It is the type a caller gets. Inferring matters: a
// function `function f() { return 1; }` once lowered with a void result and
// failed inside go build, "too many return values" (found by M4's
// re-measurement of wx-next).
func (l *lowerer) funcResult(n *ast.Node, fn *ir.Func) {
	decl := n.AsFunctionDeclaration()
	fn.Result = ir.Void
	if decl.Type != nil {
		fn.Result = l.typeOf(decl.Type)
		if fn.Result == ir.Invalid {
			l.failf(decl.Type, "function %s returns a type the backend cannot represent yet%s", fn.Name, l.reason())
		}
	}
	if fn.Async = isAsync(n); fn.Async {
		l.asyncResult(n, fn)
		return
	}
	if decl.Type == nil {
		sigs := l.checker.GetSignaturesOfType(l.checker.GetTypeAtLocation(decl.Name()), checker.SignatureKindCall)
		if len(sigs) != 1 {
			return
		}
		if fn.Result = l.typeFromType(l.checker.GetReturnTypeOfSignature(sigs[0])); fn.Result == ir.Invalid {
			l.failf(n, "function %s returns a type the backend cannot represent yet%s; annotating its result may help", fn.Name, l.reason())
		}
	}
}

// widened is v as a number, where v is one of cakebear's numeric types in a
// number's slot. TypeScript lets an i32 fill a number's slot, a parameter,
// a variable or a result, since its type is number with a brand; Go needs the
// conversion, which loses nothing for i32, u32 and f32, and for i64 and u64
// what a JavaScript number would lose past 2^53. It once reached go build
// unconverted and failed there.
func (l *lowerer) widened(n *ast.Node, v ir.Expr) ir.Expr {
	if t := v.ExprType(); !t.IsExtension() || t == ir.Base64 || l.slotType(n) != ir.Number {
		return v
	}
	return &ir.Convert{Value: v, Typ: ir.Number, Span: v.ExprSpan()}
}
