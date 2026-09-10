package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

func TestMangle(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		// Untouched where it is safe. Readable generated source is worth
		// keeping, and Go accepts Unicode identifiers just as TypeScript does.
		{"x", "x"},
		{"myValue", "myValue"},
		{"café", "café"},
		{"日本", "日本"},
		{"_private", "_private"},

		// Go keywords that TypeScript allows as identifiers.
		{"type", "ts_type"},
		{"func", "ts_func"},
		{"range", "ts_range"},
		{"chan", "ts_chan"},
		{"go", "ts_go"},

		// Predeclared names: legal to shadow, but the result is unreadable
		// or wrong.
		{"string", "ts_string"},
		{"len", "ts_len"},
		{"nil", "ts_nil"},
		{"main", "ts_main"},

		// The runtime's own import alias.
		{rtPkg, "ts_" + rtPkg},

		// Mangling must be injective: a user name that already looks mangled
		// gets prefixed again rather than colliding with a real keyword's
		// mangled form.
		{"ts_type", "ts_ts_type"},

		{"_", "ts_blank"},
		{"", "_"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := mangle(tt.in); got != tt.want {
				t.Errorf("mangle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The collision this guards against: `type` and a user variable literally named
// `ts_type` must not both become `ts_type`.
func TestMangleIsInjective(t *testing.T) {
	t.Parallel()

	if mangle("type") == mangle("ts_type") {
		t.Errorf("mangle collapsed two distinct names onto %q", mangle("type"))
	}
}

func TestGoFloat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   float64
		want string
	}{
		// The suffix is not cosmetic: an untyped Go constant 5 divided by 2 is
		// 2, while TypeScript means 2.5.
		{5, "5.0"},
		{0, "0.0"},
		{-3, "-3.0"},
		{2.5, "2.5"},
		{1e21, "1e+21"},
	}

	for _, tt := range tests {
		if got := goFloat(tt.in); got != tt.want {
			t.Errorf("goFloat(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGoType(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   ir.Type
		want string
	}{
		{ir.Number, "float64"},
		{ir.String, "string"},
		{ir.Boolean, "bool"},
	} {
		if got := goType(tt.in); got != tt.want {
			t.Errorf("goType(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLogFunc(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   ir.Type
		want string
	}{
		{ir.Number, "LogNumber"},
		{ir.String, "LogString"},
		{ir.Boolean, "LogBool"},
		{ir.Null, "LogNull"},
		{ir.Undefined, "LogUndefined"},
	} {
		if got := logFunc(tt.in); got != tt.want {
			t.Errorf("logFunc(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEmitProducesCompilableShape(t *testing.T) {
	t.Parallel()

	span := ir.Span{File: "t.ts", Line: 1, Col: 1}
	m := &ir.Module{
		Name: "t",
		Funcs: []*ir.Func{{
			Name:   "double",
			Params: []ir.Param{{Name: "n", Type: ir.Number, Span: span}},
			Result: ir.Number,
			Body: []ir.Stmt{&ir.Return{
				Value: &ir.Binary{
					Op:    ir.OpMul,
					Left:  &ir.Ident{Name: "n", Typ: ir.Number, Span: span},
					Right: &ir.NumberLit{Value: 2, Span: span},
					Typ:   ir.Number,
					Span:  span,
				},
				Span: span,
			}},
			Span: span,
		}},
		Main: []ir.Stmt{&ir.ExprStmt{
			Expr: &ir.ConsoleLog{
				Arg:  &ir.Call{Callee: "double", Args: []ir.Expr{&ir.NumberLit{Value: 21, Span: span}}, Typ: ir.Number, Span: span},
				Span: span,
			},
			Span: span,
		}},
	}

	got := Emit(m)

	for _, want := range []string{
		"package main",
		"func double(n float64) float64",
		"return (n * 2.0)",
		"func main()",
		"LogNumber(double(21.0))",
		"defer " + rtPkg + ".Flush()",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}

// An unused import is a compile error in Go, so a module that never touches the
// runtime must not import it.
func TestEmitOmitsRuntimeImportWhenUnused(t *testing.T) {
	t.Parallel()

	span := ir.Span{File: "t.ts", Line: 1, Col: 1}
	m := &ir.Module{
		Name: "t",
		Main: []ir.Stmt{&ir.VarDecl{
			Name: "x", Type: ir.Number,
			Init: &ir.NumberLit{Value: 1, Span: span},
			Span: span,
		}},
	}

	if got := Emit(m); strings.Contains(got, "import") {
		t.Errorf("emitted an import for a module that uses no runtime:\n%s", got)
	}
}

// Negative zero has no literal spelling in Go: the source text -0.0 folds to
// positive zero as an untyped constant, so the emitter must reach for a runtime
// value instead.
func TestEmitNegativeZeroUsesRuntimeValue(t *testing.T) {
	t.Parallel()

	span := ir.Span{File: "t.ts", Line: 1, Col: 1}
	negZero := 0.0
	negZero = -negZero

	m := &ir.Module{
		Name: "t",
		Main: []ir.Stmt{&ir.ExprStmt{
			Expr: &ir.ConsoleLog{Arg: &ir.NumberLit{Value: negZero, Span: span}, Span: span},
			Span: span,
		}},
	}

	got := Emit(m)
	if !strings.Contains(got, rtPkg+".NegZero") {
		t.Errorf("negative zero was emitted as a constant, which Go folds to +0:\n%s", got)
	}
}
