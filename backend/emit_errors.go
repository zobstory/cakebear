package backend

import (
	"fmt"
	"strconv"

	"github.com/zobstory/cakebear/ir"
)

// Errors (M6 plan D2-D6): Error values, unknown, and the sites an uncaught
// error's report points at. The runtime's half is runtime/error.go,
// unknown.go, thrown.go and uncaught.go; throw and try are emit_try.go's.

// goErrorType spells unknown, which is a Go any holding the value, and an
// Error, which is the runtime's.
func goErrorType(t ir.Type) (string, bool) {
	if t.Kind == ir.KindUnknown {
		return "any", true
	}
	return "*" + rtPkg + ".Error", true
}

// The runtime's names for ir's enums, by value, so that a reordering of
// either can't silently pair the wrong ones.
var (
	rtErrorKinds = map[ir.ErrorKind]string{
		ir.ErrorBase: "ErrorBase", ir.ErrorType: "ErrorType", ir.ErrorRange: "ErrorRange", ir.ErrorURI: "ErrorURI",
		ir.ErrorSyntax: "ErrorSyntax", ir.ErrorReference: "ErrorReference", ir.ErrorEval: "ErrorEval",
	}
	rtTypeTags = map[ir.TypeTag]string{
		ir.TagUndefined: "TagUndefined", ir.TagObject: "TagObject", ir.TagBoolean: "TagBoolean", ir.TagNumber: "TagNumber",
		ir.TagString: "TagString", ir.TagFunction: "TagFunction", ir.TagSymbol: "TagSymbol", ir.TagBigint: "TagBigint",
	}
	rtErrorProps = map[ir.ErrorProp]string{
		ir.ErrorMessage: "Message", ir.ErrorName: "Name", ir.ErrorStack: "Stack", ir.ErrorCause: "Cause",
	}
)

// errorExpr emits the expressions of ir/errors.go.
func (e *emitter) errorExpr(x ir.Expr) string {
	e.usesRuntime = true
	switch x := x.(type) {
	case *ir.NewError:
		message := `""` // no message of its own: Node shows nothing the subset can see differ
		if x.Message != nil {
			message = e.expr(x.Message)
		}
		return fmt.Sprintf("%s.NewError(%s.%s, %s, %s)", rtPkg, rtPkg, rtErrorKinds[x.Kind], message, e.siteArg(x.Span))
	case *ir.ErrorGet:
		return fmt.Sprintf("%s.%s()", e.expr(x.Error), rtErrorProps[x.Prop])
	case *ir.Box:
		return e.boxAs(x.Value.ExprType(), e.expr(x.Value))
	case *ir.Unbox:
		// Checked as Must is, naming the value as the source does.
		what := strconv.Quote(e.unwrapped(x.Value))
		if x.Typ.Kind == ir.KindMaybe {
			return fmt.Sprintf("%s.UnboxMaybe[%s](%s, %s)", rtPkg, e.typ(*x.Typ.Elem, x.Span), e.expr(x.Value), what)
		}
		return fmt.Sprintf("%s.Unbox[%s](%s, %s)", rtPkg, e.typ(x.Typ, x.Span), e.expr(x.Value), what)
	case *ir.InstanceOf:
		kind := rtPkg + "." + rtErrorKinds[x.Kind]
		switch t := x.Value.ExprType(); {
		case t == ir.Unknown:
			return fmt.Sprintf("%s.InstanceOf(%s, %s)", rtPkg, e.expr(x.Value), kind)
		case t.Kind == ir.KindMaybe:
			return fmt.Sprintf("%s.MaybeInstanceOf(%s, %s)", rtPkg, e.expr(x.Value), kind)
		default:
			return fmt.Sprintf("%s.InstanceOf(%s)", e.expr(x.Value), kind)
		}
	case *ir.TypeOf:
		return fmt.Sprintf("(%s.TypeOf(%s) == %s.%s)", rtPkg, e.expr(x.Value), rtPkg, rtTypeTags[x.Tag])
	}
	return e.cannot(x.ExprSpan(), describeUnsupported(x))
}

// boxAs is v, a value of type t, as an unknown holds it (runtime/unknown.go):
// a maybe as the value its state is, null as Null and undefined as nil, and
// anything else as it is. A number is a float64 even as a constant, since
// goFloat spells every one as a float literal, which Go boxes as a float64.
func (e *emitter) boxAs(t ir.Type, v string) string {
	switch {
	case t.Kind == ir.KindMaybe:
		return rtPkg + ".BoxMaybe(" + v + ")"
	case t == ir.Null:
		return rtPkg + ".Null"
	case t == ir.Undefined:
		return "nil"
	}
	return v
}
