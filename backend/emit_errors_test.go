package backend

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestErrorsAreRefusedForNow: the IR holds throw, try, Error and unknown
// (M6 step 1) before the backend can emit them (steps 2 and 3), so a program
// using them is refused, construct by construct, rather than built into
// something that quietly does less, or crashing the order pass, which panics
// on an expression it doesn't know.
func TestErrorsAreRefusedForNow(t *testing.T) {
	t.Parallel()

	newErr := &ir.NewError{Kind: ir.ErrorType, Message: str("m"), Span: span}
	boxed := &ir.Box{Value: num(1), Span: span}
	use := func(x ir.Expr) ir.Stmt { return &ir.ExprStmt{Expr: x, Span: span} }
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		&ir.Throw{Value: newErr, Span: span},
		&ir.Try{Body: []ir.Stmt{logNum(num(1))}, Finally: []ir.Stmt{}, Span: span},
		use(&ir.ErrorGet{Error: newErr, Prop: ir.ErrorMessage, Span: span}),
		use(&ir.Unbox{Value: boxed, Typ: ir.Number, Span: span}),
		use(&ir.InstanceOf{Value: boxed, Kind: ir.ErrorBase, Span: span}),
		use(&ir.TypeOf{Value: boxed, Tag: ir.TagNumber, Span: span}),
	}}
	if err := ir.ProgramOf(m).Validate(); err != nil {
		t.Fatalf("the program is not valid IR, so it tests nothing: %v", err)
	}
	_, err := Build(ir.ProgramOf(m), Options{Output: filepath.Join(t.TempDir(), "prog")})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Build = %v, want the error constructs reported as unsupported", err)
	}
	var whats []string
	for _, n := range unsupported.Nodes {
		whats = append(whats, n.What)
	}
	all := strings.Join(whats, "\n")
	for _, name := range []string{"*ir.Throw", "*ir.Try", "*ir.ErrorGet", "*ir.Unbox", "*ir.InstanceOf", "*ir.TypeOf"} {
		if !strings.Contains(all, name) {
			t.Errorf("%s is not among the constructs refused:\n%s", name, all)
		}
	}
}
