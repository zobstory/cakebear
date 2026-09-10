package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

var span = ir.Span{File: "t.ts", Line: 1, Col: 1}

func num(v float64) ir.Expr { return &ir.NumberLit{Value: v, Span: span} }

func logNum(e ir.Expr) ir.Stmt {
	return &ir.ExprStmt{Expr: &ir.ConsoleLog{Arg: e, Span: span}, Span: span}
}

// TestBuildProducesRunnableBinary exercises the whole backend: emit, write the
// throwaway module, embed the runtime, invoke the Go toolchain, run the result.
func TestBuildProducesRunnableBinary(t *testing.T) {
	t.Parallel()

	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		logNum(&ir.Binary{Op: ir.OpAdd, Left: num(2), Right: num(3), Typ: ir.Number, Span: span}),
	}}

	out := filepath.Join(t.TempDir(), "prog")
	src, err := Build(m, Options{Output: out})
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, src)
	}

	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running built binary: %v", err)
	}
	if string(got) != "5\n" {
		t.Errorf("binary printed %q, want %q", got, "5\n")
	}
}

// The embedded runtime must land in the generated module: without it the
// compiled program cannot link, and embedding is what keeps cakec a single
// self-contained file with no network dependency.
func TestWriteModuleLaysOutRuntime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := writeModule(dir, "package main\n\nfunc main() {}\n"); err != nil {
		t.Fatalf("writeModule: %v", err)
	}

	for _, want := range []string{"go.mod", "main.go", filepath.Join(rtDir, "runtime.go")} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("generated module is missing %s: %v", want, err)
		}
	}

	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	if !strings.Contains(string(goMod), "module "+genModule) {
		t.Errorf("go.mod = %q, want it to declare module %s", goMod, genModule)
	}

	rt, err := os.ReadFile(filepath.Join(dir, rtDir, "runtime.go"))
	if err != nil {
		t.Fatalf("reading embedded runtime: %v", err)
	}
	if !strings.Contains(string(rt), "func NumberToString") {
		t.Error("embedded runtime does not contain NumberToString")
	}
	// Test files must not travel into the generated module: they would pull in
	// the testing package for no reason.
	if strings.Contains(string(rt), "func Test") {
		t.Error("a _test.go file leaked into the generated runtime")
	}
}

func TestToolchainVersion(t *testing.T) {
	t.Parallel()

	v := toolchainVersion()
	// go.mod wants major.minor, not a patch level or a devel string.
	if strings.Count(v, ".") != 1 {
		t.Errorf("toolchainVersion() = %q, want major.minor", v)
	}
	if !strings.HasPrefix(v, "1.") {
		t.Errorf("toolchainVersion() = %q, want a 1.x version", v)
	}
}

func TestIsNumeric(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"0", "26", "1234"} {
		if !isNumeric(ok) {
			t.Errorf("isNumeric(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "1a", "devel", "-1", "1.2"} {
		if isNumeric(bad) {
			t.Errorf("isNumeric(%q) = true, want false", bad)
		}
	}
}

// Every statement and expression form the Phase-1 subset can produce must emit
// something the Go compiler accepts. Building it is the only assertion that
// really proves that.
func TestEmitAllStatementFormsCompile(t *testing.T) {
	t.Parallel()

	i := &ir.Ident{Name: "i", Typ: ir.Number, Span: span}

	m := &ir.Module{
		Name: "t",
		Funcs: []*ir.Func{{
			Name:   "noop",
			Result: ir.Void,
			Body:   []ir.Stmt{&ir.Return{Span: span}},
			Span:   span,
		}},
		Main: []ir.Stmt{
			&ir.VarDecl{Name: "i", Type: ir.Number, Init: num(0), Mutable: true, Span: span},
			&ir.VarDecl{Name: "s", Type: ir.String, Init: &ir.StringLit{Value: "x", Span: span}, Span: span},
			&ir.VarDecl{Name: "b", Type: ir.Boolean, Init: &ir.BoolLit{Value: true, Span: span}, Span: span},
			&ir.While{
				Cond: &ir.Binary{Op: ir.OpLess, Left: i, Right: num(3), Typ: ir.Boolean, Span: span},
				Body: []ir.Stmt{
					&ir.If{
						Cond: &ir.Unary{Op: ir.OpNot, Operand: &ir.BoolLit{Value: false, Span: span}, Typ: ir.Boolean, Span: span},
						Then: []ir.Stmt{logNum(i)},
						Else: []ir.Stmt{logNum(&ir.Unary{Op: ir.OpNeg, Operand: i, Typ: ir.Number, Span: span})},
						Span: span,
					},
					&ir.Assign{
						Name:  "i",
						Value: &ir.Binary{Op: ir.OpAdd, Left: i, Right: num(1), Typ: ir.Number, Span: span},
						Span:  span,
					},
				},
				Span: span,
			},
			&ir.ExprStmt{Expr: &ir.Call{Callee: "noop", Typ: ir.Void, Span: span}, Span: span},
		},
	}

	out := filepath.Join(t.TempDir(), "prog")
	src, err := Build(m, Options{Output: out})
	if err != nil {
		t.Fatalf("emitted source did not compile: %v\n%s", err, src)
	}

	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running built binary: %v", err)
	}
	if string(got) != "0\n1\n2\n" {
		t.Errorf("binary printed %q, want %q", got, "0\n1\n2\n")
	}
}

// KeepGoSource leaves the temp module behind for inspection, which is the
// single most useful thing to have when the emitter produces something Go
// rejects.
func TestBuildKeepGoSourceReturnsSource(t *testing.T) {
	t.Parallel()

	m := &ir.Module{Name: "t", Main: []ir.Stmt{logNum(num(1))}}
	out := filepath.Join(t.TempDir(), "prog")

	src, err := Build(m, Options{Output: out, KeepGoSource: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(src, "package main") {
		t.Errorf("returned source does not look like Go:\n%s", src)
	}
}

// Cross-compilation is a GOOS/GOARCH passthrough, which is most of what
// choosing a Go-emitting backend bought.
func TestBuildCrossCompiles(t *testing.T) {
	t.Parallel()

	m := &ir.Module{Name: "t", Main: []ir.Stmt{logNum(num(1))}}
	out := filepath.Join(t.TempDir(), "prog-linux")

	if _, err := Build(m, Options{Output: out, GOOS: "linux", GOARCH: "amd64"}); err != nil {
		t.Fatalf("cross-compiling for linux/amd64: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("cross-compiled binary missing: %v", err)
	}
}
