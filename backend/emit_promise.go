package backend

import (
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// Emission for Promise's statics and adoption (S1 plan D5): calls into the
// runtime's promise_all.go.

// promiseCall emits a call of one of Promise's statics, reporting whether x
// is one.
func (e *emitter) promiseCall(x *ir.HostCall) (string, bool) {
	var fn string
	switch x.Op {
	case ir.HostPromiseAll:
		fn = "PromiseAll"
	case ir.HostPromiseAllValues:
		fn = "PromiseAllValues"
	case ir.HostPromiseRace:
		fn = "PromiseRace"
	case ir.HostPromiseResolve:
		fn = "PromiseResolve"
	case ir.HostPromiseAllOf:
		fn = "PromiseAllOf"
	case ir.HostPromiseReject:
		e.usesRuntime = true
		elem, ok := goElem(x.Typ)
		if !ok {
			return e.cannot(x.Span, "the backend cannot emit a value of type "+x.Typ.String()+" yet"), true
		}
		return rtPkg + ".PromiseReject[" + elem + "](" + e.dom() + ", " + e.expr(x.Args[0]) + ")", true
	case ir.HostPromiseValue:
		return e.expr(x.Recv) + ".Value()", true
	default:
		return "", false
	}
	e.usesRuntime = true
	args := []string{e.dom()}
	for _, a := range x.Args {
		args = append(args, e.expr(a))
	}
	if x.Op == ir.HostPromiseResolve && len(x.Args) == 0 {
		args = append(args, "struct{}{}") // Promise.resolve(), a promise of nothing
	}
	return rtPkg + "." + fn + "(" + strings.Join(args, ", ") + ")", true
}
