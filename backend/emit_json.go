package backend

import (
	"fmt"

	"github.com/zobstory/cakebear/ir"
)

// jsonStringify emits JSON.stringify as the runtime's serializer (M4 plan
// D15). The space argument becomes a gap at run time, as V8 computes it: a
// number of spaces, at most 10, or a string's first 10 code units. The gap is
// a value of its own, not a string, because V8 pretty-prints with no indent
// for a space between 0 and 1, where an empty string is compact; the zero
// JSONGap, for a call with no space, is compact.
func (e *emitter) jsonStringify(x *ir.JSONStringify) string {
	e.usesRuntime = true
	if what := ir.Unserializable(x.Value.ExprType(), nil); what != "" {
		return e.cannot(x.Span, "the backend cannot serialise a "+x.Value.ExprType().String()+" yet: it holds "+what)
	}
	gap := rtPkg + ".JSONGap{}"
	if x.Space != nil {
		switch t := x.Space.ExprType(); {
		case t == ir.String || t == ir.Base64:
			gap = fmt.Sprintf("%s.JSONGapString(%s)", rtPkg, e.expr(x.Space))
		case t == ir.Number:
			gap = fmt.Sprintf("%s.JSONGapNumber(%s)", rtPkg, e.expr(x.Space))
		case t.IsNumeric():
			gap = fmt.Sprintf("%s.JSONGapNumber(float64(%s))", rtPkg, e.expr(x.Space))
		default:
			return e.cannot(x.Span, "JSON.stringify's space cannot be a "+t.String())
		}
	}
	return fmt.Sprintf("%s.JSONStringify(%s, %s, %s)", rtPkg, e.expr(x.Value), gap, e.siteArg(x.Span))
}
