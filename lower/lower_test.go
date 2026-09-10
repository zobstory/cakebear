package lower

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/internal/ast"
	"github.com/zobstory/cakebear/internal/bundled"
	"github.com/zobstory/cakebear/internal/compiler"
	"github.com/zobstory/cakebear/internal/core"
	"github.com/zobstory/cakebear/internal/tsoptions"
	"github.com/zobstory/cakebear/internal/tspath"
	"github.com/zobstory/cakebear/internal/vfs/osvfs"
	"github.com/zobstory/cakebear/ir"
)

// lowerSource type-checks a snippet and lowers it, the way cakec does.
func lowerSource(t *testing.T, src string) (*ir.Module, []*Error) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "t.ts")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	cwd := tspath.NormalizePath(dir)
	fs := bundled.WrapFS(osvfs.FS())
	host := compiler.NewCompilerHost(cwd, fs, bundled.LibPath(), nil, nil)

	config := tsoptions.NewParsedCommandLine(
		&core.CompilerOptions{
			Target: core.ScriptTargetESNext,
			Module: core.ModuleKindESNext,
			Strict: core.TSTrue,
			NoEmit: core.TSTrue,
		},
		[]string{tspath.NormalizePath(path)},
		tspath.ComparePathsOptions{UseCaseSensitiveFileNames: fs.UseCaseSensitiveFileNames(), CurrentDirectory: cwd},
	)

	program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})

	var file *ast.SourceFile
	for _, f := range program.GetSourceFiles() {
		if strings.HasSuffix(f.FileName(), "t.ts") {
			file = f
			break
		}
	}
	if file == nil {
		t.Fatal("source file not found in program")
	}

	ctx := context.Background()
	c, done := program.GetTypeCheckerForFile(ctx, file)
	defer done()
	return File(file, c)
}

func TestLowerFunctionAndCall(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, `
function add(a: number, b: number): number {
  return a + b;
}
const x: number = add(1, 2);
console.log(x);
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected lowering errors: %v", errs)
	}

	if len(m.Funcs) != 1 {
		t.Fatalf("got %d functions, want 1", len(m.Funcs))
	}
	fn := m.Funcs[0]
	if fn.Name != "add" || len(fn.Params) != 2 || fn.Result != ir.Number {
		t.Errorf("lowered function = %+v, want add(number, number) number", fn)
	}
	if fn.Params[0].Type != ir.Number || fn.Params[1].Type != ir.Number {
		t.Errorf("parameter types = %v, %v, want number, number", fn.Params[0].Type, fn.Params[1].Type)
	}
	if len(m.Main) != 2 {
		t.Fatalf("got %d top-level statements, want 2", len(m.Main))
	}
	if _, ok := m.Main[0].(*ir.VarDecl); !ok {
		t.Errorf("first statement is %T, want *ir.VarDecl", m.Main[0])
	}
}

// Spans must survive lowering: a backend that cannot say which line produced a
// fault has to be retrofitted later, and that is miserable.
func TestLoweredNodesCarrySpans(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, "const x: number = 1;\nconsole.log(x);\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	decl := m.Main[0]
	span := decl.StmtSpan()
	if span.Line != 1 {
		t.Errorf("first statement is on line %d, want 1", span.Line)
	}
	if !strings.HasSuffix(span.File, "t.ts") {
		t.Errorf("span file = %q, want it to end in t.ts", span.File)
	}

	second := m.Main[1].StmtSpan()
	if second.Line != 2 {
		t.Errorf("second statement is on line %d, want 2", second.Line)
	}
}

// Unary minus on a literal is folded during lowering, because Go's constant
// arithmetic would fold the emitted -0.0 back to positive zero.
func TestNegativeLiteralIsFolded(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, "const x: number = -5;\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	decl, ok := m.Main[0].(*ir.VarDecl)
	if !ok {
		t.Fatalf("statement is %T, want *ir.VarDecl", m.Main[0])
	}
	lit, ok := decl.Init.(*ir.NumberLit)
	if !ok {
		t.Fatalf("initialiser is %T, want a folded *ir.NumberLit", decl.Init)
	}
	if lit.Value != -5 {
		t.Errorf("folded value = %v, want -5", lit.Value)
	}
}

func TestRefusesUnsupportedConstructs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{"class", "class C { }\n", "class declaration"},
		{"array type", "const a: number[] = [1];\n", "cannot represent yet"},
		{"var", "var x: number = 1;\n", "`var` is not supported"},
		{"loose equality", "const b: boolean = 1 == 1;\n", "loose equality"},
		{"mixed concat", "const s: string = \"a\" + 1;\n", "same type"},
		{"null", "const n: null = null;\n", "null is not supported yet"},
		{"two-arg log", "console.log(1, 2);\n", "exactly one argument"},
		{"for-of", "for (const x of [1]) { }\n", "not supported yet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, errs := lowerSource(t, tt.src)
			if len(errs) == 0 {
				t.Fatalf("lowering %q succeeded, want a refusal mentioning %q", tt.src, tt.want)
			}
			joined := make([]string, len(errs))
			for i, e := range errs {
				joined[i] = e.Error()
			}
			all := strings.Join(joined, "\n")
			if !strings.Contains(all, tt.want) {
				t.Errorf("refusals = %q, want one mentioning %q", all, tt.want)
			}
			for _, e := range errs {
				if e.Span.Line == 0 {
					t.Errorf("refusal %q has no source line", e.Msg)
				}
			}
		})
	}
}

// Every unsupported construct is reported, not just the first, so a user
// porting a file learns the whole gap in one run.
func TestRefusalsAreCollected(t *testing.T) {
	t.Parallel()

	_, errs := lowerSource(t, "class A { }\nclass B { }\nvar x: number = 1;\n")
	if len(errs) < 3 {
		t.Errorf("got %d refusals, want at least 3 — they should be collected, not short-circuited", len(errs))
	}
}

func TestDescribeKind(t *testing.T) {
	t.Parallel()

	if got := describeKind(ast.KindClassDeclaration); got != "class declaration" {
		t.Errorf("describeKind(ClassDeclaration) = %q, want %q", got, "class declaration")
	}
}

func TestLowerControlFlow(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, `
let i: number = 0;
while (i < 3) {
  if (i === 1) {
    console.log("one");
  } else {
    console.log(i);
  }
  i = i + 1;
}
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(m.Main) != 2 {
		t.Fatalf("got %d statements, want 2 (decl, while)", len(m.Main))
	}

	loop, ok := m.Main[1].(*ir.While)
	if !ok {
		t.Fatalf("second statement is %T, want *ir.While", m.Main[1])
	}
	if len(loop.Body) != 2 {
		t.Fatalf("loop body has %d statements, want 2", len(loop.Body))
	}

	branch, ok := loop.Body[0].(*ir.If)
	if !ok {
		t.Fatalf("loop body starts with %T, want *ir.If", loop.Body[0])
	}
	if len(branch.Then) != 1 || len(branch.Else) != 1 {
		t.Errorf("if has %d then / %d else statements, want 1 / 1", len(branch.Then), len(branch.Else))
	}
	if _, ok := loop.Body[1].(*ir.Assign); !ok {
		t.Errorf("loop body ends with %T, want *ir.Assign", loop.Body[1])
	}
}

// An unbraced branch body lowers to the same shape as a braced one, so the
// backend never has to care which the author wrote.
func TestUnbracedBranchLowersLikeABlock(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, "const c: boolean = true;\nif (c) console.log(1);\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	branch, ok := m.Main[1].(*ir.If)
	if !ok {
		t.Fatalf("statement is %T, want *ir.If", m.Main[1])
	}
	if len(branch.Then) != 1 {
		t.Errorf("unbraced then has %d statements, want 1", len(branch.Then))
	}
	if branch.Else != nil {
		t.Errorf("if without else has Else = %v, want nil", branch.Else)
	}
}

// Parentheses carry no meaning past parsing, and the emitter parenthesises
// every binary expression anyway, so precedence cannot be lost by dropping them.
func TestParenthesesAreTransparent(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, "const x: number = (1 + 2) * 3;\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	decl := m.Main[0].(*ir.VarDecl)
	outer, ok := decl.Init.(*ir.Binary)
	if !ok {
		t.Fatalf("initialiser is %T, want *ir.Binary", decl.Init)
	}
	if outer.Op != ir.OpMul {
		t.Errorf("outer operator = %v, want *", outer.Op)
	}
	if inner, ok := outer.Left.(*ir.Binary); !ok || inner.Op != ir.OpAdd {
		t.Errorf("left operand = %T, want a + expression", outer.Left)
	}
}

func TestComparisonsProduceBooleans(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, "const b: boolean = 1 < 2;\nconst c: boolean = \"a\" === \"a\";\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	for i, want := range []ir.BinaryOp{ir.OpLess, ir.OpEqual} {
		decl := m.Main[i].(*ir.VarDecl)
		bin, ok := decl.Init.(*ir.Binary)
		if !ok {
			t.Fatalf("statement %d initialiser is %T, want *ir.Binary", i, decl.Init)
		}
		if bin.Op != want {
			t.Errorf("operator = %v, want %v", bin.Op, want)
		}
		if bin.Typ != ir.Boolean {
			t.Errorf("comparison type = %v, want boolean", bin.Typ)
		}
	}
}

func TestResultTypeRejectsBadOperands(t *testing.T) {
	t.Parallel()

	// Arithmetic on strings, and boolean operators on numbers, are not
	// representable and must be reported rather than emitted.
	if _, ok := resultType(ir.OpSub, ir.String); ok {
		t.Error("resultType allowed - on strings")
	}
	if _, ok := resultType(ir.OpAnd, ir.Number); ok {
		t.Error("resultType allowed && on numbers")
	}
	// + is the one operator that is both arithmetic and concatenation.
	if got, ok := resultType(ir.OpAdd, ir.String); !ok || got != ir.String {
		t.Errorf("resultType(+, string) = %v, %v; want string, true", got, ok)
	}
	if got, ok := resultType(ir.OpAdd, ir.Number); !ok || got != ir.Number {
		t.Errorf("resultType(+, number) = %v, %v; want number, true", got, ok)
	}
	if got, ok := resultType(ir.OpEqual, ir.Boolean); !ok || got != ir.Boolean {
		t.Errorf("resultType(===, boolean) = %v, %v; want boolean, true", got, ok)
	}
}

func TestVoidFunctionLowers(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, "function shout(s: string): void {\n  console.log(s);\n}\nshout(\"hi\");\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(m.Funcs) != 1 || m.Funcs[0].Result != ir.Void {
		t.Fatalf("function result = %v, want void", m.Funcs[0].Result)
	}
	if len(m.Funcs[0].Params) != 1 || m.Funcs[0].Params[0].Type != ir.String {
		t.Errorf("parameter = %+v, want a string", m.Funcs[0].Params)
	}
}

func TestErrorFormatsWithSpan(t *testing.T) {
	t.Parallel()

	e := &Error{Span: ir.Span{File: "a.ts", Line: 4, Col: 2}, Msg: "nope"}
	if got, want := e.Error(), "a.ts:4:2: nope"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
