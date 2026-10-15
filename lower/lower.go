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

// lowerer lowers one module of a program.
type lowerer struct {
	p       *program
	file    *ast.SourceFile
	checker *checker.Checker
	name    string
	errs    []*Error
	// funcDepth counts function types being mapped, to stop a recursive one.
	funcDepth int
	// discard is set while lowering a function literal that stands in for a
	// void callback, so its return values are evaluated and dropped.
	discard bool
	// async is set while lowering the body of an async function, the only
	// place an await may appear until top-level await lands.
	async bool
	// resolvers holds the type of each Promise executor's resolve parameter,
	// which the checker types as a union the IR cannot hold (async.go).
	resolvers map[*ast.Symbol]ir.Type
	// why says what the type mapped last can't represent, for the message
	// refusing it (object.go); typeDepth tells the outermost mapping, which
	// starts it afresh, from those it makes of the type's parts.
	why       string
	typeDepth int
	// temps counts the names lowering makes up, such as for the object a
	// pattern destructures (destructure.go).
	temps int
}

// module lowers the lowerer's file into a module of the program.
func (l *lowerer) module() *ir.Module {
	m := &ir.Module{Name: l.name, File: string(l.file.FileName())}
	if await := firstTopLevelAwait(l.file); await != nil {
		m.Async = true
		// M3 plan D8: an importer of a module with a top-level await waits for
		// it, and ES interleaves sibling async modules. Not reproduced yet.
		if l.file != l.p.entry {
			l.failf(await, "a top-level await is supported only in the entry module; every module importing this one would have to wait for it")
		}
	}
	for _, stmt := range l.file.AsNode().Statements() {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration:
			if ast.HasSyntacticModifier(stmt, ast.ModifierFlagsDefault) {
				l.failf(stmt, "a default export is not supported yet; export the function by name")
				continue
			}
			if fn := l.function(stmt); fn != nil {
				m.Funcs = append(m.Funcs, fn)
			}
		case ast.KindImportDeclaration:
			if file := l.importDecl(stmt); file != "" {
				m.Imports = append(m.Imports, file)
			}
		case ast.KindExportDeclaration:
			l.exportDecl(stmt)
		case ast.KindExportAssignment:
			l.failf(stmt, "a default export is not supported yet; export the value by name")
		default:
			if stmt.Kind == ast.KindVariableStatement && ast.HasSyntacticModifier(stmt, ast.ModifierFlagsExport) {
				l.exportedVariables(stmt)
			}
			// With a top-level await, top-level code is an async body.
			l.inBody(m.Async, func() { m.Main = append(m.Main, l.stmt(stmt)...) })
		}
	}
	return m
}

func (l *lowerer) span(n *ast.Node) ir.Span {
	// Pos() includes leading trivia, so it points at the end of whatever came
	// before -- a statement on line 2 would report line 1. GetTokenPosOfNode
	// skips the trivia, which is what diagnostics mean by a node's position.
	pos := scanner.GetTokenPosOfNode(n, l.file, false /*includeJSDoc*/)
	end := n.End()
	line, col := lineAndColumn(l.file, pos)
	return ir.Span{
		File:  string(l.file.FileName()),
		Start: pos,
		End:   end,
		Line:  line + 1,
		Col:   col + 1,
	}
}

// failf records a construct the backend does not support yet.
func (l *lowerer) failf(n *ast.Node, format string, args ...any) {
	l.errs = append(l.errs, &Error{Span: l.span(n), Msg: fmt.Sprintf(format, args...), Kind: Unsupported})
}

// invalidf records a genuine error in the source, which no later phase will make
// legal.
func (l *lowerer) invalidf(n *ast.Node, format string, args ...any) {
	l.errs = append(l.errs, &Error{Span: l.span(n), Msg: fmt.Sprintf(format, args...), Kind: Invalid})
}

// typeOf maps the checked type of a node onto the IR's type surface.
func (l *lowerer) typeOf(n *ast.Node) ir.Type {
	if t, ok := l.resolverType(n); ok {
		return t
	}
	return l.typeFromType(l.checker.GetTypeAtLocation(n))
}

// typeFromType maps a checked TypeScript type onto the IR's type surface.
func (l *lowerer) typeFromType(t *checker.Type) ir.Type {
	if t == nil {
		return ir.Invalid
	}
	if l.typeDepth == 0 {
		l.why = ""
	}
	l.typeDepth++
	defer func() { l.typeDepth-- }()

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
		// Promises and host types first: both are objects with members, and
		// funcType would refuse them for having properties.
		if p, ok := l.promiseType(t); ok {
			return p
		}
		if h := l.hostType(t); h != ir.HostNone {
			return ir.HostOf(h)
		}
		switch {
		case flags&checker.TypeFlagsUnion != 0:
			l.because("union types are not supported yet")
			return ir.Invalid
		case flags&checker.TypeFlagsIntersection != 0:
			l.because("intersection types are not supported yet")
			return ir.Invalid
		case l.checker.IsArrayType(t):
			return l.arrayType(t)
		case checker.IsTupleType(t):
			l.because("tuples are not supported yet")
			return ir.Invalid
		case l.isObjectType(t):
			return l.objectType(t)
		}
		return l.funcType(t)
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
		l.failf(n, "a function declaration needs a name")
		return nil
	}
	if decl.Body == nil {
		l.failf(n, "function %s has no body; overload signatures are not supported yet", decl.Name().Text())
		return nil
	}

	fn := &ir.Func{Name: decl.Name().Text(), Span: l.span(n)}
	var prologue []ir.Stmt // what binds destructured parameters

	if decl.Parameters != nil {
		for _, p := range decl.Parameters.Nodes {
			param := p.AsParameterDeclaration()
			switch {
			case param.Name() == nil:
				l.invalidf(p, "a parameter with no name")
				continue
			case param.Name().Kind == ast.KindArrayBindingPattern:
				l.failf(p, "array destructuring is not supported yet")
				continue
			}
			t := l.typeOf(p)
			if t == ir.Invalid {
				l.failf(p, "parameter %s has a type the backend cannot represent yet%s", bindingText(param.Name()), l.reason())
				continue
			}
			name := bindingText(param.Name())
			if param.Name().Kind == ast.KindObjectBindingPattern {
				var bound []ir.Stmt
				var ok bool
				if name, bound, ok = l.paramPattern(p, t); !ok {
					continue
				}
				prologue = append(prologue, bound...)
			}
			fn.Params = append(fn.Params, ir.Param{Name: name, Type: t, Span: l.span(p)})
		}
	}

	fn.Result = ir.Void
	if decl.Type != nil {
		fn.Result = l.typeOf(decl.Type)
		if fn.Result == ir.Invalid {
			l.failf(decl.Type, "function %s returns a type the backend cannot represent yet%s", fn.Name, l.reason())
		}
	}
	if fn.Async = isAsync(n); fn.Async {
		l.asyncResult(n, fn)
	}

	l.inBody(fn.Async, func() { fn.Body = append(prologue, l.block(decl.Body)...) })
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
		return l.exprStatement(n, n.AsExpressionStatement().Expression)

	case ast.KindIfStatement:
		s := n.AsIfStatement()
		cond := l.condition(s.Expression)
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
		cond := l.condition(s.Expression)
		if cond == nil {
			return nil
		}
		return []ir.Stmt{&ir.While{Cond: cond, Body: l.statementBody(s.Statement), Span: l.span(n)}}

	case ast.KindReturnStatement:
		s := n.AsReturnStatement()
		out := &ir.Return{Span: l.span(n)}
		if s.Expression != nil {
			if out.Value = l.expr(s.Expression); out.Value == nil || !l.returnable(n, out.Value) {
				return nil
			}
		}
		if l.discard && out.Value != nil {
			// A void slot: evaluate the value for its effects, then return.
			value := out.Value
			out.Value = nil
			return []ir.Stmt{&ir.ExprStmt{Expr: value, Span: out.Span}, out}
		}
		return []ir.Stmt{out}

	case ast.KindForStatement:
		return l.forStmt(n)

	case ast.KindForOfStatement:
		return l.forOf(n)

	case ast.KindBreakStatement, ast.KindContinueStatement:
		return l.jump(n)

	case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
		// Types alone: erased at runtime, so there is nothing to lower.
		return nil

	case ast.KindBlock:
		return l.block(n)

	case ast.KindEmptyStatement:
		return nil

	default:
		l.failf(n, "%s is not supported yet", describeKind(n.Kind))
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
	return l.declarations(n, list)
}

// declarations lowers a list of const or let declarations, from a statement
// or a for loop's first clause. at is where a refusal of the whole list
// points.
func (l *lowerer) declarations(at, list *ast.Node) []ir.Stmt {
	n := at
	declList := list.AsVariableDeclarationList()

	// `var` is hoisted and function-scoped; `const` and `let` are not. Rather
	// than emit something with subtly different scoping, refuse it.
	mutable := declList.Flags&ast.NodeFlagsConst == 0
	if declList.Flags&(ast.NodeFlagsConst|ast.NodeFlagsLet) == 0 {
		l.failf(n, "`var` is not supported; use `const` or `let`")
		return nil
	}

	var out []ir.Stmt
	for _, d := range declList.Declarations.Nodes {
		decl := d.AsVariableDeclaration()
		if decl.Name() == nil {
			l.invalidf(d, "a declaration with no name")
			continue
		}
		if decl.Name().Kind != ast.KindIdentifier {
			if decl.Initializer == nil {
				l.invalidf(d, "a destructuring declaration with no initialiser")
				continue
			}
			if init := l.expr(decl.Initializer); init != nil {
				topLevel := at.Parent != nil && at.Parent.Kind == ast.KindSourceFile
				out = append(out, l.destructure(d, decl.Name(), init, mutable, topLevel)...)
			}
			continue
		}
		name := decl.Name().Text()
		if decl.Initializer == nil {
			l.failf(d, "%s needs an initialiser; declarations without one are not supported yet", name)
			continue
		}
		t := l.typeOf(decl.Name())
		if t == ir.Invalid {
			if why := l.reason(); !l.explainInitializer(decl.Initializer) {
				l.failf(d, "%s has a type the backend cannot represent yet%s", name, why)
			}
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

// explainInitializer reports why a variable's initialiser cannot be stored,
// for the initialisers that can say more than "has a type the backend cannot
// represent": a function literal (async, a rest parameter, …), one of
// node:http's members used as a value, and a library global such as parseInt.
// It reports whether it said anything.
func (l *lowerer) explainInitializer(init *ast.Node) bool {
	var name *ast.Node
	switch init.Kind {
	case ast.KindArrowFunction, ast.KindFunctionExpression:
		return !l.closureShapeOK(init)
	case ast.KindIdentifier:
		name = init
	case ast.KindPropertyAccessExpression:
		name = init.Name()
	default:
		return false
	}
	if m, found, _ := l.hostMemberOf(l.checker.GetSymbolAtLocation(name)); found {
		l.failHostValue(init, m)
		return true
	}
	if init.Kind == ast.KindIdentifier && l.libraryName(init) {
		l.failOutside(init, init, "global")
		return true
	}
	return false
}

func (l *lowerer) assignment(stmt *ast.Node, bin *ast.BinaryExpression) []ir.Stmt {
	if out, ok := l.fieldSet(stmt, bin.Left, bin.Right); ok {
		return out
	}
	if out, ok := l.arrayAssign(stmt, bin.Left, bin.Right); ok {
		return out
	}
	if bin.Left.Kind != ast.KindIdentifier {
		l.failf(stmt, "only assignment to a simple variable is supported yet")
		return nil
	}
	value := l.expr(bin.Right)
	if value == nil {
		return nil
	}
	name, module := l.ref(bin.Left)
	return []ir.Stmt{&ir.Assign{
		Name: name, Module: module, Value: value, Span: l.span(stmt),
	}}
}
