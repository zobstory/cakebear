package backend

import (
	"strconv"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// Emission for fetch (S1 plan D2): calls into the runtime's fetch.go.

// fetchMethods names the runtime method behind each operation on a fetch
// value: a Response's text, and a Headers' own.
var fetchMethods = map[ir.HostOp]string{
	ir.HostResponseText:      "Text",
	ir.HostResponseJSON:      "JSON",
	ir.HostHeadersGet:        "Get",
	ir.HostHeadersHas:        "Has",
	ir.HostHeadersSet:        "Set",
	ir.HostHeadersAppend:     "Append",
	ir.HostHeadersDelete:     "Delete",
	ir.HostHeadersSetCookies: "GetSetCookie",
}

// fetchCall emits a fetch operation, reporting whether x is one.
func (e *emitter) fetchCall(x *ir.HostCall) (string, bool) {
	switch x.Op {
	case ir.HostFetch:
		e.usesRuntime, e.needsRun = true, true
		input := e.expr(x.Args[0])
		init := "nil"
		if len(x.Keys) > 1 {
			opts := make([]string, 0, len(x.Args)-1)
			for i, a := range x.Args[1:] {
				opts = append(opts, e.fetchOption(x.Keys[i+1], a))
			}
			init = rtPkg + ".FetchInitOf(" + strings.Join(opts, ", ") + ")"
		}
		return rtPkg + ".Fetch(" + e.dom() + ", " + input + ", " + init + ", " + e.siteArg(x.Span) + ")", true
	case ir.HostNewHeaders:
		e.usesRuntime = true
		pairs := make([]string, len(x.Args))
		for i, a := range x.Args {
			pairs[i] = "{" + strconv.Quote(x.Keys[i]) + ", " + e.expr(a) + "}"
		}
		return rtPkg + ".NewHeaders([][2]string{" + strings.Join(pairs, ", ") + "}, " + e.siteArg(x.Span) + ")", true
	}
	method, ok := fetchMethods[x.Op]
	if !ok {
		return "", false
	}
	e.usesRuntime = true
	args := make([]string, 0, len(x.Args)+2)
	if x.Op == ir.HostResponseText || x.Op == ir.HostResponseJSON {
		e.needsRun = true
		args = append(args, e.dom())
	}
	for _, a := range x.Args {
		args = append(args, e.expr(a))
	}
	if x.Op != ir.HostHeadersSetCookies {
		args = append(args, e.siteArg(x.Span))
	}
	recv := e.receiver(x.Recv, reading(member(x.Op.String()), x.Span))
	return recv + "." + method + "(" + strings.Join(args, ", ") + ")", true
}

// fetchOption is one property of fetch's init.
func (e *emitter) fetchOption(key string, v ir.Expr) string {
	switch {
	case key == ir.FetchKeyMethod:
		return rtPkg + ".FetchMethod(" + e.expr(v) + ")"
	case strings.HasPrefix(key, ir.FetchKeyHeader):
		return rtPkg + ".FetchHeader(" + strconv.Quote(strings.TrimPrefix(key, ir.FetchKeyHeader)) + ", " + e.expr(v) + ")"
	case key == ir.FetchKeyHeaders:
		return rtPkg + ".FetchHeaders(" + e.expr(v) + ")"
	case key == ir.FetchKeyBody:
		return rtPkg + ".FetchBody(" + e.expr(v) + ")"
	case key == ir.FetchKeyRedirect:
		return rtPkg + ".FetchRedirect(" + e.expr(v) + ")"
	case key == ir.FetchKeyCache:
		return rtPkg + ".FetchCache(" + e.expr(v) + ")"
	case key == ir.FetchKeyFormBody:
		return rtPkg + ".FetchFormBody(" + e.expr(v) + ")"
	case key == ir.FetchKeySignal:
		return rtPkg + ".FetchSignal(" + e.expr(v) + ")"
	}
	return rtPkg + ".FetchDropped(" + e.expr(v) + ")"
}
