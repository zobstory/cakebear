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
	ir.NumToFixed: "NumberToFixed", ir.NumFromStr: "StringToNumber",
	ir.NumParseInt: "ParseInt", ir.NumParseFlt: "ParseFloat",
	ir.NumIsFinite: "NumberIsFinite", ir.NumIsNaN: "NumberIsNaN", ir.NumIsInteger: "NumberIsInteger",
	ir.URIEncode: "EncodeURIComponent", ir.URIDecode: "DecodeURIComponent",
}

func (e *emitter) builtinCall(x *ir.BuiltinCall) string {
	e.usesRuntime = true
	if s, ok := e.accLength(x); ok {
		return s
	}
	if x.Fn == ir.ToString && len(x.Args) == 1 {
		return e.toString(x.Args[0])
	}
	fn, ok := builtinFuncs[x.Fn]
	if !ok {
		return e.cannot(x.Span, "the backend has no runtime function for "+x.Fn.String()+" yet")
	}
	args := make([]string, len(x.Args))
	for i, a := range x.Args {
		args[i] = e.expr(a)
	}
	return fmt.Sprintf("%s.%s(%s)", rtPkg, fn, strings.Join(args, ", "))
}

// toString is a primitive's ToString: a number as JavaScript prints it, an
// extension type as console.log prints it, a string as it is, and a maybe
// of one of them as "undefined", "null" or its value's.
func (e *emitter) toString(a ir.Expr) string {
	v := e.expr(a)
	t := a.ExprType()
	if t.Kind == ir.KindMaybe && t.Elem != nil {
		return e.maybeToString(v, t, a.ExprSpan())
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
