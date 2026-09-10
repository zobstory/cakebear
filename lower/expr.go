package lower

import (
	"strconv"
	"strings"

	"sort"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/ir"
	"github.com/zobstory/cakebear/types"
)

// expr lowers an expression, returning nil when it could not be lowered. A nil
// return always means a diagnostic was recorded.
func (l *lowerer) expr(n *ast.Node) ir.Expr {
	switch n.Kind {
	case ast.KindNumericLiteral:
		v, err := strconv.ParseFloat(n.Text(), 64)
		if err != nil {
			l.fail(n, "numeric literal %s cannot be represented", n.Text())
			return nil
		}
		return &ir.NumberLit{Value: v, Span: l.span(n)}

	case ast.KindStringLiteral:
		return &ir.StringLit{Value: n.Text(), Span: l.span(n)}

	case ast.KindTrueKeyword:
		return &ir.BoolLit{Value: true, Span: l.span(n)}

	case ast.KindFalseKeyword:
		return &ir.BoolLit{Value: false, Span: l.span(n)}

	case ast.KindNullKeyword:
		l.fail(n, "null is not supported yet; the backend has no nullable representation")
		return nil

	case ast.KindIdentifier:
		if n.Text() == "undefined" {
			l.fail(n, "undefined is not supported yet; the backend has no nullable representation")
			return nil
		}
		t := l.typeOf(n)
		if t == ir.Invalid {
			l.fail(n, "%s has a type the backend cannot represent yet", n.Text())
			return nil
		}
		return &ir.Ident{Name: n.Text(), Typ: t, Span: l.span(n)}

	case ast.KindParenthesizedExpression:
		// Parentheses carry no meaning past parsing; the emitter parenthesises
		// every binary expression anyway, so precedence cannot be lost.
		return l.expr(n.AsParenthesizedExpression().Expression)

	case ast.KindPrefixUnaryExpression:
		return l.unary(n)

	case ast.KindBinaryExpression:
		return l.binary(n)

	case ast.KindCallExpression:
		return l.call(n)

	default:
		l.fail(n, "%s is not supported yet", describeKind(n.Kind))
		return nil
	}
}

func (l *lowerer) unary(n *ast.Node) ir.Expr {
	u := n.AsPrefixUnaryExpression()
	operand := l.expr(u.Operand)
	if operand == nil {
		return nil
	}

	var op ir.UnaryOp
	switch u.Operator {
	case ast.KindMinusToken:
		op = ir.OpNeg
		if !operand.ExprType().IsNumeric() {
			l.fail(n, "unary minus needs a number, got %s", operand.ExprType())
			return nil
		}
		// Fold negation of a literal into the literal. This is not an
		// optimisation: `-0` must reach the backend as a negative-zero value,
		// because Go's constant arithmetic would fold the emitted `-0.0` back
		// to positive zero and silently lose the sign.
		if lit, ok := operand.(*ir.NumberLit); ok {
			return &ir.NumberLit{Value: -lit.Value, Span: l.span(n)}
		}
	case ast.KindExclamationToken:
		op = ir.OpNot
		if operand.ExprType() != ir.Boolean {
			l.fail(n, "! needs a boolean, got %s", operand.ExprType())
			return nil
		}
	case ast.KindPlusToken:
		// Unary plus is identity on a number and a coercion otherwise; only
		// the identity case is representable today.
		if !operand.ExprType().IsNumeric() {
			l.fail(n, "unary plus as a coercion is not supported yet")
			return nil
		}
		return operand
	default:
		l.fail(n, "unary %s is not supported yet", describeKind(u.Operator))
		return nil
	}

	return &ir.Unary{Op: op, Operand: operand, Typ: operand.ExprType(), Span: l.span(n)}
}

func (l *lowerer) binary(n *ast.Node) ir.Expr {
	b := n.AsBinaryExpression()

	left := l.expr(b.Left)
	right := l.expr(b.Right)
	if left == nil || right == nil {
		return nil
	}

	op, ok := binaryOps[b.OperatorToken.Kind]
	if !ok {
		switch b.OperatorToken.Kind {
		case ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:
			l.fail(n, "loose equality is not supported; use === or !==")
		default:
			l.fail(n, "operator %s is not supported yet", describeKind(b.OperatorToken.Kind))
		}
		return nil
	}

	lt, rt := left.ExprType(), right.ExprType()
	if lt != rt {
		// Mixed-type `+` is legal TypeScript (`"a" + 1` is a string) but needs
		// a conversion node the IR does not have yet. Refusing beats emitting
		// Go that would not compile.
		l.fail(n, "%s between %s and %s is not supported yet; both operands must have the same type", op, lt, rt)
		return nil
	}

	typ, ok := resultType(op, lt)
	if !ok {
		l.fail(n, "%s is not supported on %s", op, lt)
		return nil
	}

	return &ir.Binary{Op: op, Left: left, Right: right, Typ: typ, Span: l.span(n)}
}

var binaryOps = map[ast.Kind]ir.BinaryOp{
	ast.KindPlusToken:                    ir.OpAdd,
	ast.KindMinusToken:                   ir.OpSub,
	ast.KindAsteriskToken:                ir.OpMul,
	ast.KindSlashToken:                   ir.OpDiv,
	ast.KindLessThanToken:                ir.OpLess,
	ast.KindLessThanEqualsToken:          ir.OpLessEq,
	ast.KindGreaterThanToken:             ir.OpGreater,
	ast.KindGreaterThanEqualsToken:       ir.OpGreaterEq,
	ast.KindEqualsEqualsEqualsToken:      ir.OpEqual,
	ast.KindExclamationEqualsEqualsToken: ir.OpNotEqual,
	ast.KindAmpersandAmpersandToken:      ir.OpAnd,
	ast.KindBarBarToken:                  ir.OpOr,
}

// resultType gives the type of a binary expression, and reports whether the
// operator applies to the operand type at all.
func resultType(op ir.BinaryOp, operand ir.Type) (ir.Type, bool) {
	switch op {
	case ir.OpAdd:
		// The only operator that is both arithmetic and concatenation.
		if operand.IsNumeric() || operand == ir.String || operand == ir.Base64 {
			return operand, true
		}
		return ir.Invalid, false

	case ir.OpSub, ir.OpMul, ir.OpDiv:
		if operand.IsNumeric() {
			return operand, true
		}
		return ir.Invalid, false

	case ir.OpLess, ir.OpLessEq, ir.OpGreater, ir.OpGreaterEq:
		if operand.IsNumeric() || operand == ir.String || operand == ir.Base64 {
			return ir.Boolean, true
		}
		return ir.Invalid, false

	case ir.OpEqual, ir.OpNotEqual:
		return ir.Boolean, true

	case ir.OpAnd, ir.OpOr:
		if operand == ir.Boolean {
			return ir.Boolean, true
		}
		return ir.Invalid, false

	default:
		return ir.Invalid, false
	}
}

func (l *lowerer) call(n *ast.Node) ir.Expr {
	c := n.AsCallExpression()

	// console.log is an intrinsic rather than a method call: the subset has no
	// object model, no `console` binding and no property access, so there is
	// nothing to resolve it against. See ir.ConsoleLog.
	if isConsoleLog(c.Expression) {
		if c.Arguments == nil || len(c.Arguments.Nodes) != 1 {
			l.fail(n, "console.log takes exactly one argument in this phase")
			return nil
		}
		arg := l.expr(c.Arguments.Nodes[0])
		if arg == nil {
			return nil
		}
		if arg.ExprType() == ir.Invalid {
			l.fail(n, "console.log cannot print this type yet")
			return nil
		}
		return &ir.ConsoleLog{Arg: arg, Span: l.span(n)}
	}

	if c.Expression.Kind != ast.KindIdentifier {
		l.fail(n, "only calls to top-level functions are supported yet")
		return nil
	}

	// A conversion to one of cakebear's extension types, e.g. i32(x).
	//
	// Resolved by asking where the callee was declared, not by the call's
	// result type: any user function returning i32 also has a branded result,
	// and treating those as conversions silently replaced the call with a cast.
	// The callee's spelling is not enough either -- a local function named i32
	// would shadow ours -- so the test is whether the symbol comes from
	// cakebear.d.ts.
	if brand := l.conversionBrand(c.Expression); brand != "" {
		return l.convert(n, c, brand)
	}

	call := &ir.Call{Callee: c.Expression.Text(), Typ: l.typeOf(n), Span: l.span(n)}
	if c.Arguments != nil {
		for _, a := range c.Arguments.Nodes {
			lowered := l.expr(a)
			if lowered == nil {
				return nil
			}
			call.Args = append(call.Args, lowered)
		}
	}
	return call
}

// conversionBrand reports which extension a callee converts to, or "" when the
// callee is anything else.
//
// The symbol's declaration has to live in cakebear's virtual declarations file.
// That is the only way to tell our i32 from a user's own function of the same
// name, since both would type-check and both would produce a branded result.
func (l *lowerer) conversionBrand(callee *ast.Node) string {
	if callee.Kind != ast.KindIdentifier || !types.IsExtension(callee.Text()) {
		return ""
	}
	symbol := l.checker.GetSymbolAtLocation(callee)
	if symbol == nil {
		return ""
	}
	for _, decl := range symbol.Declarations {
		if file := ast.GetSourceFileOfNode(decl); file != nil && file.FileName() == types.DeclarationPath() {
			return callee.Text()
		}
	}
	return ""
}

// convert lowers a call to one of cakebear's conversion functions.
func (l *lowerer) convert(n *ast.Node, c *ast.CallExpression, brand string) ir.Expr {
	target := extensionType(brand)
	if target == ir.Invalid {
		l.fail(n, "%s is not a type the backend can represent yet", brand)
		return nil
	}
	if c.Arguments == nil || len(c.Arguments.Nodes) != 1 {
		l.fail(n, "%s takes exactly one argument", brand)
		return nil
	}

	argNode := c.Arguments.Nodes[0]
	value := l.expr(argNode)
	if value == nil {
		return nil
	}

	// Validate a literal argument now rather than letting it wrap silently at
	// runtime. This is the part a plain number or string could not do, and the
	// reason these types are worth having at all.
	if msg := l.validateLiteral(brand, value); msg != "" {
		l.invalid(argNode, "%s", msg)
		return nil
	}

	return &ir.Convert{Value: value, Typ: target, Span: l.span(n)}
}

// validateLiteral range-checks a literal conversion argument. Non-literals are
// the machine's problem at runtime, and Go's conversion rules apply.
func (l *lowerer) validateLiteral(brand string, value ir.Expr) string {
	switch v := value.(type) {
	case *ir.NumberLit:
		return types.ValidateLiteral(brand, v.Value, "", false)
	case *ir.StringLit:
		return types.ValidateLiteral(brand, 0, v.Value, true)
	default:
		return ""
	}
}

func isConsoleLog(n *ast.Node) bool {
	if n.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	access := n.AsPropertyAccessExpression()
	return access.Expression.Kind == ast.KindIdentifier &&
		access.Expression.Text() == "console" &&
		access.Name() != nil &&
		access.Name().Text() == "log"
}

// describeKind names an AST node kind in words a TypeScript author recognises,
// rather than leaking the compiler's internal spelling into a diagnostic.
func describeKind(k ast.Kind) string {
	s := k.String()
	s = strings.TrimPrefix(s, "Kind")
	var out strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte(' ')
		}
		out.WriteRune(r)
	}
	return strings.ToLower(out.String())
}

// lineAndColumn resolves a source position to 0-based line and column.
//
// ECMALineMap holds the offset each line starts at, so the line containing pos
// is the last entry not greater than pos, and the column is the remainder.
func lineAndColumn(file *ast.SourceFile, pos int) (line, col int) {
	starts := file.ECMALineMap()
	if len(starts) == 0 || pos < 0 {
		return 0, 0
	}
	// The first start strictly greater than pos, minus one, is our line.
	i := sort.Search(len(starts), func(i int) bool { return int(starts[i]) > pos })
	if i == 0 {
		return 0, pos
	}
	line = i - 1
	return line, pos - int(starts[line])
}
