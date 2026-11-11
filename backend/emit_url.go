package backend

import (
	"fmt"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// Emission for URL, URLSearchParams and AbortSignal.timeout (S1 plan D6,
// D2): calls into the runtime's url*.go and abort.go.

// urlProps names the runtime method behind a URL's getters and a
// URLSearchParams' size.
var urlProps = map[ir.HostProperty]string{
	ir.HostURLHref: "Href", ir.HostURLOrigin: "Origin", ir.HostURLProtocol: "Protocol",
	ir.HostURLUsername: "Username", ir.HostURLPassword: "Password", ir.HostURLHost: "Host",
	ir.HostURLHostname: "Hostname", ir.HostURLPort: "Port", ir.HostURLPathname: "Pathname",
	ir.HostURLSearch: "Search", ir.HostURLHash: "Hash", ir.HostURLSearchParams: "SearchParams",
	ir.HostParamsSize: "Size",
}

// paramsMethods names the runtime method behind URLSearchParams' own, by
// how many arguments the call passes where that picks one.
var paramsMethods = map[ir.HostOp][3]string{
	ir.HostParamsAppend: {"", "", "Append"}, ir.HostParamsDelete: {"", "Delete", "DeleteValue"},
	ir.HostParamsGet: {"", "Get"}, ir.HostParamsGetAll: {"", "GetAll"}, ir.HostParamsHas: {"", "Has", "HasValue"},
	ir.HostParamsSet: {"", "", "Set"}, ir.HostParamsSort: {"Sort"}, ir.HostParamsString: {"String"},
}

// urlCall emits one of the URL operations, reporting whether x is one.
func (e *emitter) urlCall(x *ir.HostCall) (string, bool) {
	args := make([]string, len(x.Args))
	for i, a := range x.Args {
		args[i] = e.expr(a)
	}
	rt := func(fn string, more ...string) string {
		e.usesRuntime = true
		all := make([]string, 0, len(args)+len(more))
		return rtPkg + "." + fn + "(" + strings.Join(append(append(all, args...), more...), ", ") + ")"
	}
	recv := func() string { return e.receiver(x.Recv, reading(member(x.Op.String()), x.Span)) }
	switch x.Op {
	case ir.HostNewURL:
		if len(x.Args) == 1 {
			return rt("NewURL", e.siteArg(x.Span)), true
		}
		return rt("NewURLWithBase", e.siteArg(x.Span)), true // a URL base as its href, as Node takes one
	case ir.HostURLCanParse, ir.HostURLParse:
		fn := "URLCanParse"
		if x.Op == ir.HostURLParse {
			fn = "URLParse"
		}
		if len(x.Args) == 2 {
			fn += "WithBase"
		}
		return rt(fn), true
	case ir.HostURLSet:
		name := x.Keys[0]
		set := "Set" + strings.ToUpper(name[:1]) + name[1:]
		if name == "href" {
			return recv() + ".SetHref(" + args[0] + ", " + e.siteArg(x.Span) + ")", true
		}
		return recv() + "." + set + "(" + args[0] + ")", true
	case ir.HostURLString:
		return recv() + ".String()", true
	case ir.HostNewSearchParams:
		switch {
		case x.Keys != nil:
			pairs := make([]string, 0, 2*len(args))
			for i, a := range args {
				pairs = append(pairs, fmt.Sprintf("%q", x.Keys[i]), a)
			}
			args = pairs
			return rt("NewURLSearchParamsPairs"), true
		case len(args) == 1:
			return rt("NewURLSearchParamsString"), true
		}
		return rt("NewURLSearchParams"), true
	case ir.HostParamsForEach:
		// The live list, read again each turn, as Node's forEach reads it.
		return fmt.Sprintf("func() {\n\tts_p, ts_cb := %s, %s\n\tfor ts_i := 0; ts_i < ts_p.Len(); ts_i++ {\n\t\tts_name, ts_value := ts_p.Entry(ts_i)\n\t\tts_cb(%s, ts_value, ts_name, ts_p)\n\t}\n}()",
			recv(), args[0], e.dom()), true
	case ir.HostAbortSignalTimeout:
		e.usesRuntime = true
		return rtPkg + ".AbortSignalTimeout(" + e.dom() + ", " + args[0] + ", " + e.siteArg(x.Span) + ")", true
	}
	if names, ok := paramsMethods[x.Op]; ok {
		return recv() + "." + names[len(args)] + "(" + strings.Join(args, ", ") + ")", true
	}
	return "", false
}
