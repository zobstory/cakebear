package backend

import (
	"go/token"
	"os/exec"
	"path/filepath"
	"strconv"
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
		{"item2", "item2"},
		{"日本2", "日本2"},

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

		// Characters TypeScript allows in a name and Go doesn't are escaped:
		// `_` doubles, `$` is _D, anything else its code point.
		{"$count", "ts_x__Dcount"},
		{"add$", "ts_x_add_D"},
		{"$", "ts_x__D"},
		{"a_$", "ts_x_a___D"},
		{"cafe\u0301", "ts_x_cafe_u301_"},
		{"ts_$", "ts_x_ts___D"},
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
// `ts_type` must not both become `ts_type`. Names are chosen to collide under a
// careless escape: `a$$` and `a_D$` both become `a_D_D` if `_` isn't escaped.
func TestMangleIsInjective(t *testing.T) {
	t.Parallel()

	names := []string{
		"type", "ts_type", "x", "_", "blank", "ts_blank", "init", "ts_init",
		"$", "_D", "__D", "a$", "a_D", "a__D", "a$$", "a_D$", "a$_", "a_$",
		"a_u24_", "x_a_D", "ts_x_a_D", "ts_x__D", "ts_$",
		"e\u0301", "\u00e9", "a\u200c", "a\u200d", "\u216b", "\u216bx",
	}
	seen := map[string]string{}
	for _, name := range names {
		got := mangle(name)
		if !token.IsIdentifier(got) {
			t.Errorf("mangle(%q) = %q, which is not a Go identifier", name, got)
		}
		if other, ok := seen[got]; ok {
			t.Errorf("mangle(%q) and mangle(%q) are both %q", name, other, got)
		}
		seen[got] = name
		if rest, ok := strings.CutPrefix(got, escapedPrefix); ok {
			if back := unescape(t, rest); back != name {
				t.Errorf("mangle(%q) = %q, which reads back as %q", name, got, back)
			}
		}
	}
}

// unescape reverses escape, failing the test on anything escape could not
// have written.
func unescape(t *testing.T, s string) string {
	t.Helper()
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '_' {
			b.WriteByte(s[i])
			continue
		}
		switch {
		case strings.HasPrefix(s[i:], "__"):
			b.WriteByte('_')
			i++
		case strings.HasPrefix(s[i:], "_D"):
			b.WriteByte('$')
			i++
		case strings.HasPrefix(s[i:], "_u"):
			end := strings.IndexByte(s[i+2:], '_')
			r, err := strconv.ParseInt(s[i+2:i+2+max(end, 0)], 16, 32)
			if end < 0 || err != nil {
				t.Fatalf("bad escape at %d in %q", i, s)
			}
			b.WriteRune(rune(r))
			i += 2 + end
		default:
			t.Fatalf("a lone _ at %d in %q", i, s)
		}
	}
	return b.String()
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
		if got, ok := goType(tt.in); !ok || got != tt.want {
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
		if got, ok := logFunc(tt.in); !ok || got != tt.want {
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

	got := Emit(ir.ProgramOf(m))

	for _, want := range []string{
		"package main",
		"func double(ts_domain *" + rtPkg + ".Domain, n float64) float64",
		"return (n * 2.0)",
		"func main()",
		"LogNumber(double(ts_domain, 21.0))",
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

	if got := Emit(ir.ProgramOf(m)); strings.Contains(got, "import") {
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

	got := Emit(ir.ProgramOf(m))
	if !strings.Contains(got, rtPkg+".NegZero") {
		t.Errorf("negative zero was emitted as a constant, which Go folds to +0:\n%s", got)
	}
}

func TestGoTypeForExtensions(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   ir.Type
		want string
	}{
		{ir.Int32, "int32"},
		{ir.Int64, "int64"},
		{ir.Uint32, "uint32"},
		{ir.Uint64, "uint64"},
		{ir.Float32, "float32"},
		// base64 is a compile-time refinement over string, not a distinct
		// runtime representation.
		{ir.Base64, "string"},
	} {
		if got, ok := goType(tt.in); !ok || got != tt.want {
			t.Errorf("goType(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLogFuncForExtensions(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   ir.Type
		want string
	}{
		{ir.Int32, "LogInt32"},
		{ir.Int64, "LogInt64"},
		{ir.Uint32, "LogUint32"},
		{ir.Uint64, "LogUint64"},
		{ir.Float32, "LogFloat32"},
		{ir.Base64, "LogString"},
	} {
		if got, ok := logFunc(tt.in); !ok || got != tt.want {
			t.Errorf("logFunc(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEmitConversion(t *testing.T) {
	t.Parallel()

	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		&ir.VarDecl{
			Name: "n", Type: ir.Int32,
			Init: &ir.Convert{Value: num(42), Typ: ir.Int32, Span: span},
			Span: span,
		},
	}}

	got := Emit(ir.ProgramOf(m))
	if !strings.Contains(got, "var n int32\n") || !strings.Contains(got, "\tn = int32(42.0)\n") {
		t.Errorf("conversion did not emit as a Go conversion:\n%s", got)
	}
}

// int64 holds values a float64 cannot represent exactly, so the runtime must
// print them as integers rather than routing through float formatting.
func TestExtensionIntegersRoundTrip(t *testing.T) {
	t.Parallel()

	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		logNum(&ir.Convert{Value: num(9007199254740991), Typ: ir.Int64, Span: span}),
		logNum(&ir.Convert{Value: num(4294967295), Typ: ir.Uint32, Span: span}),
	}}

	out := filepath.Join(t.TempDir(), "prog")
	src, err := Build(ir.ProgramOf(m), Options{Output: out})
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, src)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if want := "9007199254740991\n4294967295\n"; string(got) != want {
		t.Errorf("printed %q, want %q", got, want)
	}
}
