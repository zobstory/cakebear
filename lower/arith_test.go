package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

const i32s = "const a: i32 = i32(7);\nconst b: i32 = i32(2);\n"

// initOf is the initialiser of the module's statement i, a declaration.
func initOf(t *testing.T, m *ir.Module, i int) ir.Expr {
	t.Helper()
	decl, ok := m.Main[i].(*ir.VarDecl)
	if !ok {
		t.Fatalf("statement %d is %T, want *ir.VarDecl", i, m.Main[i])
	}
	return decl.Init
}

// TestExtensionArithmeticIsANumbers: outside its conversion, arithmetic on
// an i32 is a number's, as TypeScript types it, its operands widened. It was
// the i32's, so `const x: number = a / b` was 3.
func TestExtensionArithmeticIsANumbers(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		i32s + "const x: number = a / b;\n",
		i32s + "const x: number = a + 1.5;\n",
		i32s + "const x: number = -a;\n",
		i32s + "const x: boolean = a + b > 1;\n",
	} {
		m, errs := lowerSource(t, src)
		if len(errs) > 0 {
			t.Fatalf("%q: unexpected errors: %v", src, joinMsgs(errs))
		}
		var operands []ir.Expr
		switch x := initOf(t, m, 2).(type) {
		case *ir.Binary:
			if x.Op == ir.OpGreater {
				x = x.Left.(*ir.Binary)
			}
			if x.Typ != ir.Number {
				t.Errorf("%q: %v is a %s, want a number", src, x.Op, x.Typ)
			}
			operands = []ir.Expr{x.Left, x.Right}
		case *ir.Unary:
			if x.Typ != ir.Number {
				t.Errorf("%q: the negation is a %s, want a number", src, x.Typ)
			}
			operands = []ir.Expr{x.Operand}
		case *ir.Convert:
			t.Errorf("%q: the arithmetic is a %s's, widened only after it", src, x.Value.ExprType())
			continue
		default:
			t.Fatalf("%q: initialiser is %T", src, x)
		}
		for _, o := range operands {
			if o.ExprType() != ir.Number {
				t.Errorf("%q: operand %T is a %s, want it widened to a number", src, o, o.ExprType())
			}
		}
	}
}

// TestExtensionArithmeticIsNativeInItsConversion: inside `i32(…)`, a sum,
// difference or product is the i32's, at any depth, with no float64 between
// it and the conversion; a / is the i32's only as the argument itself.
func TestExtensionArithmeticIsNativeInItsConversion(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, i32s+"const s: i32 = i32(a + b * a);\nconst q: i32 = i32(a / b);\nconst r: i32 = i32((a + b) / b);\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", joinMsgs(errs))
	}
	native := func(i int, op ir.BinaryOp) *ir.Binary {
		conv, ok := initOf(t, m, i).(*ir.Convert)
		if !ok {
			t.Fatalf("statement %d: initialiser is %T, want *ir.Convert", i, initOf(t, m, i))
		}
		bin, ok := conv.Value.(*ir.Binary)
		if !ok || bin.Op != op {
			t.Fatalf("statement %d: the conversion's argument is %T, want the %v itself", i, conv.Value, op)
		}
		return bin
	}
	if s := native(2, ir.OpAdd); s.Typ != ir.Int32 || s.Right.ExprType() != ir.Int32 {
		t.Errorf("i32(a + b * a): the sum is a %s and the product a %s, want both i32", s.Typ, s.Right.ExprType())
	}
	if q := native(3, ir.OpDiv); q.Typ != ir.Int32 {
		t.Errorf("i32(a / b): the quotient is a %s, want an i32", q.Typ)
	}
	if r := native(4, ir.OpDiv); r.Typ != ir.Number {
		t.Errorf("i32((a + b) / b): the quotient is a %s, want a number: the sum below it isn't native", r.Typ)
	}
}

// TestConversionTakesItsArgumentAsItIs: converting an i64 to an i64 is the
// value itself. After PR 46 it went through a float64, the widening its
// argument's slot, a number's, gives, and lost what a number can't hold.
func TestConversionTakesItsArgumentAsItIs(t *testing.T) {
	t.Parallel()

	m, errs := lowerSource(t, "const x: i64 = i64(4611686018427387904);\nconst y: i64 = i64(x);\n")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", joinMsgs(errs))
	}
	conv := initOf(t, m, 1).(*ir.Convert)
	if _, ok := conv.Value.(*ir.Ident); !ok {
		t.Errorf("i64(x) converts a %T, want x itself", conv.Value)
	}
}

// TestIntegerLiteralsAreExact: a literal an integer type holds must be one a
// number holds exactly, and in range, the range's top checked exactly.
func TestIntegerLiteralsAreExact(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ src, want string }{
		{"i64(9007199254740993)", "9007199254740993 is 9007199254740992 as a number, which is how TypeScript reads it before i64 sees it; write a literal a number holds exactly"},
		{"u64(18446744073709551615)", "18446744073709551615 is 18446744073709551616 as a number"},
		{"i64(-9223372036854775809)", "-9223372036854775809 is -9223372036854775808 as a number"},
		{"i32(1.0000000000000001)", "1.0000000000000001 is 1 as a number"},
		{"i64(9223372036854775808)", "9223372036854775808 is outside the range of i64"},
		{"u64(18446744073709551616)", "18446744073709551616 is outside the range of u64"},
		{"i32(2147483648)", "2147483648 is outside the range of i32"},
		{"i64(4611686018427387904)", ""},
		{"i64(-9223372036854775808)", ""},
		{"u64(18446744073709549568)", ""},
		{"u32(4294967295)", ""},
		{"i64(1e18)", ""},
		{"i64(0x10)", ""},
	} {
		_, errs := lowerSource(t, "const v = "+tt.src+";\n")
		msgs := joinMsgs(errs)
		if tt.want == "" && msgs != "" || tt.want != "" && !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors = %q, want %q", tt.src, msgs, tt.want)
		}
	}
}

func TestArithmeticRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src, want string }{
		{"**", "const x: number = 2 ** 3;\n", "** is not supported yet: it waits for Math, whose functions must match V8's to the last bit"},
		{"% on strings", "const s: string = \"a\";\nconst x: string = s % s;\n", "% is not supported on string"},
	} {
		_, errs := lowerSource(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors = %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}
