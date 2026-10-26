package lower

import (
	"sort"
	"strconv"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/scanner"
	"github.com/zobstory/cakebear/ir"
	"github.com/zobstory/cakebear/types"
)

// expr lowers an expression, returning nil when it could not be lowered. A nil
// return always means a diagnostic was recorded.
func (l *lowerer) expr(n *ast.Node) ir.Expr {
	if v := l.exprOf(n); v != nil {
		return l.fit(n, v)
	}
	return nil
}

// exprOf lowers n, for expr, which then holds it to its slot.
func (l *lowerer) exprOf(n *ast.Node) ir.Expr {
	if v, ok := l.condExpr(n); ok { // cond.go
		return v
	}
	if v, ok := l.presence(n); ok { // optional.go
		return v
	}
	switch n.Kind {
	case ast.KindNumericLiteral:
		v, err := strconv.ParseFloat(n.Text(), 64)
		if err != nil {
			l.failf(n, "numeric literal %s cannot be represented", n.Text())
			return nil
		}
		return &ir.NumberLit{Value: v, Span: l.span(n)}

	case ast.KindStringLiteral:
		return &ir.StringLit{Value: n.Text(), Span: l.span(n)}

	case ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression:
		return l.template(n)

	case ast.KindTrueKeyword:
		return &ir.BoolLit{Value: true, Span: l.span(n)}

	case ast.KindFalseKeyword:
		return &ir.BoolLit{Value: false, Span: l.span(n)}

	case ast.KindNullKeyword:
		return &ir.NullLit{Span: l.span(n)} // a maybe's slot makes it an Empty (fit)

	case ast.KindIdentifier:
		if n.Text() == "undefined" {
			return &ir.UndefinedLit{Span: l.span(n)}
		}
		// A bundled function such as createServer is not a Go binding.
		if m, found, _ := l.hostMemberOf(l.checker.GetSymbolAtLocation(n)); found {
			l.failHostValue(n, m)
			return nil
		}
		if l.libraryName(n) {
			l.failOutside(n, n, "global")
			return nil
		}
		t := l.typeOf(n)
		stored, isMaybe := l.stored(n) // a maybe or an unknown, narrowed below
		if t == ir.Invalid && !isMaybe {
			l.failf(n, "%s has a type the backend cannot represent yet%s", n.Text(), l.reason())
			return nil
		}
		name, module := l.ref(n)
		if isMaybe {
			read := &ir.Ident{Name: name, Module: module, Stable: l.stable(n), Typ: stored, Span: l.span(n)}
			return narrowed(read, t, l.span(n))
		}
		return &ir.Ident{Name: name, Module: module, Stable: l.stable(n), Appended: l.appended(n), Typ: t, Span: l.span(n)}

	case ast.KindParenthesizedExpression:
		// Parentheses carry no meaning past parsing; the emitter parenthesises
		// every binary expression anyway, so precedence cannot be lost.
		return l.expr(n.AsParenthesizedExpression().Expression)

	case ast.KindPrefixUnaryExpression:
		return l.unary(n)

	case ast.KindPostfixUnaryExpression:
		l.failUpdateValue(n, scanner.TokenToString(n.AsPostfixUnaryExpression().Operator))
		return nil

	case ast.KindBinaryExpression:
		return l.binary(n)

	case ast.KindCallExpression:
		return l.call(n)

	case ast.KindArrowFunction, ast.KindFunctionExpression:
		return l.closure(n)

	case ast.KindPropertyAccessExpression:
		return l.property(n)

	case ast.KindAwaitExpression:
		return l.await(n)

	case ast.KindNewExpression:
		return l.newExpr(n)

	case ast.KindObjectLiteralExpression:
		return l.objectLit(n)

	case ast.KindArrayLiteralExpression:
		return l.arrayLit(n)

	case ast.KindElementAccessExpression:
		return l.elementGet(n)

	default:
		l.failf(n, "%s is not supported yet", describeKind(n.Kind))
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
			l.failf(n, "unary minus needs a number, got %s", operand.ExprType())
			return nil
		}
		// Fold negation of a literal into the literal. This is not an
		// optimisation: `-0` must reach the backend as a negative-zero value,
		// because Go's constant arithmetic would fold the emitted `-0.0` back
		// to positive zero and silently lose the sign.
		if lit, ok := operand.(*ir.NumberLit); ok {
			return &ir.NumberLit{Value: -lit.Value, Span: l.span(n)}
		}
		operand = l.negOperand(n, operand)
	case ast.KindExclamationToken:
		op, operand = ir.OpNot, toBoolean(operand) // truthy.go
	case ast.KindPlusToken:
		// Unary plus is identity on a number and a coercion otherwise; only
		// the identity case is representable today.
		if !operand.ExprType().IsNumeric() {
			l.failf(n, "unary plus as a coercion is not supported yet")
			return nil
		}
		return operand
	case ast.KindPlusPlusToken, ast.KindMinusMinusToken:
		l.failUpdateValue(n, scanner.TokenToString(u.Operator))
		return nil
	default:
		l.failf(n, "unary %s is not supported yet", describeKind(u.Operator))
		return nil
	}

	return &ir.Unary{Op: op, Operand: operand, Typ: operand.ExprType(), Span: l.span(n)}
}

func (l *lowerer) binary(n *ast.Node) ir.Expr {
	b := n.AsBinaryExpression()
	if b.OperatorToken.Kind == ast.KindEqualsToken {
		// An assignment statement is recognised before it gets here
		// (exprStatement), so this one is a value, or in parentheses.
		switch ast.SkipParentheses(b.Left).Kind {
		case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression:
			l.failf(n, "destructuring assignment is not supported yet; declare the names with const or let")
		default:
			l.failf(n, "an assignment used as a value is not supported yet; write it as a statement of its own")
		}
		return nil
	}

	if out, ok := l.errorBinary(n); ok { // instanceof and typeof (errors.go)
		return out
	}

	left := l.expr(b.Left)
	right := l.expr(b.Right)
	if left == nil || right == nil {
		return nil
	}
	if out, ok := l.nullishCompare(n, b.OperatorToken.Kind, left, right); ok {
		return out
	}

	op, ok := binaryOps[b.OperatorToken.Kind]
	if !ok {
		switch b.OperatorToken.Kind {
		case ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:
			l.failf(n, "loose equality is not supported; use === or !==")
		case ast.KindAsteriskAsteriskToken:
			l.failf(n, "** is not supported yet: it waits for Math, whose functions must match V8's to the last bit, which Go's math.Pow doesn't")
		default:
			l.failf(n, "operator %s is not supported yet", describeKind(b.OperatorToken.Kind))
		}
		return nil
	}
	left, right = l.numericOperands(n, op, left, right)
	if left, right, ok = l.maybeOperands(n, op, left, right); !ok {
		return nil
	}

	lt, rt := left.ExprType(), right.ExprType()
	// Equal, not ==: two function types built separately differ as pointers.
	if !lt.Equal(rt) {
		// Mixed-type `+` is legal TypeScript (`"a" + 1` is a string). Real
		// code converts with a template literal instead (10 uses of mixed
		// `+` in wx-next's 3,756 files), so that is what the refusal offers.
		if b.OperatorToken.Kind == ast.KindPlusToken && (lt == ir.String || rt == ir.String) {
			l.failf(n, "+ between %s and %s is not supported yet; write a template literal, `${a}${b}`", lt, rt)
			return nil
		}
		l.failf(n, "%s between %s and %s is not supported yet; both operands must have the same type", op, lt, rt)
		return nil
	}

	typ, ok := resultType(op, lt)
	if !ok {
		l.failf(n, "%s is not supported on %s", op, lt)
		return nil
	}

	return &ir.Binary{Op: op, Left: left, Right: right, Typ: typ, Span: l.span(n)}
}

var binaryOps = map[ast.Kind]ir.BinaryOp{
	ast.KindPlusToken:                    ir.OpAdd,
	ast.KindMinusToken:                   ir.OpSub,
	ast.KindAsteriskToken:                ir.OpMul,
	ast.KindSlashToken:                   ir.OpDiv,
	ast.KindPercentToken:                 ir.OpMod,
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

	case ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod:
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
		// Go cannot compare func values, and a host value's identity is the
		// runtime's business, so === is for primitives only.
		if operand.Kind == ir.KindFunc || operand.Kind == ir.KindHost {
			return ir.Invalid, false
		}
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

	// console.log is an intrinsic rather than a method call (ir.ConsoleLog).
	if isConsoleLog(c.Expression) {
		return l.consoleLog(n, c)
	}

	if out, handled := l.hostCall(n, c); handled {
		return out
	}
	if out, handled := l.arrayCall(n, c); handled {
		return out
	}
	if out, handled := l.jsonCall(n, c); handled {
		return out
	}
	if out, handled := l.errorCall(n, c); handled {
		return out
	}
	if out, handled := l.globalCall(n, c); handled {
		return out
	}
	if out, handled := l.stringCall(n, c); handled {
		return out
	}
	if out, handled := l.primitiveCall(n, c); handled {
		return out
	}
	if out, handled := l.timerCall(n, c); handled {
		return out
	}
	if out, handled := l.spawnCall(n, c); handled {
		return out
	}
	if l.promiseMemberCall(n, c) {
		return nil
	}

	// A callee is a name: a top-level function, or a parameter or variable
	// holding a function value. Go spells calls to both the same way.
	if c.Expression.Kind != ast.KindIdentifier {
		l.failf(n, "only calls to a function by name are supported yet")
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

	if l.libraryName(c.Expression) {
		l.failOutside(n, c.Expression, "function")
		return nil
	}

	if _, maybe := l.storedMaybe(c.Expression); maybe {
		l.failf(n, "calling a function that may be null or undefined is not supported yet")
		return nil
	}
	callee, module := l.ref(c.Expression)
	call := &ir.Call{Callee: callee, Module: module, Typ: l.typeOf(n), Span: l.span(n)}
	if c.Arguments != nil {
		for _, a := range c.Arguments.Nodes {
			lowered := l.expr(a)
			if lowered == nil {
				return nil
			}
			call.Args = append(call.Args, lowered)
		}
	}
	// A slot the checker types as no maybe, as resolve's `T | PromiseLike<T>`
	// is, still takes the callee's parameter type.
	if ft := l.typeOf(c.Expression); ft.Kind == ir.KindFunc && ft.Sig != nil {
		call.Args = fillArgs(call.Args, ft.Sig.Params, l.span(n)) // params.go
	}
	if l.resolvesWithPromise(n, call) {
		return nil
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
	for _, decl := range symbol.Declarations() {
		if file := ast.GetSourceFileOfNode(decl); file != nil && string(file.FileName()) == types.DeclarationPath() {
			return callee.Text()
		}
	}
	return ""
}

// convert lowers a call to one of cakebear's conversion functions.
func (l *lowerer) convert(n *ast.Node, c *ast.CallExpression, brand string) ir.Expr {
	target := extensionType(brand)
	if target == ir.Invalid {
		l.failf(n, "%s is not a type the backend can represent yet", brand)
		return nil
	}
	if c.Arguments == nil || len(c.Arguments.Nodes) != 1 {
		l.failf(n, "%s takes exactly one argument", brand)
		return nil
	}

	argNode := c.Arguments.Nodes[0]
	unmark := l.markNative(argNode, target)
	value := l.expr(argNode)
	unmark()
	if value == nil {
		return nil
	}
	value = unwidened(value)

	// Validate a literal argument now rather than letting it wrap silently at
	// runtime. This is the part a plain number or string could not do, and the
	// reason these types are worth having at all.
	if msg := exactLiteral(argNode, target, value); msg != "" {
		l.invalidf(argNode, "%s", msg)
		return nil
	}
	if msg := l.validateLiteral(brand, value); msg != "" {
		l.invalidf(argNode, "%s", msg)
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
