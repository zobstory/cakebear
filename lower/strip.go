package lower

import (
	"strings"
	"unicode/utf8"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/scanner"
)

// stripTypes returns the file's source lines with its types blanked as Node
// 24.18's module.stripTypeScriptTypes blanks them in its default strip mode,
// each line without its terminator: what the report of an uncaught error
// quotes, since Node runs a .ts file as its stripped text (M6 plan D6). The
// rules were measured against Node, never recalled, and
// TestStripTypesMatchesNode holds this to every case recorded
// (testdata/gen-strip-goldens.mjs).
//
// Stripping moves no byte, so the blanked file has its source's lines,
// numbered as ir.Span.Line numbers them: V8 ends a line at \n, \r\n, \r,
// U+2028 and U+2029, as the file's ECMA line map does.
//
// It is nil for a file Node refuses (an enum, a namespace holding values,
// the `module` keyword, a parameter property, `<T>x`, `export =`,
// `import x =`), which lowering refuses too, and for one the parser found an
// error in. A module with no lines is reported without excerpts.
func stripTypes(file *ast.SourceFile) []string {
	if len(file.Diagnostics()) > 0 {
		return nil
	}
	s := &stripper{file: file, text: file.Text()}
	s.children(file.AsNode())
	if s.refused {
		return nil
	}
	return s.lines()
}

type stripper struct {
	file *ast.SourceFile
	text string
	// blanks are the ranges blanked, and writes the text put over them
	// after: the `;` and `)` Node moves or adds.
	blanks  [][2]int
	writes  []stripWrite
	refused bool
}

type stripWrite struct {
	pos  int
	text string
}

func (s *stripper) children(n *ast.Node) {
	n.ForEachChild(func(c *ast.Node) bool {
		s.node(c)
		return s.refused
	})
}

func (s *stripper) node(n *ast.Node) {
	if (ast.IsTypeNodeKind(n.Kind) && n.Kind != ast.KindExpressionWithTypeArguments) || n.Kind == ast.KindTypeParameter {
		return // blanked whole, by what holds it
	}
	if s.declaration(n) {
		return
	}
	switch n.Kind {
	case ast.KindVariableDeclaration:
		d := n.AsVariableDeclaration()
		s.token(d.ExclamationToken)
		s.annotation(d.Type)
	case ast.KindParameter:
		if s.parameter(n) {
			return
		}
	case ast.KindPropertyDeclaration, ast.KindMethodDeclaration, ast.KindConstructor,
		ast.KindGetAccessor, ast.KindSetAccessor, ast.KindIndexSignature:
		if ast.IsClassLike(n.Parent) && s.member(n) {
			return
		}
	case ast.KindClassDeclaration, ast.KindClassExpression:
		s.class(n)
	case ast.KindHeritageClause:
		if n.AsHeritageClause().Token == ast.KindImplementsKeyword {
			s.blank(s.pos(n), n.End())
			return
		}
	case ast.KindAsExpression, ast.KindSatisfiesExpression:
		s.assertion(n)
	case ast.KindNonNullExpression:
		s.blank(n.End()-1, n.End())
	case ast.KindCallExpression, ast.KindNewExpression, ast.KindTaggedTemplateExpression,
		ast.KindExpressionWithTypeArguments:
		s.brackets(n.TypeArgumentList())
	case ast.KindImportSpecifier, ast.KindExportSpecifier:
		if n.IsTypeOnly() {
			s.blank(s.pos(n), s.afterComma(n.End()))
			return
		}
	case ast.KindTypeAssertionExpression:
		// Node leaves `<const>x` as it is, a JavaScript syntax error.
		if !isConstType(n.Type()) {
			s.refused = true
			return
		}
	case ast.KindEnumDeclaration, ast.KindImportEqualsDeclaration:
		// The ambient and type-only ones are blanked by declaration.
		s.refused = true
		return
	case ast.KindExportAssignment:
		if n.AsExportAssignment().IsExportEquals {
			s.refused = true
			return
		}
	}
	if ast.IsFunctionLike(n) {
		s.signature(n)
	}
	if !s.refused {
		s.children(n)
	}
}

// declaration blanks or refuses a statement that is only types, reporting
// whether it did either.
func (s *stripper) declaration(n *ast.Node) bool {
	ambient := n.ModifierFlags()&ast.ModifierFlagsAmbient != 0
	switch n.Kind {
	case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
	case ast.KindImportDeclaration:
		if clause := n.AsImportDeclaration().ImportClause; clause == nil || !clause.IsTypeOnly() {
			return false
		}
	case ast.KindImportEqualsDeclaration, ast.KindExportDeclaration:
		if !n.IsTypeOnly() {
			return false
		}
	case ast.KindModuleDeclaration:
		if moduleKeyword(n) {
			s.refused = true // even declared: Node asks for `namespace`
			return true
		}
		if !ambient && n.AsModuleDeclaration().Keyword == ast.KindNamespaceKeyword && !typesOnly(n) {
			s.refused = true
			return true
		}
	case ast.KindFunctionDeclaration:
		// An overload signature, bodyless, is only types too.
		if !ambient && n.Body() != nil {
			return false
		}
	case ast.KindVariableStatement, ast.KindClassDeclaration, ast.KindEnumDeclaration:
		if !ambient {
			return false
		}
	default:
		return false
	}
	s.statement(n)
	return true
}

// typesOnly reports whether a namespace holds nothing but types, which is the
// one kind Node blanks: interfaces, type aliases, and namespaces of the same,
// declared or not.
func typesOnly(n *ast.Node) bool {
	body := n.AsModuleDeclaration().Body
	if body == nil {
		return true
	}
	if body.Kind == ast.KindModuleDeclaration {
		return typesOnly(body) // namespace A.B
	}
	for _, st := range body.Statements() {
		switch {
		case st.Kind == ast.KindInterfaceDeclaration, st.Kind == ast.KindTypeAliasDeclaration:
		case st.Kind == ast.KindModuleDeclaration && !moduleKeyword(st) && typesOnly(st):
		default:
			return false
		}
	}
	return true
}

// moduleKeyword reports whether a module declaration is `module M`, which
// Node refuses, where `module "m"` declares a module.
func moduleKeyword(n *ast.Node) bool {
	d := n.AsModuleDeclaration()
	return d.Keyword == ast.KindModuleKeyword && d.Name().Kind == ast.KindIdentifier
}

// statement blanks a statement that is only types, after its decorators:
// Node leaves `@d` of `@d declare class C {}`.
//
// Where the statement is the body of an if, an else or a loop, Node puts a
// `;` in place of its first character, so the body is an empty statement,
// but not where it follows a label. And where blanking it leaves the next
// line starting with a token that would continue the line before, ( [ + - /
// or a template without substitutions, Node keeps a `;` between them: the
// one before the statement, if that is the token there, even where it ended
// a statement blanked too, or else one in place of the statement's first
// character. The first statement of a file needs none.
func (s *stripper) statement(n *ast.Node) {
	start := s.pos(n)
	if mods := n.ModifierNodes(); len(mods) > 0 && mods[0].Kind == ast.KindDecorator {
		last := mods[0]
		for _, m := range mods {
			if m.Kind == ast.KindDecorator {
				last = m
			}
		}
		start = scanner.SkipTrivia(s.text, last.End())
	}
	s.blank(start, n.End())
	switch n.Parent.Kind {
	case ast.KindIfStatement, ast.KindWhileStatement, ast.KindDoStatement, ast.KindForStatement,
		ast.KindForInStatement, ast.KindForOfStatement:
		s.write(start, ";")
		return
	}
	if n.Pos() == 0 || !s.continued(n.End(), true) {
		return
	}
	if s.text[n.Pos()-1] == ';' {
		s.write(n.Pos()-1, ";")
	} else {
		s.write(start, ";")
	}
}

// assertion blanks `as T` or `satisfies T`, from the end of the expression,
// so a comment before the `as` goes too. Where a line continuing it follows,
// as for a statement, Node puts a `;` in place of the `as`, but not of
// `as const`, and not for + - or /, which TypeScript too takes to continue
// it.
func (s *stripper) assertion(n *ast.Node) {
	s.blank(n.Expression().End(), n.End())
	if !isConstType(n.Type()) && s.continued(n.End(), false) {
		s.write(scanner.SkipTrivia(s.text, n.Expression().End()), ";")
	}
}

// isConstType reports whether t is the `const` of `as const`.
func isConstType(t *ast.Node) bool {
	if t.Kind != ast.KindTypeReference {
		return false
	}
	ref := t.AsTypeReferenceNode()
	return ref.TypeArguments == nil && ref.TypeName.Kind == ast.KindIdentifier && ref.TypeName.Text() == "const"
}

// continued reports whether the token after end starts a line and would
// continue an expression ending the line before: a ( [ or a template
// without substitutions, and, where operators is set, a + - or /.
func (s *stripper) continued(end int, operators bool) bool {
	next := scanner.SkipTrivia(s.text, end)
	if next >= len(s.text) || !lineBreak(s.text[end:next]) {
		return false
	}
	switch scanner.GetScannerForSourceFile(s.file, next).Token() {
	case ast.KindOpenParenToken, ast.KindOpenBracketToken, ast.KindNoSubstitutionTemplateLiteral:
		return true
	case ast.KindPlusToken, ast.KindMinusToken, ast.KindSlashToken, ast.KindSlashEqualsToken:
		return operators
	}
	return false
}

// member blanks a class member that is only types, reporting whether it did:
// an index signature, a `declare` or abstract member, or a bodyless method,
// accessor or constructor, decorators included. Of any other it blanks the
// modifiers, `?` or `!`, and a property's type.
func (s *stripper) member(n *ast.Node) bool {
	bodyless := n.Kind != ast.KindPropertyDeclaration && n.Kind != ast.KindIndexSignature && n.Body() == nil
	if n.Kind == ast.KindIndexSignature || bodyless ||
		n.ModifierFlags()&(ast.ModifierFlagsAmbient|ast.ModifierFlagsAbstract) != 0 {
		s.blank(s.pos(n), n.End())
		return true
	}
	s.modifiers(n)
	s.token(n.PostfixToken())
	if n.Kind == ast.KindPropertyDeclaration {
		s.annotation(n.Type())
		s.accessorLike(n)
	}
	return false
}

// accessorLike puts a `;` where Node does in a property named get, set or
// static with a type and no value, in place of the type's colon: without
// one, JavaScript would read the property and the member after it as an
// accessor or a static member.
func (s *stripper) accessorLike(n *ast.Node) {
	p := n.AsPropertyDeclaration()
	name := n.Name()
	if p.Type == nil || p.Initializer != nil || name.Kind != ast.KindIdentifier {
		return
	}
	if text := name.Text(); text == "get" || text == "set" || text == "static" {
		s.write(p.Type.Pos()-1, ";")
	}
}

// modifiers blanks a class member's public, private, protected, readonly
// and override. Where one comes first, Node puts a `;` in place of it when
// the member's name is computed, it is a generator, or it is named `in` or
// `instanceof`, whatever came before the member. A decorator, static or
// accessor among the modifiers stops that, and async doesn't.
func (s *stripper) modifiers(n *ast.Node) {
	mods := n.ModifierNodes()
	kept := false
	for _, m := range mods {
		switch {
		case blankedModifier(m):
			s.blank(s.pos(m), m.End())
		case m.Kind == ast.KindDecorator, m.Kind == ast.KindStaticKeyword, m.Kind == ast.KindAccessorKeyword:
			kept = true
		}
	}
	if len(mods) == 0 || !blankedModifier(mods[0]) || kept {
		return
	}
	name := n.Name()
	generator := n.Kind == ast.KindMethodDeclaration && n.BodyData().AsteriskToken != nil
	named := name != nil && name.Kind == ast.KindIdentifier && (name.Text() == "in" || name.Text() == "instanceof")
	if generator || named || (name != nil && name.Kind == ast.KindComputedPropertyName) {
		s.write(s.pos(mods[0]), ";")
	}
}

func blankedModifier(m *ast.Node) bool {
	switch m.Kind {
	case ast.KindPublicKeyword, ast.KindPrivateKeyword, ast.KindProtectedKeyword,
		ast.KindReadonlyKeyword, ast.KindOverrideKeyword:
		return true
	}
	return false
}

// class blanks a class's abstract modifier and type parameters; node blanks
// an implements clause. With a decorator before `export`, Node blanks
// `export` in place of `abstract`, and so does this.
func (s *stripper) class(n *ast.Node) {
	mods := n.ModifierNodes()
	var abstract, export *ast.Node
	for _, m := range mods {
		switch m.Kind {
		case ast.KindAbstractKeyword:
			abstract = m
		case ast.KindExportKeyword:
			export = m
		}
	}
	if abstract != nil {
		if export != nil && mods[0].Kind == ast.KindDecorator {
			abstract = export
		}
		s.blank(s.pos(abstract), abstract.End())
	}
	s.brackets(n.ClassLikeData().TypeParameters)
}

// parameter blanks a parameter's `?` and type, or the whole of a `this`
// parameter, comma included, reporting whether it blanked the whole. Node
// takes `this` as one only first. A parameter property it refuses.
func (s *stripper) parameter(n *ast.Node) bool {
	if n.ModifierFlags()&ast.ModifierFlagsParameterPropertyModifier != 0 {
		s.refused = true
		return true
	}
	p := n.AsParameterDeclaration()
	if name := p.Name(); name.Kind == ast.KindIdentifier && name.Text() == "this" && n.Parent.Parameters()[0] == n {
		s.blank(s.pos(n), s.afterComma(n.End()))
		return true
	}
	s.token(p.QuestionToken)
	s.annotation(p.Type)
	return false
}

// signature blanks a function's type parameters and result type. Where a
// line break before an arrow's result type ends would leave its `)` on a
// line before the `=>`, which JavaScript forbids, Node moves the `)` to the
// type's last character.
//
// A line break before an arrow's `=>`, which TypeScript reports as an
// error, Node refuses.
func (s *stripper) signature(n *ast.Node) {
	f := n.FunctionLikeData()
	s.brackets(f.TypeParameters)
	if n.Kind == ast.KindArrowFunction {
		arrow := n.AsArrowFunction().EqualsGreaterThanToken
		if lineBreak(s.text[arrow.Pos():s.pos(arrow)]) {
			s.refused = true
			return
		}
	}
	if f.Type == nil {
		return
	}
	s.annotation(f.Type)
	if n.Kind != ast.KindArrowFunction {
		return
	}
	paren := scanner.SkipTrivia(s.text, f.Parameters.End())
	if !lineBreak(s.text[paren+1 : f.Type.End()]) {
		return
	}
	s.blank(paren, paren+1)
	// Of a character of several bytes, Node writes the `)` over the first
	// and spaces over the rest.
	_, size := utf8.DecodeLastRuneInString(s.text[:f.Type.End()])
	s.write(f.Type.End()-size, ")"+strings.Repeat(" ", size-1))
}

// annotation blanks a type annotation, from its colon: a comment before the
// colon stays, and one between the colon and the type goes.
func (s *stripper) annotation(t *ast.Node) {
	if t != nil {
		s.blank(t.Pos()-1, t.End())
	}
}

// token blanks a one-character token, a `?` or `!`, alone.
func (s *stripper) token(t *ast.Node) {
	if t != nil {
		s.blank(t.End()-1, t.End())
	}
}

// brackets blanks a list of type arguments or parameters with its `<` and
// `>`: the list itself starts after the `<` and ends before the `>`.
func (s *stripper) brackets(list *ast.NodeList) {
	if list != nil {
		s.blank(list.Pos()-1, scanner.SkipTrivia(s.text, list.End())+1)
	}
}

// afterComma is past the comma following end, or end where none does.
func (s *stripper) afterComma(end int) int {
	if next := scanner.SkipTrivia(s.text, end); next < len(s.text) && s.text[next] == ',' {
		return next + 1
	}
	return end
}

// pos is where n's first token starts, after the trivia its range begins
// with.
func (s *stripper) pos(n *ast.Node) int {
	return scanner.SkipTrivia(s.text, n.Pos())
}

func (s *stripper) blank(start, end int) {
	s.blanks = append(s.blanks, [2]int{start, end})
}

func (s *stripper) write(pos int, text string) {
	s.writes = append(s.writes, stripWrite{pos, text})
}
