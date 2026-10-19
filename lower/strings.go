package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// The strings step (~/Desktop/cakebear/m4-strings-plan.md): template
// literals, a string's length and methods, and a primitive's toString and
// toFixed, each an ir.BuiltinCall the runtime gives JavaScript's semantics,
// UTF-16 indices included. The globals (String, Number, parseInt, …) are in
// numbers.go.

// stringMethods are the String members lowered (S6), by name.
var stringMethods = map[string]ir.Builtin{
	"charAt": ir.StrCharAt, "charCodeAt": ir.StrCharCodeAt,
	"includes": ir.StrIncludes, "startsWith": ir.StrStartsWith, "endsWith": ir.StrEndsWith,
	"indexOf": ir.StrIndexOf, "lastIndexOf": ir.StrLastIndexOf,
	"slice": ir.StrSlice, "substring": ir.StrSubstring,
	"toLowerCase": ir.StrToLowerCase, "toUpperCase": ir.StrToUpperCase,
	"trim": ir.StrTrim, "trimStart": ir.StrTrimStart, "trimEnd": ir.StrTrimEnd,
	"trimLeft": ir.StrTrimStart, "trimRight": ir.StrTrimEnd, // the old names, aliases in V8
	"split": ir.StrSplit, "replace": ir.StrReplace, "replaceAll": ir.StrReplaceAll,
	"padStart": ir.StrPadStart, "padEnd": ir.StrPadEnd, "repeat": ir.StrRepeat,
}

const (
	regexpOnly = "takes a RegExp, and RegExp is not supported yet"
	htmlMethod = "is one of the HTML methods, which are not supported"
)

// refusedStringMembers says why each other String member isn't lowered. A
// test holds the two tables to every member the libs declare.
var refusedStringMembers = map[string]string{
	"match": regexpOnly, "matchAll": regexpOnly, "search": regexpOnly,
	"localeCompare":     "needs ICU collation, which cakebear doesn't carry; compare with < and >, which compare as JavaScript does",
	"normalize":         "needs Unicode normalization tables, which the runtime doesn't carry yet",
	"toLocaleLowerCase": "is locale-sensitive; use toLowerCase", "toLocaleUpperCase": "is locale-sensitive; use toUpperCase",
	"at":           "gives undefined past the end, which waits for null and undefined (M5); use charAt",
	"codePointAt":  "gives undefined past the end, which waits for null and undefined (M5)",
	"concat":       "is not supported yet; use + or a template literal",
	"substr":       "is deprecated and not supported; use slice or substring",
	"isWellFormed": "is not supported yet", "toWellFormed": "is not supported yet",
	"anchor": htmlMethod, "big": htmlMethod, "blink": htmlMethod, "bold": htmlMethod, "fixed": htmlMethod,
	"fontcolor": htmlMethod, "fontsize": htmlMethod, "italics": htmlMethod, "link": htmlMethod,
	"small": htmlMethod, "strike": htmlMethod, "sub": htmlMethod, "sup": htmlMethod,
}

// template lowers a template literal: its text, and each substitution
// converted as ToString converts a primitive (S2).
func (l *lowerer) template(n *ast.Node) ir.Expr {
	if n.Kind == ast.KindNoSubstitutionTemplateLiteral {
		return &ir.StringLit{Value: n.Text(), Span: l.span(n)}
	}
	t := n.AsTemplateExpression()
	var parts []ir.Expr
	text := func(lit *ast.Node) {
		if s := lit.Text(); s != "" {
			parts = append(parts, &ir.StringLit{Value: s, Span: l.span(lit)})
		}
	}
	text(t.Head)
	for _, s := range t.TemplateSpans.Nodes {
		span := s.AsTemplateSpan()
		v := l.expr(span.Expression)
		if v == nil {
			return nil
		}
		if v = l.stringOf(span.Expression, v, "a template literal's ${}"); v == nil {
			return nil
		}
		parts = append(parts, v)
		text(span.Literal)
	}
	return &ir.Concat{Parts: parts, Span: l.span(n)}
}

// stringOf is v as a string, ToString applied, where v is a primitive. what
// names the place, for the refusal of anything else.
func (l *lowerer) stringOf(n *ast.Node, v ir.Expr, what string) ir.Expr {
	switch t := v.ExprType(); {
	case t == ir.String || t == ir.Base64:
		return v
	case ir.IsPrimitive(t), t.Kind == ir.KindMaybe && ir.IsPrimitive(*t.Elem):
		// A maybe writes "undefined" or "null" when it is empty (D8).
		return &ir.BuiltinCall{Fn: ir.ToString, Args: []ir.Expr{v}, Typ: ir.String, Span: l.span(n)}
	default:
		l.failf(n, "%s can hold a string, a number or a boolean; a %s is not supported yet (Node writes %s)", what, t, nodeWrites(t))
		return nil
	}
}

// nodeWrites is what ToString gives for a value of type t, for a refusal.
func nodeWrites(t ir.Type) string {
	switch t.Kind {
	case ir.KindArray:
		return "its elements joined with commas"
	case ir.KindFunc:
		return "its source text"
	case ir.KindPromise:
		return "[object Promise]"
	case ir.KindMaybe:
		if t.Elem != nil {
			return nodeWrites(*t.Elem) + ", or undefined or null"
		}
	}
	return "[object Object]"
}

// stringProperty lowers `s.length`, and reports whether n was a member of a
// string.
func (l *lowerer) stringProperty(n *ast.Node) (ir.Expr, bool) {
	access := n.AsPropertyAccessExpression()
	name, ok := l.stringMember(access)
	switch {
	case !ok:
		return nil, false
	case name == "":
		return nil, true
	case name != "length":
		l.failf(n, "a string's %s can only be called", name)
		return nil, true
	}
	s := l.expr(access.Expression)
	if s == nil {
		return nil, true
	}
	return &ir.BuiltinCall{Fn: ir.StrLength, Args: []ir.Expr{s}, Typ: ir.Number, Span: l.span(n)}, true
}

// stringMember is the name of access's member when its receiver is a string,
// and ok false when it isn't one. A member added to String outside the libs
// has nothing behind it at run time: it is refused, and name is "".
func (l *lowerer) stringMember(access *ast.PropertyAccessExpression) (name string, ok bool) {
	if t := l.typeOf(access.Expression); t != ir.String && t != ir.Base64 {
		return "", false
	}
	if sym := l.checker.GetSymbolAtLocation(access.Name()); !onlyInBundledLib(sym, "") {
		l.failf(access.Name(), "%s is added to String outside TypeScript's libs, which is not supported", access.Name().Text())
		return "", true
	}
	return access.Name().Text(), true
}

// stringCall lowers a call of a string's method, and reports whether c was
// one.
func (l *lowerer) stringCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	access := c.Expression.AsPropertyAccessExpression()
	name, ok := l.stringMember(access)
	switch {
	case !ok:
		return nil, false
	case name == "":
		return nil, true
	case name == "toString" || name == "valueOf":
		return l.expr(access.Expression), true
	}
	fn, lowered := stringMethods[name]
	if !lowered {
		if why, refused := refusedStringMembers[name]; refused {
			l.failf(n, "a string's %s %s", name, why)
		} else {
			l.failf(n, "a string's %s is not supported yet", name)
		}
		return nil, true
	}
	return l.builtin(n, fn, access.Expression, callArgs(c)), true
}

// builtin lowers a call of fn with recv, when there is one, as its first
// argument, each argument held to the type fn's signature gives it. A RegExp
// or a function where a string is wanted is refused by name.
func (l *lowerer) builtin(n *ast.Node, fn ir.Builtin, recv *ast.Node, args []*ast.Node) ir.Expr {
	nodes := args
	if recv != nil {
		nodes = append([]*ast.Node{recv}, args...)
	}
	sig := fn.Sig()
	params := append(append([]ir.Type(nil), sig.Params...), sig.Optional...)
	if len(nodes) < len(sig.Params) || len(nodes) > len(params) {
		l.invalidf(n, "%s takes %d to %d arguments, not %d", fn, len(sig.Params), len(params), len(nodes))
		return nil
	}
	out := &ir.BuiltinCall{Fn: fn, Typ: sig.Result, Span: l.span(n)}
	for i, a := range nodes {
		if a.Kind == ast.KindSpreadElement {
			l.failf(a, "spreading arguments into %s is not supported yet", fn)
			return nil
		}
		if l.isRegExp(a) {
			l.failf(a, "%s with a RegExp is not supported yet; RegExp is not supported yet", fn)
			return nil
		}
		v := l.expr(a)
		if v == nil {
			return nil
		}
		switch got, want := v.ExprType(), params[i]; {
		case got.Equal(want), want == ir.String && got == ir.Base64:
		case got.Kind == ir.KindFunc:
			l.failf(a, "%s with a function argument is not supported yet; pass a %s", fn, want)
			return nil
		default:
			l.failf(a, "%s's argument is a %s here; only a %s is supported yet", fn, got, want)
			return nil
		}
		out.Args = append(out.Args, v)
	}
	return out
}

// isRegExp reports whether n is a RegExp, the libs' own.
func (l *lowerer) isRegExp(n *ast.Node) bool {
	if n.Kind == ast.KindRegularExpressionLiteral {
		return true
	}
	sym := l.checker.GetTypeAtLocation(n).Symbol()
	return sym != nil && sym.Name() == "RegExp" && onlyInBundledLib(sym, "")
}

// primitiveCall lowers toString, toFixed and valueOf on a number or a
// boolean, and reports whether c was a call of a primitive's method.
func (l *lowerer) primitiveCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	if c.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, false
	}
	access := c.Expression.AsPropertyAccessExpression()
	t := l.typeOf(access.Expression)
	if t == ir.String || t == ir.Base64 || !ir.IsPrimitive(t) {
		return nil, false
	}
	name := access.Name().Text()
	if sym := l.checker.GetSymbolAtLocation(access.Name()); !onlyInBundledLib(sym, "") {
		l.failf(access.Name(), "%s is added outside TypeScript's libs, which is not supported", name)
		return nil, true
	}
	args := callArgs(c)
	switch {
	case name == "toString" && len(args) == 0:
		recv := l.expr(access.Expression)
		if recv == nil {
			return nil, true
		}
		return &ir.BuiltinCall{Fn: ir.ToString, Args: []ir.Expr{recv}, Typ: ir.String, Span: l.span(n)}, true
	case name == "toString":
		l.failf(n, "toString with a radix is not supported yet")
	case name == "valueOf":
		return l.expr(access.Expression), true
	case name == "toFixed" && t == ir.Number:
		if len(args) == 0 { // toFixed() is toFixed(0)
			recv := l.expr(access.Expression)
			if recv == nil {
				return nil, true
			}
			return &ir.BuiltinCall{Fn: ir.NumToFixed, Args: []ir.Expr{recv, &ir.NumberLit{Span: l.span(n)}}, Typ: ir.String, Span: l.span(n)}, true
		}
		return l.builtin(n, ir.NumToFixed, access.Expression, args), true
	case name == "toFixed":
		l.failf(n, "toFixed on a %s is not supported yet; it needs a number", t)
	default:
		l.failf(n, "a %s's %s is not supported yet", t, name)
	}
	return nil, true
}

// callArgs is c's arguments.
func callArgs(c *ast.CallExpression) []*ast.Node {
	if c.Arguments == nil {
		return nil
	}
	return c.Arguments.Nodes
}
