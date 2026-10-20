package ir

import (
	"strings"
	"testing"
)

// TestValidateChecksTruthy: any value has a truthiness, null and undefined
// included, and a Truthy is a boolean; void has none.
func TestValidateChecksTruthy(t *testing.T) {
	t.Parallel()

	prog := func(e Expr) *Program { return ProgramOf(&Module{Name: "t", Main: []Stmt{expr(e)}}) }
	for _, v := range []Expr{n(0), str(""), &BoolLit{}, &UndefinedLit{}, &NullLit{}, &Empty{Typ: MaybeOf(Number)}, &ArrayLit{Typ: ArrayOf(Number)}} {
		if err := prog(&Truthy{Value: v}).Validate(); err != nil {
			t.Errorf("the truthiness of a %s: %v", v.ExprType(), err)
		}
	}
	void := &Call{Callee: "f", Typ: Void}
	if err := prog(&Truthy{Value: void}).Validate(); err == nil || !strings.Contains(err.Error(), "the truthiness of a void") {
		t.Errorf("the truthiness of a void call: %v", err)
	}
	if (&Truthy{Value: n(1)}).ExprType() != Boolean {
		t.Error("a Truthy isn't a boolean")
	}
}
