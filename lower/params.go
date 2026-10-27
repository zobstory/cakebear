package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// Optional and default parameters, and defaults in destructuring (M5 plan
// scope D). A caller may leave out an optional parameter, `x?: T`, or one
// with a default, `x = d`, or pass undefined for it, so in the function's
// signature either is a maybe, and a call that leaves arguments out passes
// undefined for them (fillArgs). A default is evaluated where the argument is
// undefined, and only there: null is a value, as in JavaScript. It runs at
// the call, after the parameters before it, so it may read them.

// signatureParam is the type a parameter p has in its function's signature:
// what the body sees, or a maybe of it where a caller may leave it out.
func (l *lowerer) signatureParam(p *ast.Node) ir.Type {
	t := l.typeOf(p)
	if param := p.AsParameterDeclaration(); t != ir.Invalid && (param.QuestionToken != nil || param.Initializer != nil) {
		return ir.OrUndefined(t)
	}
	return t
}

// param lowers p, a declared parameter of signature type pt: the name the
// signature gives it, and what opens the body with it, to be called in the
// body, where the default is evaluated: the default given where the argument
// is undefined, and a pattern's bindings. A plain or optional parameter is
// its own binding and needs nothing.
func (l *lowerer) param(p *ast.Node, pt ir.Type) (string, func() []ir.Stmt, bool) {
	decl := p.AsParameterDeclaration()
	pattern := decl.Name().Kind == ast.KindObjectBindingPattern
	if decl.Initializer == nil && !pattern {
		return bindingText(decl.Name()), nil, true
	}
	arg := l.tempName("param")
	prologue := func() []ir.Stmt {
		var src ir.Expr = &ir.Ident{Name: arg, Stable: true, Typ: pt, Span: l.span(p)}
		var out []ir.Stmt
		if decl.Initializer != nil {
			inner := l.typeOf(p)
			given := l.defaulted(src, decl.Initializer, inner)
			if given == nil {
				return nil
			}
			name := l.tempName("param")
			if !pattern {
				name = decl.Name().Text()
			}
			// A string parameter the body appends to keeps its accumulator
			// on the binding, which here is this declaration (append.go).
			appended := !pattern && inner == ir.String && l.appended(decl.Name())
			out = append(out, &ir.VarDecl{Name: name, Type: inner, Init: given, Mutable: true, Appended: appended, Span: l.span(p)})
			if !pattern {
				return out
			}
			src = &ir.Ident{Name: name, Stable: true, Typ: inner, Span: l.span(p)}
		}
		bound := l.bindPattern(decl.Name(), src, true)
		if bound == nil {
			return nil
		}
		return append(out, bound...)
	}
	return arg, prologue, true
}

// defaulted is v, a maybe, as a value of t, or d where v is undefined: a
// default's `x = d`, evaluated only there. v is read twice, so it must be one
// reading again finds unchanged, as a parameter or a field of a held value
// is.
func (l *lowerer) defaulted(v ir.Expr, d *ast.Node, t ir.Type) ir.Expr {
	// exprOf: in a pattern the checker types d's slot as the property, which
	// may be undefined, where d is the value given for that.
	dv := l.exprOf(d)
	if dv == nil {
		return nil
	}
	if dv = into(dv, t); !dv.ExprType().Equal(t) {
		l.failf(d, "a default of %s for %s is not supported yet", aType(dv.ExprType()), aType(t))
		return nil
	}
	given := reread(v)
	if given == nil {
		l.failf(d, "a default here is not supported yet")
		return nil
	}
	if !given.ExprType().Equal(t) {
		given = &ir.Unwrap{Value: given, Typ: t, Span: v.ExprSpan()}
	}
	undefined := &ir.IsEmpty{Value: v, Undefined: true, Span: v.ExprSpan()}
	return &ir.Cond{Test: undefined, Then: dv, Else: given, Typ: t, Span: l.span(d)}
}

// fillArgs is a call's arguments as its callee's parameters, params, take
// them: each a maybe where the parameter is one, and undefined for a
// parameter the call leaves out, which TypeScript allows only where the
// parameter is optional or has a default.
func fillArgs(args []ir.Expr, params []ir.Type, span ir.Span) []ir.Expr {
	for i, pt := range params {
		if i < len(args) {
			args[i] = intoMaybe(args[i], pt)
		} else if pt.Kind == ir.KindMaybe {
			args = append(args, &ir.Empty{Typ: pt, Span: span})
		} else if pt == ir.Unknown { // which holds undefined itself
			args = append(args, &ir.Box{Value: &ir.UndefinedLit{Span: span}, Span: span})
		}
	}
	return args
}

// boxedArgs boxes each argument that a parameter of the callee's takes as an
// unknown, where the checker's slot for it is none: reject's reason, which
// TypeScript types any (ir.RejectType). An argument a slot boxed already is
// left as it is. It reports false once it has refused one.
func (l *lowerer) boxedArgs(nodes []*ast.Node, args []ir.Expr, params []ir.Type) bool {
	for i, pt := range params {
		if pt == ir.Unknown && i < len(nodes) && i < len(args) {
			if args[i] = l.box(nodes[i], args[i]); args[i] == nil { // errors.go
				return false
			}
		}
	}
	return true
}

// opening is the statements the prologues give, in the parameters' order.
func opening(prologues []func() []ir.Stmt) []ir.Stmt {
	var out []ir.Stmt
	for _, open := range prologues {
		out = append(out, open()...)
	}
	return out
}
