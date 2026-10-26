package lower

import (
	"slices"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// Errors (M6 plan D1-D5): throw, try/catch/finally, Error and its built-in
// kinds, and unknown, which is what a catch binds. Error and its kinds are
// recognised by their declarations, in TypeScript's libs and nowhere else, as
// Promise is: a program that declares an Error of its own has an ordinary
// binding, and one that adds members to the libs' gets a refusal.

// errorKindOf is the built-in Error kind sym names, as a type or as its
// constructor, which TypeScript's libs declare as one symbol.
func errorKindOf(sym *ast.Symbol) (ir.ErrorKind, bool) {
	if sym == nil {
		return 0, false
	}
	k, ok := ir.ErrorKindNamed(sym.Name())
	return k, ok && onlyInBundledLib(sym, "")
}

// isErrorType reports whether t is the type of one of the built-in kinds:
// Error, TypeError and the rest are one IR type (D4). The libs merge each
// kind's interface and constructor into one symbol, the interface's type's.
func isErrorType(t *checker.Type) bool {
	_, ok := errorKindOf(t.Symbol())
	return ok
}

// anyOrUnknown maps `unknown` and `any`, which flags is one of, onto the IR:
// any is refused, since it allows anything without a check (D5).
func (l *lowerer) anyOrUnknown(flags checker.TypeFlags) ir.Type {
	if flags&checker.TypeFlagsUnknown != 0 {
		return ir.Unknown
	}
	l.because("any is not supported yet: it allows anything without a check; unknown, which needs one, is")
	return ir.Invalid
}

// errorStmt lowers a throw or a try.
func (l *lowerer) errorStmt(n *ast.Node) []ir.Stmt {
	if n.Kind == ast.KindThrowStatement {
		return l.throwStmt(n)
	}
	return l.tryStmt(n)
}

// errorBinary lowers `v instanceof K` and `typeof v === "tag"`, ok false for
// any other binary expression. Both come before their operands are lowered:
// Error is a library global, which an operand of its own would refuse, and a
// typeof is no value yet.
func (l *lowerer) errorBinary(n *ast.Node) (ir.Expr, bool) {
	if n.AsBinaryExpression().OperatorToken.Kind == ast.KindInstanceOfKeyword {
		return l.instanceOf(n), true
	}
	return l.typeOfTest(n)
}

// throwStmt lowers `throw v`. A catch binds what was thrown as an unknown,
// which prints and serialises as it is at run time, so a throw carries only
// data: a function, a promise or a host value, or anything holding one, is
// refused, as ir.Box refuses it. An extension type is thrown as the number
// it is.
func (l *lowerer) throwStmt(n *ast.Node) []ir.Stmt {
	v := l.expr(n.AsThrowStatement().Expression)
	if v == nil {
		return nil
	}
	v = asNumber(v)
	if t := v.ExprType(); t == ir.Base64 {
		l.failf(n, "throwing a base64 is not supported yet; throw a string")
		return nil
	} else if what := ir.Unserializable(t, nil); what != "" {
		l.failf(n, "throwing %s is not supported: what it holds, %s, couldn't be printed where it is caught", aType(t), what)
		return nil
	}
	return []ir.Stmt{&ir.Throw{Value: v, Span: l.span(n)}}
}

// tryStmt lowers try/catch/finally (D2). Each block is lowered as a block of
// its own; the backend runs the try in a function literal, and emits the
// catch and the finally in place.
func (l *lowerer) tryStmt(n *ast.Node) []ir.Stmt {
	s := n.AsTryStatement()
	out := &ir.Try{Span: l.span(n)}
	if s.CatchClause != nil {
		c, ok := l.catchClause(s.CatchClause)
		if !ok {
			return nil
		}
		out.Catch = c
	}
	out.Body = l.block(s.TryBlock)
	if out.Catch != nil {
		out.Catch.Body = l.block(s.CatchClause.AsCatchClause().Block)
	}
	if s.FinallyBlock != nil {
		out.Finally = append([]ir.Stmt{}, l.block(s.FinallyBlock)...) // non-nil, even empty
	}
	return []ir.Stmt{out}
}

// catchClause is a catch clause's binding, which is an unknown (D5): its
// body is lowered by the caller. `catch {}` binds nothing.
func (l *lowerer) catchClause(n *ast.Node) (*ir.Catch, bool) {
	out := &ir.Catch{Span: l.span(n)}
	decl := n.AsCatchClause().VariableDeclaration
	if decl == nil {
		return out, true
	}
	name := decl.Name()
	switch {
	case name.Kind != ast.KindIdentifier:
		l.failf(name, "destructuring what a catch binds is not supported yet; bind it to a name, and read what it holds after a check")
		return nil, false
	case l.typeOf(name) != ir.Unknown:
		l.failf(name, "a catch binding of type %s is not supported; leave it unannotated, or annotate it unknown, as strict TypeScript types it",
			l.checker.TypeToString(l.checker.GetTypeAtLocation(name)))
		return nil, false
	}
	out.Name = name.Text()
	return out, true
}

// newError lowers `new K(message)`, or `K(message)`, which is the same, for K
// one of the built-in Error kinds (D4). TypeScript's libs type the message
// `string | undefined`; none, or undefined, leaves the error with no message
// of its own. Options, which carry a cause, are refused: no program measured
// passes one (M6 plan §2a).
func (l *lowerer) newError(n *ast.Node, k ir.ErrorKind, args []*ast.Node) ir.Expr {
	out := &ir.NewError{Kind: k, Span: l.span(n)}
	if len(args) > 1 {
		l.failf(args[1], "%s's options, which carry a cause, are not supported yet", k)
		return nil
	}
	if len(args) == 0 {
		return out
	}
	m := l.expr(args[0])
	if m == nil {
		return nil
	}
	switch t := m.ExprType(); t {
	case ir.Undefined:
	case ir.String:
		out.Message = m
	default:
		// A maybe: undefined would leave no message, where ToString writes
		// "undefined", and the IR holds a message or none, not a choice.
		l.failf(args[0], "a message of type %s is not supported yet; give it a string", t)
		return nil
	}
	return out
}

// errorCall lowers `Error(message)`, a built-in kind called without new, and
// `e.toString()`, which is `Name: message`, as String(e) is.
func (l *lowerer) errorCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	if c.Expression.Kind == ast.KindPropertyAccessExpression {
		access := c.Expression.AsPropertyAccessExpression()
		if access.Name().Text() != "toString" || !isErrorType(l.checker.GetTypeAtLocation(access.Expression)) ||
			!onlyInBundledLib(l.checker.GetSymbolAtLocation(access.Name()), "") || len(n.Arguments()) > 0 {
			return nil, false
		}
		e := l.expr(access.Expression)
		if e == nil {
			return nil, true
		}
		return &ir.BuiltinCall{Fn: ir.ToStringMethod, Args: []ir.Expr{e}, Typ: ir.String, Span: l.calleeCaret(l.span(n), n)}, true
	}
	if c.Expression.Kind != ast.KindIdentifier {
		return nil, false
	}
	k, ok := errorKindOf(l.checker.GetSymbolAtLocation(c.Expression))
	if !ok {
		return nil, false
	}
	return l.newError(n, k, n.Arguments()), true
}

// errorProps are the properties of an Error a program may read (D4), each
// declared by TypeScript's libs.
var errorProps = map[string]ir.ErrorProp{
	"message": ir.ErrorMessage,
	"name":    ir.ErrorName,
	"stack":   ir.ErrorStack,
	"cause":   ir.ErrorCause,
}

// errorProperty lowers a read of an Error's property: `e.message`.
func (l *lowerer) errorProperty(n *ast.Node) (ir.Expr, bool) {
	access := n.AsPropertyAccessExpression()
	if !isErrorType(l.checker.GetTypeAtLocation(access.Expression)) {
		return nil, false
	}
	prop, ok := errorProps[access.Name().Text()]
	if !ok || !onlyInBundledLib(l.checker.GetSymbolAtLocation(access.Name()), "") {
		l.failf(n, "an Error's %s is not supported yet", access.Name().Text())
		return nil, true
	}
	e := l.expr(access.Expression)
	if e == nil {
		return nil, true
	}
	if e.ExprType() != ir.Error {
		l.failf(n, "reading %s from %s is not supported yet", access.Name().Text(), aType(e.ExprType()))
		return nil, true
	}
	return narrowed(&ir.ErrorGet{Error: e, Prop: prop, Span: l.span(n)}, l.typeOf(n), l.span(n)), true
}

// instanceOf lowers `v instanceof K` for K a built-in Error kind, of an
// unknown, an Error or a maybe of one. A program's own classes wait for M12.
func (l *lowerer) instanceOf(n *ast.Node) ir.Expr {
	b := n.AsBinaryExpression()
	k, ok := errorKindOf(l.checker.GetSymbolAtLocation(b.Right))
	if b.Right.Kind != ast.KindIdentifier || !ok {
		l.failf(b.Right, "instanceof is supported only for Error and its built-in kinds yet")
		return nil
	}
	v := l.expr(b.Left)
	if v == nil {
		return nil
	}
	switch t := v.ExprType(); {
	case t == ir.Unknown, t == ir.Error, t.Kind == ir.KindMaybe && *t.Elem == ir.Error:
	default:
		l.failf(n, "instanceof on %s is not supported yet", aType(t))
		return nil
	}
	return &ir.InstanceOf{Value: v, Kind: k, Span: l.span(n)}
}

// typeOfTest lowers `typeof v === "tag"`, and !==, == and != (a typeof is a
// string, so loose equality is strict here), for v an unknown. ok is false
// when n is no such comparison.
func (l *lowerer) typeOfTest(n *ast.Node) (ir.Expr, bool) {
	b := n.AsBinaryExpression()
	op := b.OperatorToken.Kind
	if op != ast.KindEqualsEqualsEqualsToken && op != ast.KindExclamationEqualsEqualsToken &&
		op != ast.KindEqualsEqualsToken && op != ast.KindExclamationEqualsToken {
		return nil, false
	}
	operand, tag := ast.SkipParentheses(b.Left), ast.SkipParentheses(b.Right)
	if tag.Kind == ast.KindTypeOfExpression {
		operand, tag = tag, operand
	}
	if operand.Kind != ast.KindTypeOfExpression {
		return nil, false
	}
	if tag.Kind != ast.KindStringLiteral {
		l.failf(n, "comparing typeof with anything but a string literal is not supported yet")
		return nil, true
	}
	t, ok := ir.TypeTagNamed(tag.Text())
	if !ok {
		l.failf(tag, "typeof never gives %q", tag.Text())
		return nil, true
	}
	v := l.expr(operand.AsTypeOfExpression().Expression)
	if v == nil {
		return nil, true
	}
	if v.ExprType() != ir.Unknown {
		l.failf(operand, "typeof of %s is not supported yet; it is known while compiling", aType(v.ExprType()))
		return nil, true
	}
	var test ir.Expr = &ir.TypeOf{Value: v, Tag: t, Span: l.span(n)}
	if op == ast.KindExclamationEqualsEqualsToken || op == ast.KindExclamationEqualsToken {
		test = &ir.Unary{Op: ir.OpNot, Operand: test, Typ: ir.Boolean, Span: l.span(n)}
	}
	return test, true
}

// boxed holds v to an unknown's slot, when n's is one (D5): a value of any
// other type is boxed, an extension type widened to the number it is first.
// An unknown holds only data, so a function, a promise or a host value, or
// anything holding one, is refused.
func (l *lowerer) boxed(n *ast.Node, v ir.Expr) ir.Expr {
	if v.ExprType() == ir.Unknown || l.slotType(n) != ir.Unknown {
		return v
	}
	return l.box(n, v)
}

// box is v, the value of n, as an unknown holds it, whatever n's slot.
func (l *lowerer) box(n *ast.Node, v ir.Expr) ir.Expr {
	if v.ExprType() == ir.Unknown {
		return v
	}
	v = asNumber(v)
	switch t := v.ExprType(); {
	case t == ir.Base64:
		l.failf(n, "a base64 as an unknown is not supported yet; convert it to a string")
		return nil
	case t == ir.Void:
		l.failf(n, "a void value as an unknown is not supported")
		return nil
	default:
		if what := ir.Unserializable(t, nil); what != "" {
			l.failf(n, "%s as an unknown is not supported: it holds %s, and an unknown is printed as what it holds", aType(t), what)
			return nil
		}
	}
	return &ir.Box{Value: v, Span: v.ExprSpan()}
}

// aType is t's name with "a" or "an" before it, by its first letter, for a
// message: "an unknown", "an Error", "a () => number".
func aType(t ir.Type) string {
	name := t.String()
	if name != "" && strings.ContainsRune("AEIOUaeiou", rune(name[0])) {
		return "an " + name
	}
	return "a " + name
}

// mentionsUnknown reports whether t is an unknown, or a function type taking
// or giving one.
func mentionsUnknown(t ir.Type) bool {
	if t == ir.Unknown {
		return true
	}
	if t.Kind != ir.KindFunc || t.Sig == nil {
		return false
	}
	return mentionsUnknown(t.Sig.Result) || slices.ContainsFunc(t.Sig.Params, mentionsUnknown)
}

// libraryTyped reports whether n's slot is one a library's declaration
// types, where it says unknown for what cakebear has a type of its own: an
// argument to a function the program doesn't declare (Number.isInteger's),
// or what a function literal passed to one returns (`some`'s predicate gives
// an unknown). Lowering gives those the types the call needs
// (array_methods.go, numbers.go), as emptyOutside does for maybes, so such a
// slot is none the IR holds (slotType), and nothing is boxed into it.
func (l *lowerer) libraryTyped(n *ast.Node) bool {
	if l.libraryArgument(n) {
		return true
	}
	p := n.Parent
	for p != nil && p.Kind == ast.KindParenthesizedExpression {
		n, p = p, p.Parent
	}
	var fn *ast.Node
	switch {
	case p == nil:
		return false
	case p.Kind == ast.KindReturnStatement:
		fn = ast.GetContainingFunction(p)
	case p.Kind == ast.KindArrowFunction && p.Body() == n:
		fn = p
	}
	return fn != nil && (fn.Kind == ast.KindArrowFunction || fn.Kind == ast.KindFunctionExpression) && l.libraryArgument(fn)
}

// libraryArgument reports whether n is an argument to a call of a function
// the program doesn't declare.
func (l *lowerer) libraryArgument(n *ast.Node) bool {
	p := n.Parent
	for p != nil && p.Kind == ast.KindParenthesizedExpression {
		n, p = p, p.Parent
	}
	if p == nil || (p.Kind != ast.KindCallExpression && p.Kind != ast.KindNewExpression) || !slices.Contains(p.Arguments(), n) {
		return false
	}
	sig := l.checker.GetResolvedSignature(p)
	return sig != nil && sig.Declaration() != nil && !l.p.inProgram(sig.Declaration())
}

// unknownOperands boxes the other side of `===` or `!==` between an unknown
// and a value of another type, which JavaScript compares by type, then value
// or identity, as the runtime compares two boxes.
func (l *lowerer) unknownOperands(n *ast.Node, op ir.BinaryOp, left, right ir.Expr) (ir.Expr, ir.Expr, bool) {
	if op != ir.OpEqual && op != ir.OpNotEqual {
		return left, right, true
	}
	box := func(v ir.Expr) ir.Expr {
		if v.ExprType() == ir.Unknown {
			return v
		}
		v = asNumber(v)
		if t := v.ExprType(); t == ir.Base64 || ir.Unserializable(t, nil) != "" {
			l.failf(n, "%s between an unknown and %s is not supported yet", op, aType(t))
			return nil
		}
		return &ir.Box{Value: v, Span: v.ExprSpan()}
	}
	if left.ExprType() != ir.Unknown && right.ExprType() != ir.Unknown {
		return left, right, true
	}
	if left, right = box(left), box(right); left == nil || right == nil {
		return nil, nil, false
	}
	return left, right, true
}
