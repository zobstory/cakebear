package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestJSONSpaceOfEveryKind: a space of one of cakebear's numeric types is
// converted for the runtime, a string space is passed as it is, and a call
// with none is compact. What each prints is what Node 24.18 printed for the
// same values written as plain numbers:
//
//	console.log(JSON.stringify([1, 2], null, 2));      // the space an i32
//	console.log(JSON.stringify(-5, null, 1));          // the value an i64, the space a u32
//	console.log(JSON.stringify([0.5], null, "ab"));    // the value an f32 array
//	console.log(JSON.stringify([1, 2]));
func TestJSONSpaceOfEveryKind(t *testing.T) {
	t.Parallel()

	conv := func(v float64, typ ir.Type) ir.Expr { return &ir.Convert{Value: num(v), Typ: typ, Span: span} }
	json := func(value, space ir.Expr) ir.Expr { return &ir.JSONStringify{Value: value, Space: space, Span: span} }
	pair := func() ir.Expr { return arrayOf(ir.Number, num(1), num(2)) }
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		logNum(json(pair(), conv(2, ir.Int32))),
		logNum(json(conv(-5, ir.Int64), conv(1, ir.Uint32))),
		logNum(json(arrayOf(ir.Float32, conv(0.5, ir.Float32)), str("ab"))),
		logNum(json(pair(), nil)),
	}}
	p := ir.ProgramOf(m)
	want := "[\n  1,\n  2\n]\n-5\n[\nab0.5\n]\n[1,2]\n"
	if got := buildAndRun(t, p); got != want {
		t.Errorf("the program printed %q, want %q", got, want)
	}
	if src := Emit(p); !strings.Contains(src, "JSONGapNumber(float64(") || !strings.Contains(src, ".JSONGap{}, &ts_sites[0])") {
		t.Errorf("want an extension space converted, and a call with none given the zero gap:\n%s", src)
	}
}
