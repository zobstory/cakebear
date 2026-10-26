package ir

import "fmt"

// Builtin is one of the standard library's string and number functions the
// subset compiles (the M4 strings step): a string method, a conversion, a
// parser or a URI function. Each is a function of its arguments, with a
// string method's receiver first, and runs in the runtime with JavaScript's
// own semantics, UTF-16 indices included.
type Builtin int

const (
	BuiltinInvalid Builtin = iota // the zero value, never lowered

	StrLength // s.length
	StrCharAt
	StrCharCodeAt
	StrIncludes
	StrStartsWith
	StrEndsWith
	StrIndexOf
	StrLastIndexOf
	StrSlice
	StrSubstring
	StrToLowerCase
	StrToUpperCase
	StrTrim
	StrTrimStart
	StrTrimEnd
	StrSplit
	StrReplace // with a string pattern and a string replacement
	StrReplaceAll
	StrPadStart
	StrPadEnd
	StrRepeat
	StrAt // s.at(i): OrUndefined, M5's (D9), as are the three below
	StrCodePointAt
	StrIndex // s[i] typed string, which panics past the end, as xs[i] does (M4 D8)
	StrIndexOrUndefined

	ToString     // String(x), a template's ${x}: x a primitive
	// x.toString(): ToString of x, read as a method's receiver, so a
	// null or undefined x throws, where String(x) carries on with it.
	ToStringMethod
	NumToFixed   // n.toFixed(digits)
	NumFromStr   // Number(s)
	NumParseInt  // parseInt(s, radix?)
	NumParseFlt  // parseFloat(s)
	NumIsFinite  // Number.isFinite(n), isFinite(n)
	NumIsNaN     // Number.isNaN(n), isNaN(n)
	NumIsInteger // Number.isInteger(n)
	URIEncode    // encodeURIComponent(s)
	URIDecode    // decodeURIComponent(s)
)

// BuiltinSig is a builtin's signature: its required parameters, a string
// method's receiver first, then the optional ones, which a call may leave off
// from the end, and its result.
type BuiltinSig struct {
	Name     string
	Params   []Type
	Optional []Type
	Result   Type
}

var (
	str1 = []Type{String}
	str2 = []Type{String, String}
	num1 = []Type{Number}
)

var builtinSigs = map[Builtin]BuiltinSig{
	StrLength:           {"length", str1, nil, Number},
	StrCharAt:           {"charAt", []Type{String, Number}, nil, String},
	StrCharCodeAt:       {"charCodeAt", []Type{String, Number}, nil, Number},
	StrIncludes:         {"includes", str2, num1, Boolean},
	StrStartsWith:       {"startsWith", str2, num1, Boolean},
	StrEndsWith:         {"endsWith", str2, num1, Boolean},
	StrIndexOf:          {"indexOf", str2, num1, Number},
	StrLastIndexOf:      {"lastIndexOf", str2, num1, Number},
	StrSlice:            {"slice", str1, []Type{Number, Number}, String},
	StrSubstring:        {"substring", []Type{String, Number}, num1, String},
	StrToLowerCase:      {"toLowerCase", str1, nil, String},
	StrToUpperCase:      {"toUpperCase", str1, nil, String},
	StrTrim:             {"trim", str1, nil, String},
	StrTrimStart:        {"trimStart", str1, nil, String},
	StrTrimEnd:          {"trimEnd", str1, nil, String},
	StrSplit:            {"split", str2, num1, ArrayOf(String)},
	StrReplace:          {"replace", []Type{String, String, String}, nil, String},
	StrReplaceAll:       {"replaceAll", []Type{String, String, String}, nil, String},
	StrPadStart:         {"padStart", []Type{String, Number}, str1, String},
	StrPadEnd:           {"padEnd", []Type{String, Number}, str1, String},
	StrRepeat:           {"repeat", []Type{String, Number}, nil, String},
	StrAt:               {"at", []Type{String, Number}, nil, MaybeOf(String)},
	StrCodePointAt:      {"codePointAt", []Type{String, Number}, nil, MaybeOf(Number)},
	StrIndex:            {"[i]", []Type{String, Number}, nil, String},
	StrIndexOrUndefined: {"[i]", []Type{String, Number}, nil, MaybeOf(String)},
	ToString:            {"String", nil, nil, String}, // one primitive, checked on its own
	ToStringMethod:      {"toString", nil, nil, String},
	NumToFixed:          {"toFixed", []Type{Number, Number}, nil, String},
	NumFromStr:          {"Number", str1, nil, Number},
	NumParseInt:         {"parseInt", str1, num1, Number},
	NumParseFlt:         {"parseFloat", str1, nil, Number},
	NumIsFinite:         {"isFinite", num1, nil, Boolean},
	NumIsNaN:            {"isNaN", num1, nil, Boolean},
	NumIsInteger:        {"isInteger", num1, nil, Boolean},
	URIEncode:           {"encodeURIComponent", str1, nil, String},
	URIDecode:           {"decodeURIComponent", str1, nil, String},
}

// Sig is b's signature. A Builtin the table doesn't hold has the zero one.
func (b Builtin) Sig() BuiltinSig { return builtinSigs[b] }

func (b Builtin) String() string {
	if sig, ok := builtinSigs[b]; ok {
		return sig.Name
	}
	return fmt.Sprintf("builtin(%d)", int(b))
}

// BuiltinCall calls a Builtin. Typ is its signature's result.
type BuiltinCall struct {
	Fn   Builtin
	Args []Expr
	Typ  Type
	Span Span
}

// Concat is a template literal, `a${b}c`, as the strings it joins, each
// substitution already converted (ToString). A surrogate pair split across
// two parts is joined, as it is in JavaScript's UTF-16.
type Concat struct {
	Parts []Expr
	Span  Span
}

func (*BuiltinCall) isExpr()          {}
func (*Concat) isExpr()               {}
func (e *BuiltinCall) ExprType() Type { return e.Typ }
func (e *Concat) ExprType() Type      { return String }
func (e *BuiltinCall) ExprSpan() Span { return e.Span }
func (e *Concat) ExprSpan() Span      { return e.Span }

// stringLike reports whether t is a string at run time: a string, or
// cakebear's base64, which is one.
func stringLike(t Type) bool { return t == String || t == Base64 }

// IsPrimitive reports whether a value of type t has a string form ToString
// gives without calling into the program: a string, a number, a boolean or
// an extension type.
func IsPrimitive(t Type) bool {
	return t == String || t == Number || t == Boolean || t.IsExtension()
}

// ToStringable reports whether ToString takes a value of type t: a
// primitive, a maybe of one, an Error (`Name: message`), or an unknown,
// whatever it holds (M6 plan D4, D5). None calls into the program: the
// built-in kinds have no toString of a program's, and nor have the objects
// an unknown may hold, which give `[object Object]`.
func ToStringable(t Type) bool {
	return IsPrimitive(primitiveOf(t)) || t == Error || t == Unknown
}

// checkBuiltin holds a builtin call to its signature. A string argument may
// be a base64, which is a string.
func checkBuiltin(x *BuiltinCall) error {
	sig, ok := builtinSigs[x.Fn]
	if !ok {
		return fmt.Errorf("unknown builtin %d", int(x.Fn))
	}
	if !x.Typ.Equal(sig.Result) {
		return fmt.Errorf("%s gives a %s, not a %s", sig.Name, sig.Result, x.Typ)
	}
	if x.Fn == ToString || x.Fn == ToStringMethod {
		// A maybe of one is "undefined" or "null" when it is empty (M5 D8).
		if len(x.Args) != 1 || !ToStringable(x.Args[0].ExprType()) {
			return fmt.Errorf("%s takes one primitive, or a maybe of one, an Error or an unknown", sig.Name)
		}
		return nil
	}
	if len(x.Args) < len(sig.Params) || len(x.Args) > len(sig.Params)+len(sig.Optional) {
		return fmt.Errorf("%s takes %d to %d arguments, not %d", sig.Name, len(sig.Params), len(sig.Params)+len(sig.Optional), len(x.Args))
	}
	params := append(append([]Type(nil), sig.Params...), sig.Optional...)
	for i, a := range x.Args {
		got, want := a.ExprType(), params[i]
		if !got.Equal(want) && !(want == String && got == Base64) {
			return fmt.Errorf("%s's argument %d is a %s, not a %s", sig.Name, i+1, got, want)
		}
	}
	return nil
}

func checkConcat(x *Concat) error {
	for i, p := range x.Parts {
		if !stringLike(p.ExprType()) {
			return fmt.Errorf("part %d of a template is a %s, not a string", i+1, p.ExprType())
		}
	}
	return nil
}

// primitiveOf is t, or a maybe's element: what ToString converts.
func primitiveOf(t Type) Type {
	if t.Kind == KindMaybe && t.Elem != nil {
		return *t.Elem
	}
	return t
}
