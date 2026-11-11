package lower

import (
	"path"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/ir"
)

// @types/node's declarations of what cakebear's runtime implements (S1 plan
// D12). A program with @types/node is built without cakebear's own node:http
// (cmd/cakec/typesnode.go), so its node:http, and its timers, are
// @types/node's. They are recognised as cakebear's own are, by the file
// declaring them, and mapped onto cakebear's names for them, so the rest of
// lowering doesn't know which declared what.
//
// @types/node declares far more than the runtime implements, and declares
// it more loosely: setHeader takes a number or an array, end any chunk,
// listen a path or a host. So a call through @types/node is held to the
// argument types cakebear's own declarations would have made the checker
// insist on (typesNodeArgs).

const typesNodeDir = "/node_modules/@types/node/"

// typesNodeFile is decl's file within @types/node, such as "http.d.ts", or ""
// when @types/node doesn't declare it.
func typesNodeFile(decl *ast.Node) string {
	f := ast.GetSourceFileOfNode(decl)
	if f == nil {
		return ""
	}
	_, within, ok := strings.Cut(string(f.FileName()), typesNodeDir)
	if !ok {
		return ""
	}
	return within
}

// typesNodeDecl is a member as @types/node declares it: its file, the class
// or interface declaring it ("" for a module's function), and its name.
type typesNodeDecl struct{ file, container, name string }

// typesNodeMembers maps @types/node's declarations onto cakebear's own. A
// response's setHeader and end are inherited, from OutgoingMessage and from
// stream.Writable; a server's listen is net.Server's.
var typesNodeMembers = map[typesNodeDecl]hostMember{
	{"http.d.ts", "", "createServer"}:             {"", "createServer"},
	{"net.d.ts", "Server", "listen"}:              {"Server", "listen"},
	{"http.d.ts", "OutgoingMessage", "setHeader"}: {"ServerResponse", "setHeader"},
	{"http.d.ts", "ServerResponse", "writeHead"}:  {"ServerResponse", "writeHead"},
	{"stream.d.ts", "Writable", "end"}:            {"ServerResponse", "end"},
	{"http.d.ts", "IncomingMessage", "url"}:       {"IncomingMessage", "url"},
	{"http.d.ts", "IncomingMessage", "method"}:    {"IncomingMessage", "method"},
}

// typesNodeHosts are @types/node's types for the runtime's host values.
var typesNodeHosts = map[typesNodeDecl]ir.HostType{
	{"http.d.ts", "", "Server"}:          ir.HostHTTPServer,
	{"http.d.ts", "", "IncomingMessage"}: ir.HostIncomingMessage,
	{"http.d.ts", "", "ServerResponse"}:  ir.HostServerResponse,
	{"timers.d.ts", "", "Timeout"}:       ir.HostTimeout,
}

// httpFiles are @types/node's files declaring what node:http's members are.
// Its other files, its timers say, are recognised where they are lowered.
var httpFiles = map[string]bool{"http.d.ts": true, "net.d.ts": true, "stream.d.ts": true}

// typesNodeContainers are the classes whose other members are named in a
// refusal as node:http's, so it says which member the runtime lacks.
var typesNodeContainers = map[string]bool{"IncomingMessage": true, "ServerResponse": true, "OutgoingMessage": true, "Server": true}

// container is the name of the class or interface declaring decl, or "".
func container(decl *ast.Node) string {
	if p := decl.Parent; p != nil && (p.Kind == ast.KindClassDeclaration || p.Kind == ast.KindInterfaceDeclaration) {
		if name := p.Name(); name != nil {
			return name.Text()
		}
	}
	return ""
}

// typesNodeMember is hostMemberOf for a symbol @types/node declares. found
// is true for what typesNodeMembers maps, and for any other member of
// node:http's own classes or module, which then names itself in the refusal;
// anything else of @types/node's is no host member, and is refused as the
// library global it is.
func typesNodeMember(sym *ast.Symbol) (m hostMember, found, augmented bool) {
	decls := sym.Declarations()
	theirs := 0
	for _, d := range decls {
		if httpFiles[typesNodeFile(d)] {
			theirs++
		}
	}
	switch {
	case theirs == 0:
		return m, false, false
	case theirs != len(decls):
		return m, false, true
	}
	key := typesNodeDecl{typesNodeFile(decls[0]), container(decls[0]), sym.Name()}
	if mapped, ok := typesNodeMembers[key]; ok {
		return mapped, true, false
	}
	if key.file == "http.d.ts" && (key.container == "" && sym.Flags()&ast.SymbolFlagsFunction != 0 || typesNodeContainers[key.container]) {
		return hostMember{iface: key.container, name: key.name}, true, false
	}
	return m, false, false
}

// typesNodeHostType is hostType for @types/node's types: a class or
// interface it declares, or an intersection holding one, which is how it
// types a request handler's response (`ServerResponse & { req: … }`).
func (l *lowerer) typesNodeHostType(t *checker.Type) ir.HostType {
	if t.Flags()&checker.TypeFlagsIntersection != 0 {
		for _, part := range t.Types() {
			if h := l.typesNodeHostType(part); h != ir.HostNone {
				return h
			}
		}
		return ir.HostNone
	}
	sym := t.Symbol()
	if sym == nil || sym.Flags()&(ast.SymbolFlagsClass|ast.SymbolFlagsInterface) == 0 {
		return ir.HostNone
	}
	for _, d := range sym.Declarations() {
		if h, ok := typesNodeHosts[typesNodeDecl{typesNodeFile(d), "", sym.Name()}]; ok {
			return h
		}
	}
	return ir.HostNone
}

// typesNodeArgs refuses arguments to a call through @types/node that
// cakebear's own declarations would not have let through. It reports
// whether the call may go on.
func (l *lowerer) typesNodeArgs(m hostMember, args []*ast.Node, lowered []ir.Expr) bool {
	want := func(i int, ok bool, what string) bool {
		if !ok {
			l.failf(args[i], "%s's %s must be %s, not a %s", m, ordinal(i), what, lowered[i].ExprType())
		}
		return ok
	}
	switch m {
	case hostMember{"Server", "listen"}:
		if !want(0, lowered[0].ExprType() == ir.Number, "a port number; listening on a host or a path is not supported yet") {
			return false
		}
		return len(lowered) < 2 || want(1, lowered[1].ExprType().Kind == ir.KindFunc, "a callback")
	case hostMember{"ServerResponse", "setHeader"}:
		return want(0, lowered[0].ExprType() == ir.String, "a string") && want(1, lowered[1].ExprType() == ir.String, "a string")
	case hostMember{"ServerResponse", "writeHead"}:
		return want(0, lowered[0].ExprType() == ir.Number, "a number")
	case hostMember{"ServerResponse", "end"}:
		return len(lowered) == 0 || want(0, lowered[0].ExprType() == ir.String, "a string; a callback or a buffer is not supported yet")
	}
	return true
}

func ordinal(i int) string {
	switch i {
	case 0:
		return "first argument"
	case 1:
		return "second argument"
	}
	return "argument"
}

// viaTypesNode reports whether name, a call's callee, is declared by
// @types/node, through an import's alias too.
func (l *lowerer) viaTypesNode(name *ast.Node) bool {
	sym := l.checker.GetSymbolAtLocation(name)
	if sym != nil && sym.Flags()&ast.SymbolFlagsAlias != 0 {
		sym = l.checker.GetAliasedSymbol(sym)
	}
	if sym == nil {
		return false
	}
	for _, d := range sym.Declarations() {
		if typesNodeFile(d) != "" {
			return true
		}
	}
	return false
}

// timerDeclared reports whether every declaration of sym, setTimeout or
// clearTimeout, is the DOM's or @types/node's: with both in the program,
// TypeScript merges them into one set of overloads.
func timerDeclared(sym *ast.Symbol) bool {
	if sym == nil || len(sym.Declarations()) == 0 {
		return false
	}
	for _, d := range sym.Declarations() {
		dom := inBundledLib(d) && path.Base(string(ast.GetSourceFileOfNode(d).FileName())) == "lib.dom.d.ts"
		if !dom && typesNodeFile(d) != "timers.d.ts" {
			return false
		}
	}
	return true
}

// nodeTimer reports whether call, a setTimeout, resolved to one of
// @types/node's overloads, which return a Timeout.
func (l *lowerer) nodeTimer(call *ast.Node) bool {
	sig := l.checker.GetResolvedSignature(call)
	return sig != nil && sig.Declaration() != nil && typesNodeFile(sig.Declaration()) == "timers.d.ts"
}
