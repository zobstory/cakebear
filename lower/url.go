package lower

import (
	"path"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// URL, URLSearchParams and AbortSignal.timeout (S1 plan D6, D2), recognised
// by the files declaring them, as fetch's are: TypeScript's lib.dom.d.ts,
// or @types/node's url.d.ts and web-globals. So a program's own URL is an
// ordinary binding. Each is a host value of its domain (runtime/url*.go,
// abort.go).

// urlFile reports whether decl is in one of the files declaring them.
func urlFile(decl *ast.Node) bool {
	if inBundledLib(decl) {
		f := ast.GetSourceFileOfNode(decl)
		if f == nil {
			return false
		}
		base := path.Base(string(f.FileName()))
		return base == "lib.dom.d.ts" || base == "lib.dom.iterable.d.ts"
	}
	switch typesNodeFile(decl) {
	case "url.d.ts", "web-globals/abortcontroller.d.ts":
		return true
	}
	return false
}

// declaredForURL reports whether every declaration of sym is in such a file.
func declaredForURL(sym *ast.Symbol) bool {
	if sym == nil || len(sym.Declarations()) == 0 {
		return false
	}
	for _, d := range sym.Declarations() {
		if !urlFile(d) {
			return false
		}
	}
	return true
}

var urlHostTypes = map[string]ir.HostType{"URL": ir.HostURLValue, "URLSearchParams": ir.HostSearchParams, "AbortSignal": ir.HostAbortSignal}

// urlHostType is hostType for the three: one declaration in such a file is
// enough, as for fetch's types.
func urlHostType(t *checker.Type) ir.HostType {
	sym := t.Symbol()
	if sym == nil {
		return ir.HostNone
	}
	h, ok := urlHostTypes[sym.Name()]
	if !ok {
		return ir.HostNone
	}
	for _, d := range sym.Declarations() {
		if urlFile(d) {
			return h
		}
	}
	return ir.HostNone
}

// urlSetters are the URL's properties a program may assign.
var urlSetters = map[string]bool{
	"href": true, "protocol": true, "username": true, "password": true, "host": true,
	"hostname": true, "port": true, "pathname": true, "search": true, "hash": true,
}

// paramsMethods are URLSearchParams' methods: their operation and how many
// strings they take, least and most.
var paramsMethods = map[string]struct {
	op       ir.HostOp
	min, max int
}{
	"append": {ir.HostParamsAppend, 2, 2}, "delete": {ir.HostParamsDelete, 1, 2}, "get": {ir.HostParamsGet, 1, 1},
	"getAll": {ir.HostParamsGetAll, 1, 1}, "has": {ir.HostParamsHas, 1, 2}, "set": {ir.HostParamsSet, 2, 2},
	"sort": {ir.HostParamsSort, 0, 0}, "toString": {ir.HostParamsString, 0, 0},
}

// urlReceiver is the host type of a property access's receiver, where the
// member it names is declared for URL; HostNone for anything else.
func (l *lowerer) urlReceiver(access *ast.Node) ir.HostType {
	pa := access.AsPropertyAccessExpression()
	if !declaredForURL(l.checker.GetSymbolAtLocation(pa.Name())) {
		return ir.HostNone
	}
	switch t := l.typeFromTypeQuiet(l.checker.GetTypeAtLocation(pa.Expression)); t.Host {
	case ir.HostURLValue, ir.HostSearchParams, ir.HostAbortSignal:
		return t.Host
	}
	return ir.HostNone // fetch's, say, which declare their members in lib.dom.d.ts too
}

// ofAbortController reports, having refused it, whether access reads a
// member of an AbortController: its signal is AbortSignal.timeout's alone.
func (l *lowerer) ofAbortController(access *ast.Node) bool {
	pa := access.AsPropertyAccessExpression()
	sym := l.checker.GetTypeAtLocation(pa.Expression).Symbol()
	if sym == nil || sym.Name() != "AbortController" || !declaredForURL(l.checker.GetSymbolAtLocation(pa.Name())) {
		return false
	}
	l.failf(access, "AbortController is not supported yet: fetch's signal comes from AbortSignal.timeout")
	return true
}

// urlProperty lowers a read of a URL's getter or a URLSearchParams' size.
func (l *lowerer) urlProperty(n *ast.Node) (ir.Expr, bool) {
	if l.ofAbortController(n) {
		return nil, true
	}
	recv := l.urlReceiver(n)
	if recv == ir.HostNone {
		return nil, false
	}
	name := n.Name().Text()
	prop, ok := ir.URLProps[name]
	switch {
	case recv == ir.HostURLValue && ok:
	case recv == ir.HostSearchParams && name == "size":
		prop = ir.HostParamsSize
	case recv == ir.HostAbortSignal:
		l.failf(n, "AbortSignal.%s is not supported yet: a signal is only fetch's, from AbortSignal.timeout", name)
		return nil, true
	default:
		l.failf(n, "%s.%s can only be called, not used as a value, yet", recv, name)
		return nil, true
	}
	v := l.expr(n.Expression())
	if v == nil {
		return nil, true
	}
	t := ir.String
	switch prop {
	case ir.HostURLSearchParams:
		t = ir.HostOf(ir.HostSearchParams)
	case ir.HostParamsSize:
		t = ir.Number
	}
	return &ir.HostProp{Prop: prop, Recv: v, Typ: t, Span: l.span(n)}, true
}

// urlSet lowers `u.pathname = v` and the other setters, as a statement.
func (l *lowerer) urlSet(stmt, target, value *ast.Node) ([]ir.Stmt, bool) {
	if target.Kind != ast.KindPropertyAccessExpression || l.urlReceiver(target) != ir.HostURLValue {
		return nil, false
	}
	name := target.Name().Text()
	if !urlSetters[name] {
		l.failf(stmt, "URL.%s can't be assigned", name)
		return nil, true
	}
	recv := l.expr(target.Expression())
	v := l.expr(value)
	if recv == nil || v == nil {
		return nil, true
	}
	if v.ExprType() != ir.String {
		l.failf(value, "a URL's %s is set from a string here, not %s", name, aType(v.ExprType()))
		return nil, true
	}
	set := &ir.HostCall{Op: ir.HostURLSet, Recv: recv, Args: []ir.Expr{v}, Keys: []string{name}, Typ: ir.Void, Span: l.caretAfter(l.span(stmt), target)}
	return []ir.Stmt{&ir.ExprStmt{Expr: set, Span: set.Span}}, true
}

// urlMethodCall lowers a call of a URL's, a URLSearchParams' or their
// constructors' members: toString and toJSON, the params' methods,
// forEach, URL.canParse and URL.parse, and AbortSignal.timeout.
func (l *lowerer) urlMethodCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	if out, ok := l.urlStatic(n, c); ok {
		return out, true
	}
	if l.ofAbortController(c.Expression) {
		return nil, true
	}
	recv := l.urlReceiver(c.Expression)
	if recv == ir.HostNone {
		return nil, false
	}
	name := c.Expression.Name().Text()
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}
	span := l.calleeCaret(l.span(n), n)
	call := &ir.HostCall{Typ: l.typeOf(n), Span: span}
	switch {
	case recv == ir.HostURLValue && (name == "toString" || name == "toJSON") && len(args) == 0:
		call.Op, call.Typ = ir.HostURLString, ir.String
	case recv == ir.HostSearchParams && name == "forEach":
		return l.paramsForEach(n, c, args, span)
	case recv == ir.HostSearchParams:
		m, ok := paramsMethods[name]
		if !ok {
			l.failf(n, "URLSearchParams.%s is not supported yet; forEach goes over its pairs", name)
			return nil, true
		}
		if len(args) < m.min || len(args) > m.max {
			l.failf(n, "URLSearchParams.%s takes %d to %d arguments here, got %d", name, m.min, m.max, len(args))
			return nil, true
		}
		call.Op = m.op
		if !l.stringArgs(call, args, "URLSearchParams."+name) {
			return nil, true
		}
	default:
		l.failf(n, "%s.%s is not supported yet", recv, name)
		return nil, true
	}
	if call.Recv = l.expr(c.Expression.Expression()); call.Recv == nil {
		return nil, true
	}
	return call, true
}

// stringArgs lowers args into call's, each a string.
func (l *lowerer) stringArgs(call *ir.HostCall, args []*ast.Node, what string) bool {
	for _, a := range args {
		v := l.expr(a)
		if v == nil {
			return false
		}
		if v.ExprType() != ir.String {
			l.failf(a, "%s takes strings here, not %s", what, aType(v.ExprType()))
			return false
		}
		call.Args = append(call.Args, v)
	}
	return true
}

// paramsForEach lowers `params.forEach(callback)`, which the runtime runs
// over the live list, as Node's does.
func (l *lowerer) paramsForEach(n *ast.Node, c *ast.CallExpression, args []*ast.Node, span ir.Span) (ir.Expr, bool) {
	if len(args) != 1 {
		l.failf(n, "URLSearchParams.forEach takes its callback alone here")
		return nil, true
	}
	recv := l.expr(c.Expression.Expression())
	cb := l.expr(args[0])
	if recv == nil || cb == nil {
		return nil, true
	}
	want := ir.FuncType([]ir.Type{ir.String, ir.String, ir.HostOf(ir.HostSearchParams)}, ir.Void)
	if !cb.ExprType().Equal(want) {
		l.failf(args[0], "URLSearchParams.forEach's callback must be %s here, not %s", want, cb.ExprType())
		return nil, true
	}
	return &ir.HostCall{Op: ir.HostParamsForEach, Recv: recv, Args: []ir.Expr{cb}, Typ: ir.Void, Span: span}, true
}

// urlStatic lowers URL.canParse and URL.parse, and AbortSignal.timeout.
func (l *lowerer) urlStatic(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	recv := c.Expression.Expression()
	if recv.Kind != ast.KindIdentifier || (recv.Text() != "URL" && recv.Text() != "AbortSignal") ||
		!declaredForURL(l.checker.GetSymbolAtLocation(recv)) {
		return nil, false
	}
	name := c.Expression.Name().Text()
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}
	span := l.calleeCaret(l.span(n), n)
	switch {
	case recv.Text() == "AbortSignal" && name == "timeout" && len(args) == 1:
		ms := l.expr(args[0])
		if ms == nil {
			return nil, true
		}
		if ms.ExprType() != ir.Number {
			l.failf(args[0], "AbortSignal.timeout takes a number here, not %s", aType(ms.ExprType()))
			return nil, true
		}
		return &ir.HostCall{Op: ir.HostAbortSignalTimeout, Args: []ir.Expr{ms}, Typ: ir.HostOf(ir.HostAbortSignal), Span: span}, true
	case recv.Text() == "URL" && (name == "canParse" || name == "parse") && len(args) >= 1 && len(args) <= 2:
		call := &ir.HostCall{Op: ir.HostURLCanParse, Typ: ir.Boolean, Span: span}
		if name == "parse" {
			call.Op, call.Typ = ir.HostURLParse, ir.MaybeOf(ir.HostOf(ir.HostURLValue))
		}
		if !l.urlInputs(call, args, "URL."+name) {
			return nil, true
		}
		return call, true
	}
	l.failf(n, "%s.%s is not supported yet", recv.Text(), name)
	return nil, true
}

// urlInputs lowers a URL's input and base into call's arguments: each a
// string, or a URL as its href, which Node's URL takes a URL base as too
// (`${base}`), parsing it again.
func (l *lowerer) urlInputs(call *ir.HostCall, args []*ast.Node, what string) bool {
	for _, a := range args {
		v := l.exprOf(a)
		if v == nil {
			return false
		}
		switch t := v.ExprType(); {
		case t == ir.String:
		case t == ir.HostOf(ir.HostURLValue):
			v = &ir.HostCall{Op: ir.HostURLString, Recv: v, Typ: ir.String, Span: v.ExprSpan()}
		default:
			l.failf(a, "%s takes a string or a URL here, not %s", what, aType(t))
			return false
		}
		call.Args = append(call.Args, v)
	}
	return true
}

// newURL lowers `new URL(input, base?)`.
func (l *lowerer) newURL(n *ast.Node, args []*ast.Node) ir.Expr {
	if len(args) < 1 || len(args) > 2 {
		l.failf(n, "new URL takes 1 or 2 arguments, got %d", len(args))
		return nil
	}
	call := &ir.HostCall{Op: ir.HostNewURL, Typ: ir.HostOf(ir.HostURLValue), Span: l.span(n)}
	if !l.urlInputs(call, args, "new URL") {
		return nil
	}
	return call
}

// newSearchParams lowers `new URLSearchParams(init?)`: none, a string (or a
// URLSearchParams, as its string), or an object literal of strings, its
// names in Keys.
func (l *lowerer) newSearchParams(n *ast.Node, args []*ast.Node) ir.Expr {
	call := &ir.HostCall{Op: ir.HostNewSearchParams, Typ: ir.HostOf(ir.HostSearchParams), Span: l.span(n)}
	switch {
	case len(args) == 0:
		return call
	case len(args) > 1:
		l.failf(n, "new URLSearchParams takes 1 argument, got %d", len(args))
		return nil
	case args[0].Kind == ast.KindObjectLiteralExpression:
		for _, p := range args[0].AsObjectLiteralExpression().Properties.Nodes {
			key, value, ok := l.literalProperty(p, "URLSearchParams' init") // fetch_call.go
			if !ok {
				return nil
			}
			v := l.exprOf(value)
			if v == nil {
				return nil
			}
			if v.ExprType() != ir.String {
				l.failf(value, "URLSearchParams' init takes strings here, not %s", aType(v.ExprType()))
				return nil
			}
			call.Args, call.Keys = append(call.Args, v), append(call.Keys, key)
		}
		if call.Keys == nil {
			call.Keys = []string{}
		}
		return call
	}
	v := l.exprOf(args[0])
	if v == nil {
		return nil
	}
	switch v.ExprType() {
	case ir.String:
	case ir.HostOf(ir.HostSearchParams):
		v = &ir.HostCall{Op: ir.HostParamsString, Recv: v, Typ: ir.String, Span: v.ExprSpan()}
	default:
		l.failf(args[0], "new URLSearchParams takes a string, an object literal of strings or a URLSearchParams here, not %s", aType(v.ExprType()))
		return nil
	}
	call.Args = []ir.Expr{v}
	return call
}

// urlString is v as a string where v is a URL (its href) or a
// URLSearchParams (its form encoding): ToString's, for String(),
// templates and fetch's input and body.
func urlString(v ir.Expr) (ir.Expr, bool) {
	switch v.ExprType() {
	case ir.HostOf(ir.HostURLValue):
		return &ir.HostCall{Op: ir.HostURLString, Recv: v, Typ: ir.String, Span: v.ExprSpan()}, true
	case ir.HostOf(ir.HostSearchParams):
		return &ir.HostCall{Op: ir.HostParamsString, Recv: v, Typ: ir.String, Span: v.ExprSpan()}, true
	}
	return v, false
}

// urlUpdate is `u.pathname += v` and the like: the getter's value, updated,
// through the setter.
func (l *lowerer) urlUpdate(stmt *ast.Node, get *ir.HostProp, sum ir.Expr, at ir.Span) []ir.Stmt {
	for name, p := range ir.URLProps {
		if p == get.Prop && urlSetters[name] {
			set := &ir.HostCall{Op: ir.HostURLSet, Recv: get.Recv, Args: []ir.Expr{sum}, Keys: []string{name}, Typ: ir.Void, Span: at}
			return []ir.Stmt{&ir.ExprStmt{Expr: set, Span: at}}
		}
	}
	l.failf(stmt, "%s can't be assigned", get.Prop)
	return nil
}
