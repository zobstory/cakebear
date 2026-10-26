package backend

import (
	"fmt"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// The strings step: the standard library's string and number functions, as
// the runtime's (str*.go, num*.go, uri*.go), and strings joined and compared
// as JavaScript's UTF-16 strings are, though they are Go's UTF-8 ones.

// builtinFuncs names the runtime function each builtin calls. ToString has
// one per type (toString).
var builtinFuncs = map[ir.Builtin]string{
	ir.StrLength: "StrLength", ir.StrCharAt: "StrCharAt", ir.StrCharCodeAt: "StrCharCodeAt",
	ir.StrIncludes: "StrIncludes", ir.StrStartsWith: "StrStartsWith", ir.StrEndsWith: "StrEndsWith",
	ir.StrIndexOf: "StrIndexOf", ir.StrLastIndexOf: "StrLastIndexOf",
	ir.StrSlice: "StrSlice", ir.StrSubstring: "StrSubstring",
	ir.StrToLowerCase: "StrToLowerCase", ir.StrToUpperCase: "StrToUpperCase",
	ir.StrTrim: "StrTrim", ir.StrTrimStart: "StrTrimStart", ir.StrTrimEnd: "StrTrimEnd",
	ir.StrSplit: "StrSplit", ir.StrReplace: "StrReplace", ir.StrReplaceAll: "StrReplaceAll",
	ir.StrPadStart: "StrPadStart", ir.StrPadEnd: "StrPadEnd", ir.StrRepeat: "StrRepeat",
	ir.StrAt: "StrAt", ir.StrCodePointAt: "StrCodePointAt", // a Maybe, undefined outside the string (str_at.go)
	ir.StrIndex: "StrIndex", ir.StrIndexOrUndefined: "StrIndexOrUndefined",
	ir.NumToFixed: "NumberToFixed", ir.NumFromStr: "StringToNumber",
	ir.NumParseInt: "ParseInt", ir.NumParseFlt: "ParseFloat",
	ir.NumIsFinite: "NumberIsFinite", ir.NumIsNaN: "NumberIsNaN", ir.NumIsInteger: "NumberIsInteger",
	ir.URIEncode: "EncodeURIComponent", ir.URIDecode: "DecodeURIComponent",
}

// sitedBuiltins are the builtins whose runtime function may throw, as Node
// throws there (M6 plan D9), and so takes the call's site last.
var sitedBuiltins = map[ir.Builtin]bool{
	ir.StrRepeat: true, ir.NumToFixed: true, ir.URIEncode: true, ir.URIDecode: true,
}

func (e *emitter) builtinCall(x *ir.BuiltinCall) string {
	e.usesRuntime = true
	if s, ok := e.accLength(x); ok {
		return s
	}
	if (x.Fn == ir.ToString || x.Fn == ir.ToStringMethod) && len(x.Args) == 1 {
		var use access // String(x) carries on with an empty x, as Node does
		if x.Fn == ir.ToStringMethod {
			use = reading("toString", x.Span)
		}
		return e.toString(x.Args[0], use)
	}
	fn, ok := builtinFuncs[x.Fn]
	if !ok {
		return e.cannot(x.Span, "the backend has no runtime function for "+x.Fn.String()+" yet")
	}
	args := make([]string, len(x.Args))
	for i, a := range x.Args {
		switch {
		case i == 0 && (x.Fn == ir.StrIndex || x.Fn == ir.StrIndexOrUndefined):
			args[i] = e.receiver(a, elementAccess(x.Args[1], false, x.Span)) // `s[i]` reads the property i
		case i == 0 && isMethod(x.Fn):
			args[i] = e.receiver(a, reading(x.Fn.String(), x.Span))
		default:
			args[i] = e.expr(a)
		}
	}
	list := strings.Join(args, ", ")
	if sitedBuiltins[x.Fn] {
		list += ", " + e.siteArg(x.Span)
	}
	return fmt.Sprintf("%s.%s(%s)", rtPkg, fn, list)
}

// isMethod reports whether fn is a method, whose first argument is its
// receiver: a string's, which ir/builtin.go lists together, `s[i]`
// included, or toFixed. x.toString() is builtinCall's own.
func isMethod(fn ir.Builtin) bool {
	return fn >= ir.StrLength && fn <= ir.StrIndexOrUndefined || fn == ir.NumToFixed
}

// toString is a primitive's ToString: a number as JavaScript prints it, an
// extension type as console.log prints it, a string as it is, and a maybe
// of one of them as "undefined", "null" or its value's. a is the receiver
// of use, the zero access for String(a).
func (e *emitter) toString(a ir.Expr, use access) string {
	v := e.receiver(a, use)
	t := a.ExprType()
	if t.Kind == ir.KindMaybe && t.Elem != nil {
		return e.maybeToString(v, t, a.ExprSpan())
	}
	switch t { // `Name: message`, and what an unknown holds (M6 D4, D5)
	case ir.Error:
		e.usesRuntime = true
		return rtPkg + ".ErrorToString(" + v + ")"
	case ir.Unknown:
		e.usesRuntime = true
		return rtPkg + ".UnknownToString(" + v + ")"
	}
	s, ok := primitiveToString(t, v)
	if !ok {
		return e.cannot(a.ExprSpan(), "the backend cannot convert a "+t.String()+" to a string yet")
	}
	return s
}

// primitiveToString is ToString of v, a primitive of type t. ok is false for
// a type that isn't one.
func primitiveToString(t ir.Type, v string) (string, bool) {
	switch t {
	case ir.String, ir.Base64:
		return v, true
	case ir.Number:
		return fmt.Sprintf("%s.NumberToString(%s)", rtPkg, v), true
	case ir.Float32:
		return fmt.Sprintf("%s.NumberToString(float64(%s))", rtPkg, v), true
	case ir.Boolean:
		return fmt.Sprintf("%s.BoolToString(%s)", rtPkg, v), true
	case ir.Int32, ir.Int64:
		return fmt.Sprintf("%s.IntToString(int64(%s))", rtPkg, v), true
	case ir.Uint32, ir.Uint64:
		return fmt.Sprintf("%s.UintToString(uint64(%s))", rtPkg, v), true
	}
	return "", false
}

// concat joins a template's parts through the runtime, which joins a
// surrogate pair split across two of them, as UTF-16 strings join.
func (e *emitter) concat(x *ir.Concat) string {
	if len(x.Parts) == 1 {
		return e.expr(x.Parts[0])
	}
	e.usesRuntime = true
	parts := make([]string, len(x.Parts))
	for i, p := range x.Parts {
		parts[i] = e.expr(p)
	}
	return fmt.Sprintf("%s.StrConcat(%s)", rtPkg, strings.Join(parts, ", "))
}

// stringBinary emits a binary operation on two strings, when x is one: the
// ordering comparisons in UTF-16 code unit order, where Go compares UTF-8
// bytes, which puts a character outside the BMP above U+E000–U+FFFF rather
// than below. Equality is Go's: two strings joined as JavaScript joins them
// are equal exactly when their bytes are. `+` is stringSum (emit_append.go).
func (e *emitter) stringBinary(x *ir.Binary, left, right string) (string, bool) {
	if !stringish(x.Left.ExprType()) || !stringish(x.Right.ExprType()) {
		return "", false
	}
	switch x.Op {
	case ir.OpLess, ir.OpLessEq, ir.OpGreater, ir.OpGreaterEq:
		op, _ := goBinaryOp(x.Op)
		e.usesRuntime = true
		return fmt.Sprintf("(%s.StrCompare(%s, %s) %s 0)", rtPkg, left, right, op), true
	}
	return "", false
}

func stringish(t ir.Type) bool { return t == ir.String || t == ir.Base64 }
