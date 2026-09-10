package ir

import "testing"

func TestTypeString(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   Type
		want string
	}{
		{Number, "number"},
		{String, "string"},
		{Boolean, "boolean"},
		{Void, "void"},
		{Null, "null"},
		{Undefined, "undefined"},
		{Invalid, "invalid"},
	} {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Type(%d).String() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSpanString(t *testing.T) {
	t.Parallel()

	s := Span{File: "main.ts", Line: 3, Col: 7}
	if got, want := s.String(), "main.ts:3:7"; got != want {
		t.Errorf("Span.String() = %q, want %q", got, want)
	}
}

func TestOperatorsPrintAsTypeScript(t *testing.T) {
	t.Parallel()

	// These strings reach users in diagnostics, so they spell the TypeScript
	// operator rather than the Go one it lowers to: === not ==.
	for _, tt := range []struct {
		in   BinaryOp
		want string
	}{
		{OpAdd, "+"}, {OpSub, "-"}, {OpMul, "*"}, {OpDiv, "/"},
		{OpLess, "<"}, {OpLessEq, "<="}, {OpGreater, ">"}, {OpGreaterEq, ">="},
		{OpEqual, "==="}, {OpNotEqual, "!=="},
		{OpAnd, "&&"}, {OpOr, "||"},
	} {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("BinaryOp(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}

	if got := OpNeg.String(); got != "-" {
		t.Errorf("OpNeg = %q, want %q", got, "-")
	}
	if got := OpNot.String(); got != "!" {
		t.Errorf("OpNot = %q, want %q", got, "!")
	}
}

// Every expression reports its own type so the backend never has to consult the
// checker, and every node carries a span so a backend that needs to report a
// fault has somewhere to point.
func TestExpressionsCarryTypeAndSpan(t *testing.T) {
	t.Parallel()

	span := Span{File: "t.ts", Line: 1, Col: 1}

	cases := []struct {
		name string
		expr Expr
		typ  Type
	}{
		{"number", &NumberLit{Value: 1, Span: span}, Number},
		{"string", &StringLit{Value: "s", Span: span}, String},
		{"bool", &BoolLit{Value: true, Span: span}, Boolean},
		{"null", &NullLit{Span: span}, Null},
		{"undefined", &UndefinedLit{Span: span}, Undefined},
		{"ident", &Ident{Name: "x", Typ: String, Span: span}, String},
		{"binary", &Binary{Typ: Boolean, Span: span}, Boolean},
		{"unary", &Unary{Typ: Number, Span: span}, Number},
		{"call", &Call{Typ: Number, Span: span}, Number},
		{"console.log", &ConsoleLog{Span: span}, Void},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.expr.ExprType(); got != tt.typ {
				t.Errorf("ExprType() = %v, want %v", got, tt.typ)
			}
			if got := tt.expr.ExprSpan(); got != span {
				t.Errorf("ExprSpan() = %v, want %v", got, span)
			}
		})
	}
}

func TestStatementsCarrySpan(t *testing.T) {
	t.Parallel()

	span := Span{File: "t.ts", Line: 2, Col: 4}

	stmts := []Stmt{
		&VarDecl{Span: span}, &Assign{Span: span}, &If{Span: span},
		&While{Span: span}, &Return{Span: span}, &ExprStmt{Span: span},
	}
	for _, s := range stmts {
		if got := s.StmtSpan(); got != span {
			t.Errorf("%T.StmtSpan() = %v, want %v", s, got, span)
		}
	}
}
