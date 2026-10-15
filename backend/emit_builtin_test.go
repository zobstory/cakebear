package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestStringOperatorsGoThroughTheRuntime: `+` on two strings joins them
// with StrConcat, which joins a split surrogate pair, and the ordering
// comparisons compare in UTF-16 order through StrCompare; equality stays Go's
// own ==, and `+` on numbers stays Go's +.
func TestStringOperatorsGoThroughTheRuntime(t *testing.T) {
	t.Parallel()

	a, b := stable("a", ir.String), stable("b", ir.String)
	x := stable("x", ir.Number)
	bin := func(op ir.BinaryOp, l, r ir.Expr, typ ir.Type) ir.Expr {
		return &ir.Binary{Op: op, Left: l, Right: r, Typ: typ, Span: span}
	}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		&ir.VarDecl{Name: "a", Type: ir.String, Init: str("x"), Span: span},
		&ir.VarDecl{Name: "b", Type: ir.String, Init: str("y"), Span: span},
		&ir.VarDecl{Name: "x", Type: ir.Number, Init: num(1), Span: span},
		logNum(bin(ir.OpAdd, a, b, ir.String)),
		logNum(bin(ir.OpLess, a, b, ir.Boolean)),
		logNum(bin(ir.OpGreaterEq, a, b, ir.Boolean)),
		logNum(bin(ir.OpEqual, a, b, ir.Boolean)),
		logNum(bin(ir.OpAdd, x, x, ir.Number)),
		logNum(&ir.Concat{Parts: []ir.Expr{str("<"), a, str(">")}, Span: span}),
		logNum(&ir.BuiltinCall{Fn: ir.ToString, Args: []ir.Expr{x}, Typ: ir.String, Span: span}),
	}}
	src := Emit(ir.ProgramOf(m))
	for _, want := range []string{
		`cakebearrt.StrConcat(a, b)`,
		`(cakebearrt.StrCompare(a, b) < 0)`,
		`(cakebearrt.StrCompare(a, b) >= 0)`,
		`(a == b)`,
		`(x + x)`,
		`cakebearrt.StrConcat("<", a, ">")`,
		`cakebearrt.NumberToString(x)`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the Go has no %s:\n%s", want, src)
		}
	}
}

// TestToStringOfExtensionTypes: an extension type converts to the string
// console.log prints for it, exactly, where a trip through float64 would
// round an int64 past 2^53 and a uint64 past 2^63.
func TestToStringOfExtensionTypes(t *testing.T) {
	t.Parallel()

	conv := func(v float64, typ ir.Type) ir.Expr { return &ir.Convert{Value: num(v), Typ: typ, Span: span} }
	str := func(e ir.Expr) ir.Stmt {
		return logNum(&ir.BuiltinCall{Fn: ir.ToString, Args: []ir.Expr{e}, Typ: ir.String, Span: span})
	}
	big := &ir.VarDecl{Name: "big", Type: ir.Int64, Init: conv(9007199254740992, ir.Int64), Mutable: true, Span: span}
	top := &ir.VarDecl{Name: "top", Type: ir.Uint64, Init: conv(0, ir.Uint64), Mutable: true, Span: span}
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		big, top,
		&ir.Assign{Name: "big", Value: &ir.Binary{Op: ir.OpAdd, Left: stable("big", ir.Int64), Right: conv(1, ir.Int64), Typ: ir.Int64, Span: span}, Span: span},
		&ir.Assign{Name: "top", Value: &ir.Binary{Op: ir.OpSub, Left: stable("top", ir.Uint64), Right: conv(1, ir.Uint64), Typ: ir.Uint64, Span: span}, Span: span},
		str(conv(-5, ir.Int32)),
		str(stable("big", ir.Int64)),
		str(conv(4294967295, ir.Uint32)),
		str(stable("top", ir.Uint64)),
		str(conv(0.1, ir.Float32)),
		str(&ir.Convert{Value: str2("aGk="), Typ: ir.Base64, Span: span}),
	}}
	want := "-5\n9007199254740993\n4294967295\n18446744073709551615\n0.10000000149011612\naGk=\n"
	if got := buildAndRun(t, ir.ProgramOf(m)); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
}

func str2(s string) ir.Expr { return &ir.StringLit{Value: s, Span: span} }
