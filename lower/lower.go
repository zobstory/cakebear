// Package lower translates the checked TypeScript AST into cakebear IR.
//
// It is the only package that sees both worlds: it imports the forked compiler
// for the AST and the checker, and ir/ for the output. That isolation is
// deliberate -- ir/ stays free of upstream so backend/ can be extracted, and
// this is where the coupling is paid for instead.
//
// The Phase-1 language subset is enforced here, not in the checker. cakec
// type-checks all of TypeScript; this package decides what the backend can
// currently give a representation to, and refuses the rest with a diagnostic
// naming the construct rather than failing somewhere in codegen.
package lower

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/checker"
	"github.com/zobstory/cakebear/internal/scanner"
	"github.com/zobstory/cakebear/ir"
	"github.com/zobstory/cakebear/types"
)

// Kind separates the two very different things lowering can refuse.
type Kind int

const (
	// Unsupported is a construct a later phase will handle. Nothing is wrong
	// with the source; the backend just cannot represent it yet.
	Unsupported Kind = iota
	// Invalid is a genuine error in the source that no future phase will make
	// legal, such as a literal outside its type's range.
	//
	// Conflating the two would be a lie in both directions: it tells someone
	// their correct-but-early code is broken, and tells someone whose value is
	// out of range to wait for a release that will never fix it.
	Invalid
)

// Error is something lowering refused, with a span so the CLI can point at it.
type Error struct {
	Span ir.Span
	Msg  string
	Kind Kind
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Span, e.Msg)
}

type lowerer struct {
	file    *ast.SourceFile
	checker *checker.Checker
	name    string
	errs    []*Error
}

// File lowers one checked source file into a module.
//
// It gathers every unsupported construct rather than stopping at the first, so
// a user porting a file learns the whole gap in one run.
func File(file *ast.SourceFile, c *checker.Checker) (*ir.Module, []*Error) {
	l := &lowerer{
		file:    file,
		checker: c,
		name:    strings.TrimSuffix(filepath.Base(file.FileName()), filepath.Ext(file.FileName())),
	}

	m := &ir.Module{Name: l.name}
	for _, stmt := range file.AsNode().Statements() {
		if stmt.Kind == ast.KindFunctionDeclaration {
			if fn := l.function(stmt); fn != nil {
				m.Funcs = append(m.Funcs, fn)
			}
			continue
		}
		m.Main = append(m.Main, l.stmt(stmt)...)
	}

	if len(l.errs) > 0 {
		return nil, l.errs
	}
	return m, nil
}

func (l *lowerer) span(n *ast.Node) ir.Span {
	// Pos() includes leading trivia, so it points at the end of whatever came
	// before -- a statement on line 2 would report line 1. GetTokenPosOfNode
	// skips the trivia, which is what diagnostics mean by a node's position.
	pos := scanner.GetTokenPosOfNode(n, l.file, false /*includeJSDoc*/)
	end := n.End()
	line, col := lineAndColumn(l.file, pos)
	return ir.Span{
		File:  l.file.FileName(),
		Start: pos,
		End:   end,
		Line:  line + 1,
		Col:   col + 1,
	}
}

// fail records a construct the backend does not support yet.
func (l *lowerer) fail(n *ast.Node, format string, args ...any) {
	l.errs = append(l.errs, &Error{Span: l.span(n), Msg: fmt.Sprintf(format, args...), Kind: Unsupported})
}

// invalid records a genuine error in the source, which no later phase will make
// legal.
func (l *lowerer) invalid(n *ast.Node, format string, args ...any) {
	l.errs = append(l.errs, &Error{Span: l.span(n), Msg: fmt.Sprintf(format, args...), Kind: Invalid})
}

// typeOf maps a checked TypeScript type onto the Phase-1 type surface.
func (l *lowerer) typeOf(n *ast.Node) ir.Type {
	t := l.checker.GetTypeAtLocation(n)
	if t == nil {
		return ir.Invalid
	}

	// cakebear's extensions are checked first: they are intersections whose
	// primitive half would otherwise match below and lose the brand, turning
	// an i32 back into a float64.
	if brand := types.BrandOf(l.checker, t); brand != "" {
		return extensionType(brand)
	}

	flags := t.Flags()
	switch {
	case flags&checker.TypeFlagsNumberLike != 0:
		return ir.Number
	case flags&checker.TypeFlagsStringLike != 0:
		return ir.String
	case flags&checker.TypeFlagsBooleanLike != 0:
		return ir.Boolean
	case flags&checker.TypeFlagsVoid != 0:
		return ir.Void
	case flags&checker.TypeFlagsNull != 0:
		return ir.Null
	case flags&checker.TypeFlagsUndefined != 0:
		return ir.Undefined
	default:
		return ir.Invalid
	}
}

// extensionType maps a brand name onto its IR type.
func extensionType(brand string) ir.Type {
	switch brand {
	case types.I32:
		return ir.Int32
	case types.I64:
		return ir.Int64
	case types.U32:
		return ir.Uint32
	case types.U64:
		return ir.Uint64
	case types.F32:
		return ir.Float32
	case types.Base64:
		return ir.Base64
	default:
		return ir.Invalid
	}
}

func (l *lowerer) function(n *ast.Node) *ir.Func {
	decl := n.AsFunctionDeclaration()

	if decl.Name() == nil {
		l.fail(n, "a function declaration needs a name")
		return nil
	}
	if decl.Body == nil {
		l.fail(n, "function %s has no body; overload signatures are not supported yet", decl.Name().Text())
		return nil
	}

	fn := &ir.Func{Name: decl.Name().Text(), Span: l.span(n)}

	if decl.Parameters != nil {
		for _, p := range decl.Parameters.Nodes {
			param := p.AsParameterDeclaration()
			if param.Name() == nil || param.Name().Kind != ast.KindIdentifier {
				l.fail(p, "destructured parameters are not supported yet")
				continue
			}
			t := l.typeOf(p)
			if t == ir.Invalid {
				l.fail(p, "parameter %s has a type the backend cannot represent yet", param.Name().Text())
				continue
			}
			fn.Params = append(fn.Params, ir.Param{
				Name: param.Name().Text(),
				Type: t,
				Span: l.span(p),
			})
		}
	}

	fn.Result = ir.Void
	if decl.Type != nil {
		fn.Result = l.typeOf(decl.Type)
		if fn.Result == ir.Invalid {
			l.fail(decl.Type, "function %s returns a type the backend cannot represent yet", fn.Name)
		}
	}

	fn.Body = l.block(decl.Body)
	return fn
}

func (l *lowerer) block(n *ast.Node) []ir.Stmt {
	if n == nil {
		return nil
	}
	var out []ir.Stmt
	for _, s := range n.Statements() {
		out = append(out, l.stmt(s)...)
	}
	return out
}

// stmt returns a slice because one TypeScript statement can lower to several:
// `const a = 1, b = 2;` is a single VariableStatement holding two declarations.
func (l *lowerer) stmt(n *ast.Node) []ir.Stmt {
	switch n.Kind {
	case ast.KindVariableStatement:
		return l.varStatement(n)

	case ast.KindExpressionStatement:
		expr := n.AsExpressionStatement().Expression
		// Assignment is an expression in TypeScript and a statement in Go, so
		// it is recognised here rather than in expr().
		if expr.Kind == ast.KindBinaryExpression {
			if bin := expr.AsBinaryExpression(); bin.OperatorToken.Kind == ast.KindEqualsToken {
				return l.assignment(n, bin)
			}
		}
		lowered := l.expr(expr)
		if lowered == nil {
			return nil
		}
		return []ir.Stmt{&ir.ExprStmt{Expr: lowered, Span: l.span(n)}}

	case ast.KindIfStatement:
		s := n.AsIfStatement()
		cond := l.expr(s.Expression)
		if cond == nil {
			return nil
		}
		out := &ir.If{Cond: cond, Then: l.statementBody(s.ThenStatement), Span: l.span(n)}
		if s.ElseStatement != nil {
			out.Else = l.statementBody(s.ElseStatement)
		}
		return []ir.Stmt{out}

	case ast.KindWhileStatement:
		s := n.AsWhileStatement()
		cond := l.expr(s.Expression)
		if cond == nil {
			return nil
		}
		return []ir.Stmt{&ir.While{Cond: cond, Body: l.statementBody(s.Statement), Span: l.span(n)}}

	case ast.KindReturnStatement:
		s := n.AsReturnStatement()
		out := &ir.Return{Span: l.span(n)}
		if s.Expression != nil {
			if out.Value = l.expr(s.Expression); out.Value == nil {
				return nil
			}
		}
		return []ir.Stmt{out}

	case ast.KindBlock:
		return l.block(n)

	case ast.KindEmptyStatement:
		return nil

	default:
		l.fail(n, "%s is not supported yet", describeKind(n.Kind))
		return nil
	}
}

// statementBody normalises `if (c) x;` and `if (c) { x }` to the same shape.
func (l *lowerer) statementBody(n *ast.Node) []ir.Stmt {
	if n == nil {
		return nil
	}
	if n.Kind == ast.KindBlock {
		return l.block(n)
	}
	return l.stmt(n)
}

func (l *lowerer) varStatement(n *ast.Node) []ir.Stmt {
	list := n.AsVariableStatement().DeclarationList
	if list == nil {
		return nil
	}
	declList := list.AsVariableDeclarationList()

	// `var` is hoisted and function-scoped; `const` and `let` are not. Rather
	// than emit something with subtly different scoping, refuse it.
	mutable := declList.Flags&ast.NodeFlagsConst == 0
	if declList.Flags&(ast.NodeFlagsConst|ast.NodeFlagsLet) == 0 {
		l.fail(n, "`var` is not supported; use `const` or `let`")
		return nil
	}

	var out []ir.Stmt
	for _, d := range declList.Declarations.Nodes {
		decl := d.AsVariableDeclaration()
		if decl.Name() == nil || decl.Name().Kind != ast.KindIdentifier {
			l.fail(d, "destructuring is not supported yet")
			continue
		}
		name := decl.Name().Text()
		if decl.Initializer == nil {
			l.fail(d, "%s needs an initialiser; declarations without one are not supported yet", name)
			continue
		}
		t := l.typeOf(decl.Name())
		if t == ir.Invalid {
			l.fail(d, "%s has a type the backend cannot represent yet", name)
			continue
		}
		init := l.expr(decl.Initializer)
		if init == nil {
			continue
		}
		out = append(out, &ir.VarDecl{
			Name: name, Type: t, Init: init, Mutable: mutable, Span: l.span(d),
		})
	}
	return out
}

func (l *lowerer) assignment(stmt *ast.Node, bin *ast.BinaryExpression) []ir.Stmt {
	if bin.Left.Kind != ast.KindIdentifier {
		l.fail(stmt, "only assignment to a simple variable is supported yet")
		return nil
	}
	value := l.expr(bin.Right)
	if value == nil {
		return nil
	}
	return []ir.Stmt{&ir.Assign{
		Name: bin.Left.Text(), Value: value, Span: l.span(stmt),
	}}
}
