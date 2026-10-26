package backend

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestEmitErrors: each node of ir/errors.go as the Go a reader would check
// it against (M6 step 3). A throw carries its site, a try with a finally
// ends by passing on what was thrown, and every place is one entry of the
// site table.
func TestEmitErrors(t *testing.T) {
	t.Parallel()

	newErr := &ir.NewError{Kind: ir.ErrorType, Message: str("m"), Span: span}
	boxed := &ir.Box{Value: num(1), Span: span}
	use := func(x ir.Expr) ir.Stmt { return &ir.ExprStmt{Expr: x, Span: span} }
	m := &ir.Module{Name: "t", Main: []ir.Stmt{
		&ir.Try{Body: []ir.Stmt{&ir.Throw{Value: newErr, Span: span}}, Catch: &ir.Catch{Name: "e", Span: span},
			Finally: []ir.Stmt{logNum(num(2))}, Span: span},
		use(&ir.ErrorGet{Error: newErr, Prop: ir.ErrorMessage, Span: span}),
		use(&ir.Unbox{Value: boxed, Typ: ir.Number, Span: span}),
		use(&ir.InstanceOf{Value: boxed, Kind: ir.ErrorBase, Span: span}),
		use(&ir.TypeOf{Value: boxed, Tag: ir.TagNumber, Span: span}),
		use(&ir.IsEmpty{Value: boxed, Null: true, Span: span}),
		use(&ir.BuiltinCall{Fn: ir.ToString, Args: []ir.Expr{newErr}, Typ: ir.String, Span: span}),
	}}
	if err := ir.ProgramOf(m).Validate(); err != nil {
		t.Fatalf("the program is not valid IR, so it tests nothing: %v", err)
	}
	got := Emit(ir.ProgramOf(m))
	mustParse(t, got)
	site := "&ts_sites[0]"
	for _, want := range []string{
		"\tts_t1 := cakebearrt.Try(func() {\n", // no way out but its end: no code
		"\t\tpanic(&cakebearrt.Thrown{V: cakebearrt.NewError(cakebearrt.ErrorType, \"m\", " + site + "), Site: " + site + "})\n",
		"\tif ts_t1 != nil {\n\t\tts_t1 = cakebearrt.Try(func() {\n\t\t\te := ts_t1.V\n\t\t\t_ = e\n", // a catch before a finally is a literal too
		"\tcakebearrt.LogNumber(2.0)\n\tcakebearrt.Rethrow(ts_t1)\n",                                  // the finally, then what was thrown
		"cakebearrt.NewError(cakebearrt.ErrorType, \"m\", " + site + ").Message()",
		"cakebearrt.Unbox[float64](1.0, ",
		"cakebearrt.InstanceOf(1.0, cakebearrt.ErrorBase)",
		"(cakebearrt.TypeOf(1.0) == cakebearrt.TagNumber)",
		"cakebearrt.IsNullUnknown(1.0)",
		"cakebearrt.ErrorToString(cakebearrt.NewError(",
		"var ts_sites = [...]cakebearrt.Site{\n\t{File: \"t.ts\", Line: 1, Col: 1, Text: \"\"},\n}\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted source missing %q:\n%s", want, got)
		}
	}
}

// TestUTF16Col: a site's column is in UTF-16 units, as V8 counts it, where a
// span's is in bytes: a character of the BMP past ASCII is one unit and
// several bytes, one outside it two units and four bytes.
func TestUTF16Col(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		line     string
		byteCol  int
		utf16Col int
	}{
		{"throw x", 1, 1},
		{"  throw x", 3, 3},
		{"日本 throw", 8, 4},
		{"😀throw", 5, 3},
		{"past", 9, 9}, // past the line's end: unchanged
	} {
		if got := utf16Col(tt.line, tt.byteCol); got != tt.utf16Col {
			t.Errorf("utf16Col(%q, %d) = %d, want %d", tt.line, tt.byteCol, got, tt.utf16Col)
		}
	}
}
