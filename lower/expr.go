package lower

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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
			l.failf(n, "numeric literal %s cannot be represented", n.Text())
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
		l.failf(n, "null is not supported yet; the backend has no nullable representation")
		return nil

	case ast.KindIdentifier:
		if n.Text() == "undefined" {
			l.failf(n, "undefined is not supported yet; the backend has no nullable representation")
			return nil
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
		if t == ir.Invalid {
			l.failf(n, "%s has a type the backend cannot represent yet", n.Text())
			return nil
		}
		name, module := l.ref(n)
		return &ir.Ident{Name: name, Module: module, Typ: t, Span: l.span(n)}

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

	case ast.KindArrowFunction, ast.KindFunctionExpression:
		return l.closure(n)

	case ast.KindPropertyAccessExpression:
		return l.property(n)

	case ast.KindAwaitExpression:
		return l.await(n)

	case ast.KindNewExpression:
		return l.newExpr(n)

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
	case ast.KindExclamationToken:
		op = ir.OpNot
		if operand.ExprType() != ir.Boolean {
			l.failf(n, "! needs a boolean, got %s", operand.ExprType())
			return nil
		}
	case ast.KindPlusToken:
		// Unary plus is identity on a number and a coercion otherwise; only
		// the identity case is representable today.
		if !operand.ExprType().IsNumeric() {
			l.failf(n, "unary plus as a coercion is not supported yet")
			return nil
		}
		return operand
	default:
		l.failf(n, "unary %s is not supported yet", describeKind(u.Operator))
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
			l.failf(n, "loose equality is not supported; use === or !==")
		default:
			l.failf(n, "operator %s is not supported yet", describeKind(b.OperatorToken.Kind))
		}
		return nil
	}

	lt, rt := left.ExprType(), right.ExprType()
	// Equal, not ==: two function types built separately differ as pointers.
	if !lt.Equal(rt) {
		// Mixed-type `+` is legal TypeScript (`"a" + 1` is a string) but needs
		// a conversion node the IR does not have yet. Refusing beats emitting
		// Go that would not compile.
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

	// console.log is an intrinsic rather than a method call: the subset has no
	// object model, no `console` binding and no property access, so there is
	// nothing to resolve it against. See ir.ConsoleLog.
	if isConsoleLog(c.Expression) {
		if c.Arguments == nil || len(c.Arguments.Nodes) != 1 {
			l.failf(n, "console.log takes exactly one argument in this phase")
			return nil
		}
		arg := l.expr(c.Arguments.Nodes[0])
		if arg == nil {
			return nil
		}
		switch t := arg.ExprType(); {
		case t == ir.Invalid:
			l.failf(n, "console.log cannot print this type yet")
			return nil
		case t.Kind == ir.KindFunc:
			l.failf(n, "console.log cannot print a function yet")
			return nil
		case t.Kind == ir.KindHost:
			l.failf(n, "console.log cannot print a %s yet", t)
			return nil
		case t.Kind == ir.KindPromise:
			l.failf(n, "console.log cannot print a promise yet; log what it resolves to, with await")
			return nil
		}
		return &ir.ConsoleLog{Arg: arg, Span: l.span(n)}
	}

	if out, handled := l.hostCall(n, c); handled {
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
	if l.resolvesWithPromise(n, call) {
		return nil
	}
	return call
}

// libraryName reports whether n names something declared only outside the
// program's modules: in TypeScript's libs (parseInt, NaN, setTimeout), one of
// cakebear's bundled files, or a file the entry module doesn't import. Those
// have Go behind them only where lowering maps them explicitly (console.log,
// node:http, the extension conversions, all checked before this), so a plain
// reference would emit an undefined Go name and fail inside go build. An
// import resolves to the module that exports it.
func (l *lowerer) libraryName(n *ast.Node) bool {
	sym := l.p.resolve(l.checker.GetSymbolAtLocation(n))
	if sym == nil || len(sym.Declarations()) == 0 {
		return false
	}
	for _, d := range sym.Declarations() {
		if l.p.inProgram(d) {
			return false // the program's own, such as a function named parseInt
		}
	}
	return true
}

// failOutside refuses at, which uses name, declared outside the program's
// modules: what is a library global or function, unless name is declared in
// a TypeScript file of the user's own that the entry module doesn't import,
// such as a script whose top-level names are global.
func (l *lowerer) failOutside(at, name *ast.Node, what string) {
	if sym := l.p.resolve(l.checker.GetSymbolAtLocation(name)); sym != nil {
		for _, d := range sym.Declarations() {
			if f := ast.GetSourceFileOfNode(d); f != nil && !f.IsDeclarationFile {
				l.failf(at, "%s is declared in %s, which the entry module doesn't import; cakebear compiles only the modules it imports",
					name.Text(), filepath.Base(string(f.FileName())))
				return
			}
		}
	}
	l.failf(at, "%s is a library %s cakebear does not implement yet", name.Text(), what)
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
	value := l.expr(argNode)
	if value == nil {
		return nil
	}

	// Validate a literal argument now rather than letting it wrap silently at
	// runtime. This is the part a plain number or string could not do, and the
	// reason these types are worth having at all.
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
