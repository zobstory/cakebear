package backend

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestMaybesAreRefusedForNow: the IR holds `T | null | undefined` and the
// nodes on it (M5 step 1) before the backend can emit them (step 3), so a
// program using them is refused, construct by construct, rather than built
// into something that quietly does less, or crashing the order pass, which
// panics on an expression it doesn't know.
func TestMaybesAreRefusedForNow(t *testing.T) {
	t.Parallel()

	maybe := ir.MaybeOf(ir.Number)
	v := &ir.Ident{Name: "v", Typ: maybe, Span: span}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		&ir.VarDecl{Name: "v", Type: maybe, Init: &ir.Empty{Typ: maybe, Span: span}, Mutable: true, Span: span},
		logNum(&ir.Cond{Test: &ir.IsEmpty{Value: v, Undefined: true, Span: span}, Then: num(0), Else: &ir.Unwrap{Value: v, Typ: ir.Number, Span: span}, Typ: ir.Number, Span: span}),
	}}
	_, err := Build(ir.ProgramOf(m), Options{Output: filepath.Join(t.TempDir(), "prog")})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || len(unsupported.Nodes) == 0 {
		t.Fatalf("Build = %v, want the maybe constructs reported as unsupported", err)
	}
}
