package backend

import (
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// Emission for node:http. Each host operation is a call into the runtime's
// http.go, which documents the mapping from the TypeScript API.

// hostGoTypes names the runtime type behind each host type. ir/ names them
// abstractly; this is the one place they become Go.
var hostGoTypes = map[ir.HostType]string{
	ir.HostHTTPServer:      "Server",
	ir.HostIncomingMessage: "IncomingMessage",
	ir.HostServerResponse:  "ServerResponse",
}

// hostMethods names the runtime method behind each host operation on a value.
// createServer is the one free function, and is handled separately.
var hostMethods = map[ir.HostOp]string{
	ir.HostListen:    "Listen",
	ir.HostSetHeader: "SetHeader",
	ir.HostWriteHead: "WriteHead",
	ir.HostEnd:       "End",
}

var hostProps = map[ir.HostProperty]string{
	ir.HostURL:    "URL",
	ir.HostMethod: "Method",
}

func goHostType(h ir.HostType) string {
	if name, ok := hostGoTypes[h]; ok {
		return "*" + rtPkg + "." + name
	}
	return "any"
}

// typ spells a type for a declaration, noting when that needs the runtime: a
// host type in a signature is a reference to the runtime package even in a
// function that never calls it, and Go rejects the import's absence as surely
// as an unused one.
func (e *emitter) typ(t ir.Type) string {
	if mentionsHost(t) {
		e.usesRuntime = true
	}
	return goType(t)
}

func mentionsHost(t ir.Type) bool {
	switch t.Kind {
	case ir.KindHost:
		return true
	case ir.KindFunc:
		for _, p := range t.Sig.Params {
			if mentionsHost(p) {
				return true
			}
		}
		return mentionsHost(t.Sig.Result)
	}
	return false
}

func (e *emitter) hostCall(x *ir.HostCall) string {
	args := make([]string, len(x.Args))
	for i, a := range x.Args {
		args[i] = e.expr(a)
	}
	list := strings.Join(args, ", ")

	if x.Op == ir.HostCreateServer {
		e.usesRuntime, e.usesHost = true, true
		return rtPkg + ".CreateServer(" + list + ")"
	}
	method, ok := hostMethods[x.Op]
	if !ok || x.Recv == nil {
		e.unsupported = append(e.unsupported, Unsupported{Span: x.Span, What: describeUnsupported(x)})
		return "#error unhandled host operation"
	}
	e.usesRuntime, e.usesHost = true, true
	return e.expr(x.Recv) + "." + method + "(" + list + ")"
}

func (e *emitter) hostProp(x *ir.HostProp) string {
	method, ok := hostProps[x.Prop]
	if !ok {
		e.unsupported = append(e.unsupported, Unsupported{Span: x.Span, What: describeUnsupported(x)})
		return "#error unhandled host property"
	}
	return e.expr(x.Recv) + "." + method + "()"
}
