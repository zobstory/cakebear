package backend

import "github.com/zobstory/cakebear/ir"

// siteArg is the *cakebearrt.Site argument of a runtime call that may throw
// (runtime/thrown.go), the place an uncaught error's report points at (M6
// plan D6): a throw, a `new Error`, or a call the runtime throws for, as
// Node points at it (R6). A site carries the source line with its types
// blanked, which lowering will provide in M6 step 3; until then every site
// is nil, a place the report doesn't know, and the report has no excerpt.
func (e *emitter) siteArg(ir.Span) string { return "nil" }
