package lower

import (
	"fmt"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
	"github.com/zobstory/cakebear/types"
)

// hostTypes are the interfaces in types/node-http.d.ts the runtime implements.
var hostTypes = map[string]ir.HostType{
	"Server":          ir.HostHTTPServer,
	"IncomingMessage": ir.HostIncomingMessage,
	"ServerResponse":  ir.HostServerResponse,
}

// hostMember names something declared in the bundled node:http: the declaring
// interface ("" for a module-level function) and the member's own name.
type hostMember struct{ iface, name string }

func (m hostMember) String() string {
	if m.iface == "" {
		return m.name
	}
	return m.iface + "." + m.name
}

// hostSpec is an operation and the arguments the runtime's version takes.
type hostSpec struct {
	op       ir.HostOp
	min, max int
}

// hostCalls must list every method and function node-http.d.ts declares, and
// hostProps every property. TestHostTablesCoverDeclarations fails when the
// declarations and these tables drift apart.
var hostCalls = map[hostMember]hostSpec{
	{"", "createServer"}:            {ir.HostCreateServer, 1, 1},
	{"Server", "listen"}:            {ir.HostListen, 1, 2},
	{"ServerResponse", "setHeader"}: {ir.HostSetHeader, 2, 2},
	{"ServerResponse", "writeHead"}: {ir.HostWriteHead, 1, 1},
	{"ServerResponse", "end"}:       {ir.HostEnd, 0, 1},
}

var hostProps = map[hostMember]ir.HostProperty{
	{"IncomingMessage", "url"}:    ir.HostURL,
	{"IncomingMessage", "method"}: ir.HostMethod,
}

func inBundledHTTP(decl *ast.Node) bool {
	f := ast.GetSourceFileOfNode(decl)
	return f != nil && f.FileName() == types.NodeHTTPPath()
}

// hostType reports which host type t is, or ir.HostNone.
//
// One declaration in the bundled file is enough: a program that augments
// ServerResponse still holds a real ServerResponse. Members are held to a
// stricter test (hostMemberOf), because an augmented member has nothing behind
// it at runtime.
func (l *lowerer) hostType(t *checker.Type) ir.HostType {
	sym := t.Symbol()
	if sym == nil || sym.Flags&ast.SymbolFlagsInterface == 0 {
		return ir.HostNone
	}
	h, ok := hostTypes[sym.Name]
	if !ok {
		return ir.HostNone
	}
	for _, d := range sym.Declarations {
		if inBundledHTTP(d) {
			return h
		}
	}
	return ir.HostNone
}

// hostMemberOf resolves a symbol to the bundled member it names.
//
// The symbol's own declarations decide, never the receiver's type: under a
// module augmentation that type is merged across files, so "declared somewhere
// in the bundled file" would accept a member the program added itself. found
// means every declaration is bundled; augmented means only some are.
func (l *lowerer) hostMemberOf(sym *ast.Symbol) (m hostMember, found, augmented bool) {
	if sym == nil {
		return m, false, false
	}
	// A named import is an alias declared in the program's own file.
	if sym.Flags&ast.SymbolFlagsAlias != 0 {
		if sym = l.checker.GetAliasedSymbol(sym); sym == nil {
			return m, false, false
		}
	}
	bundled := 0
	for _, d := range sym.Declarations {
		if inBundledHTTP(d) {
			bundled++
		}
	}
	if bundled == 0 {
		return m, false, false
	}
	if bundled != len(sym.Declarations) {
		return m, false, true
	}
	m.name = sym.Name
	if p := sym.Declarations[0].Parent; p != nil && p.Kind == ast.KindInterfaceDeclaration {
		m.iface = p.Name().Text()
	}
	return m, true, false
}

// importDecl accepts an import of node:http and produces no IR: the names it
// binds are resolved through the checker wherever they are used.
func (l *lowerer) importDecl(n *ast.Node) {
	decl := n.AsImportDeclaration()
	spec := decl.ModuleSpecifier
	if spec.Kind != ast.KindStringLiteral || (spec.Text() != "node:http" && spec.Text() != "http") {
		l.fail(n, "imports from %q are not supported yet; cakebear bundles only node:http", spec.Text())
		return
	}
	if clause := decl.ImportClause; clause != nil && clause.Name() != nil {
		l.fail(n, "a default import of %s is not supported; use `import { createServer }` or `import * as http`", spec.Text())
	}
}

// hostCall lowers a call into node:http. handled is false when the callee is
// not part of it, and the caller carries on with an ordinary call.
func (l *lowerer) hostCall(n *ast.Node, c *ast.CallExpression) (out ir.Expr, handled bool) {
	var name, recv *ast.Node
	switch c.Expression.Kind {
	case ast.KindIdentifier:
		name = c.Expression
	case ast.KindPropertyAccessExpression:
		name, recv = c.Expression.Name(), c.Expression.Expression()
	default:
		return nil, false
	}

	m, found, augmented := l.hostMemberOf(l.checker.GetSymbolAtLocation(name))
	switch {
	case augmented:
		l.fail(n, "%s is augmented outside node:http, which is not supported yet", name.Text())
		return nil, true
	case !found:
		if recv != nil {
			if rt := l.typeOf(recv); rt.Kind == ir.KindHost {
				l.fail(n, "%s.%s is not part of cakebear's node:http", rt, name.Text())
				return nil, true
			}
		}
		return nil, false
	}

	spec, ok := hostCalls[m]
	if !ok {
		l.fail(n, "cakebear's runtime does not implement %s yet", m)
		return nil, true
	}
	var args []*ast.Node
	if c.Arguments != nil {
		args = c.Arguments.Nodes
	}
	if len(args) < spec.min || len(args) > spec.max {
		l.fail(n, "%s takes %s, got %d", m, spec.arity(), len(args))
		return nil, true
	}

	call := &ir.HostCall{Op: spec.op, Typ: l.typeOf(n), Span: l.span(n)}
	// A method needs its receiver. A free function reached through a
	// namespace import (`http.createServer`) has none: `http` is not a value.
	if m.iface != "" {
		if call.Recv = l.expr(recv); call.Recv == nil {
			return nil, true
		}
	}
	for _, a := range args {
		v := l.expr(a)
		if v == nil {
			return nil, true
		}
		call.Args = append(call.Args, v)
	}
	return call, true
}

func (s hostSpec) arity() string {
	switch {
	case s.min == s.max && s.min == 1:
		return "1 argument"
	case s.min == s.max:
		return fmt.Sprintf("%d arguments", s.min)
	case s.max == s.min+1:
		return fmt.Sprintf("%d or %d arguments", s.min, s.max)
	default:
		return fmt.Sprintf("%d to %d arguments", s.min, s.max)
	}
}

// property lowers a property read. node:http's are the only ones so far.
func (l *lowerer) property(n *ast.Node) ir.Expr {
	access := n.AsPropertyAccessExpression()
	m, found, augmented := l.hostMemberOf(l.checker.GetSymbolAtLocation(access.Name()))
	if prop, ok := hostProps[m]; found && ok {
		recv := l.expr(access.Expression)
		if recv == nil {
			return nil
		}
		return &ir.HostProp{Prop: prop, Recv: recv, Typ: l.typeOf(n), Span: l.span(n)}
	}
	switch {
	case augmented:
		l.fail(n, "%s is augmented outside node:http, which is not supported yet", access.Name().Text())
	case found:
		l.failHostValue(n, m)
	default:
		l.fail(n, "%s is not supported yet", describeKind(n.Kind))
	}
	return nil
}

// failHostValue refuses a bundled member used as a value rather than called.
func (l *lowerer) failHostValue(n *ast.Node, m hostMember) {
	if _, ok := hostCalls[m]; ok {
		l.fail(n, "%s can only be called, not used as a value, yet", m)
		return
	}
	l.fail(n, "cakebear's runtime does not implement %s yet", m)
}
