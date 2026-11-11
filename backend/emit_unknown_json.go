package backend

import (
	"strconv"

	"github.com/zobstory/cakebear/ir"
)

// Emission for JSON.parse and an unknown's reads (S1 plan D3, D4 (b)): calls
// into the runtime's json_parse.go and unknown_object.go.
func (e *emitter) unknownJSON(x ir.Expr) string {
	e.usesRuntime = true
	switch x := x.(type) {
	case *ir.JSONParse:
		return rtPkg + ".JSONParse(" + e.expr(x.Text) + ")"
	case *ir.UnknownGet:
		return rtPkg + ".UnknownGet(" + e.expr(x.Value) + ", " + strconv.Quote(x.Key) + ", " + e.siteArg(x.Span) + ")"
	case *ir.UnknownIn:
		return rtPkg + ".UnknownIn(" + e.expr(x.Key) + ", " + e.expr(x.Value) + ", " + e.siteArg(x.Span) + ")"
	case *ir.UnknownIsArray:
		return rtPkg + ".UnknownIsArray(" + e.expr(x.Value) + ")"
	case *ir.Decode:
		return e.decode(x) // emit_decode.go
	case *ir.UnknownIndex:
		return rtPkg + ".UnknownIndex(" + e.expr(x.Value) + ", " + e.expr(x.Index) + ", " + e.siteArg(x.Span) + ")"
	}
	panic("unknownJSON: not one of its nodes")
}
