package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestConstantArithmeticRunsAtRunTime: arithmetic on two Go constants is
// emitted with one operand through readFunc, so Go computes it as a float64
// at run time, as JavaScript does, rather than exactly at compile time.
// Lowering folds `-(0.1)` into one literal, so the negated and converted
// forms here come only from IR; they are constants to Go all the same.
func TestConstantArithmeticRunsAtRunTime(t *testing.T) {
	t.Parallel()

	neg := &ir.Unary{Op: ir.OpNeg, Operand: num(0.1), Typ: ir.Number, Span: span}
	f32 := func(v float64) ir.Expr { return &ir.Convert{Value: num(v), Typ: ir.Float32, Span: span} }
	x := &ir.Ident{Name: "x", Typ: ir.Number, Stable: true, Span: span}
	for _, tt := range []struct {
		name    string
		sum     *ir.Binary
		ordered bool
	}{
		{"two literals", &ir.Binary{Op: ir.OpAdd, Left: num(0.1), Right: num(0.2), Typ: ir.Number, Span: span}, true},
		{"a negated literal", &ir.Binary{Op: ir.OpAdd, Left: neg, Right: num(0.2), Typ: ir.Number, Span: span}, true},
		{"two converted literals", &ir.Binary{Op: ir.OpAdd, Left: f32(0.1), Right: f32(0.2), Typ: ir.Float32, Span: span}, true},
		{"a variable and a literal", &ir.Binary{Op: ir.OpDiv, Left: x, Right: num(0), Typ: ir.Number, Span: span}, false},
	} {
		m := &ir.Module{Name: "t", Main: []ir.Stmt{
			&ir.VarDecl{Name: "x", Type: ir.Number, Init: num(1), Span: span},
			&ir.VarDecl{Name: "s", Type: tt.sum.Typ, Init: tt.sum, Span: span},
		}}
		src := Emit(ir.ProgramOf(m))
		if got := strings.Contains(src, readFunc+"("); got != tt.ordered {
			t.Errorf("%s: the emitted source uses %s: %v, want %v\n%s", tt.name, readFunc, got, tt.ordered, src)
		}
	}
}
