package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// fetch(input, init) and new Headers(init) (S1 plan D2). init is an object
// literal, whose properties become the call's arguments in source order, so
// they are evaluated in the order Node evaluates the literal. Each value is
// lowered as it is, not fitted to the slot RequestInit declares for it,
// which makes every property one that may be undefined.

// fetchIgnored are init's properties Node's fetch accepts and does nothing
// with (s1-measure, measured on Node 24.18): Next.js's `next`, and the rest.
var fetchIgnored = map[string]bool{"next": true, "keepalive": true, "credentials": true}

// fetchCall lowers a call of fetch.
func (l *lowerer) fetchCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	callee := c.Expression
	if callee.Kind != ast.KindIdentifier || callee.Text() != "fetch" || !declaredForFetch(l.checker.GetSymbolAtLocation(callee)) {
		return nil, false
	}
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}
	if len(args) < 1 || len(args) > 2 {
		l.failf(n, "fetch takes 1 or 2 arguments, got %d", len(args))
		return nil, true
	}
	input := l.exprOf(args[0])
	if input == nil {
		return nil, true
	}
	if input.ExprType() == ir.HostOf(ir.HostURLValue) {
		input, _ = urlString(input) // url.go
	}
	if input.ExprType() != ir.String {
		l.failf(args[0], "fetch's input must be a string here, not a %s; a URL or a Request is not supported yet", input.ExprType())
		return nil, true
	}
	call := &ir.HostCall{Op: ir.HostFetch, Args: []ir.Expr{input}, Keys: []string{""}, Typ: l.typeOf(n), Span: l.calleeCaret(l.span(n), n)}
	if call.Typ == ir.Invalid {
		l.failf(n, "fetch's result has a type the backend cannot represent yet%s", l.reason())
		return nil, true
	}
	if len(args) == 2 && !l.fetchInit(args[1], call) {
		return nil, true
	}
	return call, true
}

// fetchInit lowers fetch's init into call's arguments.
func (l *lowerer) fetchInit(init *ast.Node, call *ir.HostCall) bool {
	if init.Kind != ast.KindObjectLiteralExpression {
		l.failf(init, "fetch's init must be an object literal here")
		return false
	}
	for _, p := range init.AsObjectLiteralExpression().Properties.Nodes {
		key, value, ok := l.literalProperty(p, "fetch's init")
		if !ok {
			return false
		}
		switch {
		case key == "method" || key == "body" || key == "cache":
			v := l.exprOf(value)
			if v == nil {
				return false
			}
			if key == "body" && v.ExprType() == ir.HostOf(ir.HostSearchParams) {
				v, _ = urlString(v) // url.go: the form encoding, its own content type
				key = ir.FetchKeyFormBody
			}
			if v.ExprType() != ir.String {
				l.failf(value, "fetch's %s must be a string here, not a %s", key, v.ExprType())
				return false
			}
			l.fetchArg(call, key, v)
		case key == "headers":
			if !l.fetchHeaders(value, call, ir.FetchKeyHeader) {
				return false
			}
		case key == "redirect":
			mode := ""
			if value.Kind == ast.KindStringLiteral {
				mode = value.Text()
			}
			if mode != "follow" && mode != "manual" && mode != "error" {
				l.failf(value, "fetch's redirect must be \"follow\", \"manual\" or \"error\", written as a string")
				return false
			}
			l.fetchArg(call, ir.FetchKeyRedirect, &ir.StringLit{Value: mode, Span: l.span(value)})
		case fetchIgnored[key]:
			if inert(value) {
				continue // a literal: evaluating it does nothing
			}
			v := l.exprOf(value)
			if v == nil {
				return false
			}
			l.fetchArg(call, ir.FetchKeyDropped, v)
		case key == "signal":
			v := l.exprOf(value)
			if v == nil {
				return false
			}
			if v.ExprType() != ir.HostOf(ir.HostAbortSignal) {
				l.failf(value, "fetch's signal must be an AbortSignal here, from AbortSignal.timeout, not %s", aType(v.ExprType()))
				return false
			}
			l.fetchArg(call, ir.FetchKeySignal, v)
		default:
			l.failf(p, "fetch's %s is not supported yet", key)
			return false
		}
	}
	return true
}

func (l *lowerer) fetchArg(call *ir.HostCall, key string, v ir.Expr) {
	call.Args = append(call.Args, v)
	call.Keys = append(call.Keys, key)
}

// fetchHeaders lowers headers written as an object literal of strings, or
// an array of pairs, into call's arguments, each keyed by prefix and its
// name; any other value must be a Headers.
func (l *lowerer) fetchHeaders(value *ast.Node, call *ir.HostCall, prefix string) bool {
	switch value.Kind {
	case ast.KindObjectLiteralExpression:
		for _, p := range value.AsObjectLiteralExpression().Properties.Nodes {
			name, v, ok := l.literalProperty(p, "headers")
			if !ok || !l.headerValue(v, call, prefix+name) {
				return false
			}
		}
		return true
	case ast.KindArrayLiteralExpression:
		for _, e := range value.AsArrayLiteralExpression().Elements.Nodes {
			if e.Kind != ast.KindArrayLiteralExpression || len(e.AsArrayLiteralExpression().Elements.Nodes) != 2 ||
				e.AsArrayLiteralExpression().Elements.Nodes[0].Kind != ast.KindStringLiteral {
				l.failf(e, "headers as pairs must each be written `[\"name\", value]` here")
				return false
			}
			pair := e.AsArrayLiteralExpression().Elements.Nodes
			if !l.headerValue(pair[1], call, prefix+pair[0].Text()) {
				return false
			}
		}
		return true
	}
	if prefix != ir.FetchKeyHeader {
		l.failf(value, "new Headers takes an object literal or an array of pairs here")
		return false
	}
	v := l.exprOf(value)
	if v == nil {
		return false
	}
	if v.ExprType() != ir.HostOf(ir.HostHeaders) {
		l.failf(value, "fetch's headers must be an object literal, an array of pairs or a Headers here, not a %s", v.ExprType())
		return false
	}
	l.fetchArg(call, ir.FetchKeyHeaders, v)
	return true
}

func (l *lowerer) headerValue(value *ast.Node, call *ir.HostCall, key string) bool {
	v := l.exprOf(value)
	if v == nil {
		return false
	}
	if v.ExprType() != ir.String {
		l.failf(value, "a header's value must be a string here, not a %s", v.ExprType())
		return false
	}
	l.fetchArg(call, key, v)
	return true
}

// literalProperty is an object literal's property: its name, as written or
// as a string literal, and its value. A shorthand is its name's binding.
func (l *lowerer) literalProperty(p *ast.Node, what string) (string, *ast.Node, bool) {
	switch p.Kind {
	case ast.KindPropertyAssignment:
		name := p.Name()
		switch name.Kind {
		case ast.KindIdentifier, ast.KindStringLiteral:
			return name.Text(), p.Initializer(), true
		}
		l.failf(p, "a computed name in %s is not supported yet", what)
	case ast.KindShorthandPropertyAssignment:
		return p.Name().Text(), p.Name(), true
	case ast.KindSpreadAssignment:
		l.failf(p, "spreading into %s is not supported yet", what)
	default:
		l.failf(p, "a method or accessor in %s is not supported", what)
	}
	return "", nil, false
}

// inert reports whether evaluating n can do nothing observable: a literal,
// or an object or array literal of them.
func inert(n *ast.Node) bool {
	switch n.Kind {
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindTrueKeyword, ast.KindFalseKeyword,
		ast.KindNullKeyword, ast.KindNoSubstitutionTemplateLiteral:
		return true
	case ast.KindObjectLiteralExpression:
		for _, p := range n.AsObjectLiteralExpression().Properties.Nodes {
			if p.Kind != ast.KindPropertyAssignment || p.Name().Kind == ast.KindComputedPropertyName || !inert(p.Initializer()) {
				return false
			}
		}
		return true
	case ast.KindArrayLiteralExpression:
		for _, e := range n.AsArrayLiteralExpression().Elements.Nodes {
			if !inert(e) {
				return false
			}
		}
		return true
	}
	return false
}

// newHeaders lowers `new Headers(init?)`, its pairs keyed by name.
func (l *lowerer) newHeaders(n *ast.Node, args []*ast.Node) ir.Expr {
	call := &ir.HostCall{Op: ir.HostNewHeaders, Typ: ir.HostOf(ir.HostHeaders), Span: l.span(n)}
	switch len(args) {
	case 0:
		return call
	case 1:
		if !l.fetchHeaders(args[0], call, "") {
			return nil
		}
		return call
	}
	l.failf(n, "new Headers takes at most 1 argument, got %d", len(args))
	return nil
}
