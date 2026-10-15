package lower

import (
	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
)

// The strings step's globals (S3, S6b): String, Number, the number parsers
// and predicates, and the URI functions. Each is recognised as the global
// TypeScript's libs declare, by where its value is declared, so a program's
// own String or parseInt is an ordinary binding.

// globalFuncs are the global functions lowered, other than String and
// Number, by the builtin each is.
var globalFuncs = map[string]ir.Builtin{
	"parseInt": ir.NumParseInt, "parseFloat": ir.NumParseFlt,
	"isNaN": ir.NumIsNaN, "isFinite": ir.NumIsFinite,
	"encodeURIComponent": ir.URIEncode, "decodeURIComponent": ir.URIDecode,
}

// numberStatics are Number's own functions lowered.
var numberStatics = map[string]ir.Builtin{
	"isFinite": ir.NumIsFinite, "isNaN": ir.NumIsNaN, "isInteger": ir.NumIsInteger,
	"parseInt": ir.NumParseInt, "parseFloat": ir.NumParseFlt,
}

// libGlobal reports whether n names the global value name the libs declare,
// and nothing else: in a script, a program's own `function parseInt` merges
// with the lib's declaration into one symbol, and is then the program's. An
// interface of the same name may be augmented (String's, Number's) and
// doesn't count.
func (l *lowerer) libGlobal(n *ast.Node, name string) bool {
	if n.Kind != ast.KindIdentifier || n.Text() != name {
		return false
	}
	sym := l.checker.GetSymbolAtLocation(n)
	if sym == nil || sym.ValueDeclaration() == nil {
		return false
	}
	for _, d := range sym.Declarations() {
		if d.Kind != ast.KindInterfaceDeclaration && !inBundledLib(d) {
			return false
		}
	}
	return true
}

// globalCall lowers a call of one of the globals above, and reports whether
// c was one.
func (l *lowerer) globalCall(n *ast.Node, c *ast.CallExpression) (ir.Expr, bool) {
	args := callArgs(c)
	callee := c.Expression
	if callee.Kind == ast.KindPropertyAccessExpression {
		access := callee.AsPropertyAccessExpression()
		switch {
		case l.libGlobal(access.Expression, "Number"):
			name := access.Name().Text()
			fn, ok := numberStatics[name]
			if !ok {
				l.failf(n, "Number.%s is not supported yet", name)
				return nil, true
			}
			return l.numberFunc(n, "Number."+name, fn, args), true
		case l.libGlobal(access.Expression, "String"):
			l.failf(n, "String.%s is not supported yet", access.Name().Text())
			return nil, true
		}
		return nil, false
	}
	if callee.Kind != ast.KindIdentifier {
		return nil, false
	}
	name := callee.Text()
	switch {
	case l.libGlobal(callee, "String"):
		return l.stringCtor(n, args), true
	case l.libGlobal(callee, "Number"):
		return l.numberCtor(n, args), true
	}
	if fn, ok := globalFuncs[name]; ok && l.libGlobal(callee, name) {
		return l.numberFunc(n, name, fn, args), true
	}
	return nil, false
}

// stringCtor lowers String(x): "" for no argument, ToString of a primitive.
func (l *lowerer) stringCtor(n *ast.Node, args []*ast.Node) ir.Expr {
	switch len(args) {
	case 0:
		return &ir.StringLit{Span: l.span(n)}
	case 1:
		v := l.expr(args[0])
		if v == nil {
			return nil
		}
		return l.stringOf(args[0], v, "String()")
	}
	l.invalidf(n, "String takes one argument")
	return nil
}

// numberCtor lowers Number(x): 0 for no argument, a number as it is, and a
// string parsed as JavaScript's StringToNumber parses it.
func (l *lowerer) numberCtor(n *ast.Node, args []*ast.Node) ir.Expr {
	switch len(args) {
	case 0:
		return &ir.NumberLit{Span: l.span(n)}
	case 1:
	default:
		l.invalidf(n, "Number takes one argument")
		return nil
	}
	v := l.expr(args[0])
	if v == nil {
		return nil
	}
	switch t := v.ExprType(); t {
	case ir.Number:
		return v
	case ir.String, ir.Base64:
		return &ir.BuiltinCall{Fn: ir.NumFromStr, Args: []ir.Expr{v}, Typ: ir.Number, Span: l.span(n)}
	default:
		l.failf(args[0], "Number() of a %s is not supported yet; only of a string or a number", t)
		return nil
	}
}

// numberFunc lowers a call of a parser, a predicate or a URI function. A
// predicate given something other than a number is refused: the global one
// would convert it first, and Number's would answer false. encodeURIComponent
// converts a number or a boolean to a string first, as JavaScript does.
func (l *lowerer) numberFunc(n *ast.Node, name string, fn ir.Builtin, args []*ast.Node) ir.Expr {
	if fn == ir.URIEncode && len(args) == 1 {
		v := l.expr(args[0])
		if v == nil {
			return nil
		}
		if v = l.stringOf(args[0], v, "encodeURIComponent"); v == nil {
			return nil
		}
		return &ir.BuiltinCall{Fn: fn, Args: []ir.Expr{v}, Typ: ir.String, Span: l.span(n)}
	}
	if len(args) > 0 {
		if t := l.typeOf(args[0]); fn.Sig().Params[0] == ir.Number && t != ir.Number && t != ir.Invalid {
			l.failf(args[0], "%s of a %s is not supported yet; pass a number, such as Number(s)", name, t)
			return nil
		}
	}
	return l.builtin(n, fn, nil, args)
}
