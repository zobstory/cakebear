package lower

import (
	"path"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// fetch, its Response and its Headers (S1 plan D2), recognised by the files
// declaring them, as node:http's members are: TypeScript's lib.dom.d.ts, or
// @types/node's web-globals, whose types are undici-types'. So a program's
// own fetch is an ordinary function.

// fetchFile reports whether decl is in one of the files declaring fetch's
// globals and types.
func fetchFile(decl *ast.Node) bool {
	f := ast.GetSourceFileOfNode(decl)
	if f == nil {
		return false
	}
	name := string(f.FileName())
	switch {
	case inBundledLib(decl):
		return path.Base(name) == "lib.dom.d.ts"
	case typesNodeFile(decl) == "web-globals/fetch.d.ts":
		return true
	}
	return strings.Contains(name, "/node_modules/undici-types/")
}

// declaredForFetch reports whether every declaration of sym is in a fetch
// file.
func declaredForFetch(sym *ast.Symbol) bool {
	if sym == nil || len(sym.Declarations()) == 0 {
		return false
	}
	for _, d := range sym.Declarations() {
		if !fetchFile(d) {
			return false
		}
	}
	return true
}

// fetchHostType is hostType for fetch's Response and Headers: one
// declaration in a fetch file is enough, as for node:http's types.
func fetchHostType(t *checker.Type) ir.HostType {
	sym := t.Symbol()
	if sym == nil {
		return ir.HostNone
	}
	var h ir.HostType
	switch sym.Name() {
	case "Response":
		h = ir.HostResponse
	case "Headers":
		h = ir.HostHeaders
	default:
		return ir.HostNone
	}
	for _, d := range sym.Declarations() {
		if fetchFile(d) {
			return h
		}
	}
	return ir.HostNone
}

// fetchMember is a member of Response or Headers as the runtime has it: a
// call's operation or a property's. The libs declare a Response's body
// readers on Body, undici-types on BodyMixin.
type fetchMember struct {
	op   ir.HostOp
	prop ir.HostProperty
	args int
}

var fetchMembers = map[string]map[string]fetchMember{
	"Response": {
		"status": {prop: ir.HostStatus}, "ok": {prop: ir.HostOK}, "statusText": {prop: ir.HostStatusText},
		"url": {prop: ir.HostResponseURL}, "redirected": {prop: ir.HostRedirected}, "headers": {prop: ir.HostResponseHeaders},
		"type": {prop: ir.HostResponseType}, "bodyUsed": {prop: ir.HostBodyUsed}, "text": {op: ir.HostResponseText},
		"json": {op: ir.HostResponseJSON},
	},
	"Headers": {
		"get": {op: ir.HostHeadersGet, args: 1}, "has": {op: ir.HostHeadersHas, args: 1},
		"set": {op: ir.HostHeadersSet, args: 2}, "append": {op: ir.HostHeadersAppend, args: 2},
		"delete": {op: ir.HostHeadersDelete, args: 1}, "getSetCookie": {op: ir.HostHeadersSetCookies},
	},
}

// fetchMemberOf is the member a property access names on a Response or a
// Headers, by its receiver's host type and the member's name, once the
// member is known to be declared for fetch. ok is false for anything else.
func (l *lowerer) fetchMemberOf(access *ast.Node) (recv ir.HostType, name string, m fetchMember, known, ok bool) {
	pa := access.AsPropertyAccessExpression()
	sym := l.checker.GetSymbolAtLocation(pa.Name())
	if !declaredForFetch(sym) {
		return
	}
	switch h := l.typeFromTypeQuiet(l.checker.GetTypeAtLocation(pa.Expression)); h {
	case ir.HostOf(ir.HostResponse):
		recv = ir.HostResponse
	case ir.HostOf(ir.HostHeaders):
		recv = ir.HostHeaders
	default:
		return
	}
	name = pa.Name().Text()
	m, known = fetchMembers[recv.String()][name]
	return recv, name, m, known, true
}

// typeFromTypeQuiet is typeFromType keeping the refusal reason it would
// set: asking a receiver's type must not change what an error reports.
func (l *lowerer) typeFromTypeQuiet(t *checker.Type) ir.Type {
	why := l.why
	defer func() { l.why = why }()
	return l.typeFromType(t)
}

// fetchRefused names what lowering refuses of a Response or a Headers, with
// what to do instead where there is something.
var fetchRefused = map[string]string{
	"arrayBuffer": "a body as bytes is not supported yet; read it with text()",
	"blob":        "a body as a Blob is not supported yet; read it with text()",
	"bytes":       "a body as bytes is not supported yet; read it with text()",
	"formData":    "a body as form data is not supported yet; read it with text()",
	"body":        "a body as a stream is not supported yet; read it with text()",
	"clone":       "Response.clone() is not supported yet",
}

// fetchProperty lowers a property read of a Response or a Headers.
func (l *lowerer) fetchProperty(n *ast.Node) (ir.Expr, bool) {
	recv, name, m, known, ok := l.fetchMemberOf(n)
	if !ok {
		return nil, false
	}
	if !known || m.prop == ir.HostPropInvalid {
		l.failFetchMember(n, recv, name, known)
		return nil, true
	}
	v := l.expr(n.AsPropertyAccessExpression().Expression)
	if v == nil {
		return nil, true
	}
	return &ir.HostProp{Prop: m.prop, Recv: v, Typ: l.typeOf(n), Span: l.span(n)}, true
}

func (l *lowerer) failFetchMember(n *ast.Node, recv ir.HostType, name string, known bool) {
	switch why, refused := fetchRefused[name]; {
	case refused && recv == ir.HostResponse:
		l.failf(n, "%s", why)
	case known:
		l.failf(n, "%s.%s can only be called, not used as a value, yet", recv, name)
	default:
		l.failf(n, "%s.%s is not supported yet", recv, name)
	}
}

// fetchMethodCall lowers a call of a Response's or a Headers' method.
func (l *lowerer) fetchMethodCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	recv, name, m, known, ok := l.fetchMemberOf(c.Expression)
	if !ok {
		return nil, false
	}
	if !known || m.op == ir.HostOpInvalid {
		l.failFetchMember(c.Expression, recv, name, known && m.op != ir.HostOpInvalid)
		return nil, true
	}
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}
	if len(args) != m.args {
		l.failf(n, "%s.%s takes %d argument%s, got %d", recv, name, m.args, plural(m.args), len(args))
		return nil, true
	}
	call := &ir.HostCall{Op: m.op, Typ: l.typeOf(n), Span: l.calleeCaret(l.span(n), n)}
	if m.op == ir.HostResponseJSON {
		// The libs type it Promise<any>, undici-types Promise<unknown>: an
		// unknown, as JSON.parse's result is (unknown_json.go).
		if call.Typ != ir.PromiseOf(ir.Unknown) && !l.jsonSlot(n) {
			l.failf(n, "Response.json() returns any, which cakebear doesn't lower: give what it resolves to a type, unknown to narrow it or a data type to cast it to")
			return nil, true
		}
		call.Typ = ir.PromiseOf(ir.Unknown)
	}
	if call.Recv = l.expr(c.Expression.AsPropertyAccessExpression().Expression); call.Recv == nil {
		return nil, true
	}
	for _, a := range args {
		v := l.expr(a)
		if v == nil {
			return nil, true
		}
		if v.ExprType() != ir.String {
			l.failf(a, "%s.%s takes strings, not a %s", recv, name, v.ExprType())
			return nil, true
		}
		call.Args = append(call.Args, v)
	}
	return call, true
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
