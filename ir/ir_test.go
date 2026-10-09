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
			t.Errorf("Type(%d).String() = %q, want %q", tt.in.Kind, got, tt.want)
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

func TestExtensionTypeNames(t *testing.T) {
	t.Parallel()

	// These strings reach users in diagnostics, so they spell the cakebear
	// type, not the Go type it lowers to.
	for _, tt := range []struct {
		in   Type
		want string
	}{
		{Int32, "i32"}, {Int64, "i64"}, {Uint32, "u32"},
		{Uint64, "u64"}, {Float32, "f32"}, {Base64, "base64"},
	} {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Type(%d).String() = %q, want %q", tt.in.Kind, got, tt.want)
		}
	}
}

func TestIsExtension(t *testing.T) {
	t.Parallel()

	for _, ext := range []Type{Int32, Int64, Uint32, Uint64, Float32, Base64} {
		if !ext.IsExtension() {
			t.Errorf("%v.IsExtension() = false, want true", ext)
		}
	}
	for _, prim := range []Type{Number, String, Boolean, Void, Null, Undefined, Invalid} {
		if prim.IsExtension() {
			t.Errorf("%v.IsExtension() = true, want false", prim)
		}
	}
}

// IsNumeric decides where arithmetic and comparison apply. base64 is an
// extension but is not a number, which is the case worth pinning down.
func TestIsNumeric(t *testing.T) {
	t.Parallel()

	for _, num := range []Type{Number, Int32, Int64, Uint32, Uint64, Float32} {
		if !num.IsNumeric() {
			t.Errorf("%v.IsNumeric() = false, want true", num)
		}
	}
	for _, notNum := range []Type{String, Boolean, Base64, Void, Null, Undefined, Invalid} {
		if notNum.IsNumeric() {
			t.Errorf("%v.IsNumeric() = true, want false", notNum)
		}
	}
}

func TestConvertCarriesTargetType(t *testing.T) {
	t.Parallel()

	span := Span{File: "t.ts", Line: 1, Col: 1}
	c := &Convert{Value: &NumberLit{Value: 1, Span: span}, Typ: Int32, Span: span}

	if got := c.ExprType(); got != Int32 {
		t.Errorf("ExprType() = %v, want i32", got)
	}
	if got := c.ExprSpan(); got != span {
		t.Errorf("ExprSpan() = %v, want %v", got, span)
	}
}

// allPrimitives is every non-function, non-host type, in Kind order.
var allPrimitives = []Type{
	Invalid, Number, String, Boolean, Void, Null, Undefined,
	Int32, Int64, Uint32, Uint64, Float32, Base64,
}

// An unset Type must read as Invalid: Func.Result and Param.Type rely on the
// zero value meaning "no type", exactly as the old int enum did.
func TestZeroTypeIsInvalid(t *testing.T) {
	t.Parallel()

	var zero Type
	if zero != Invalid || !zero.Equal(Invalid) {
		t.Errorf("zero Type = %#v, want Invalid", zero)
	}
	if got := zero.String(); got != "invalid" {
		t.Errorf("zero Type.String() = %q, want %q", got, "invalid")
	}
}

func TestTypeEqualPrimitives(t *testing.T) {
	t.Parallel()

	for i, a := range allPrimitives {
		for j, b := range allPrimitives {
			if got, want := a.Equal(b), i == j; got != want {
				t.Errorf("%v.Equal(%v) = %v, want %v", a, b, got, want)
			}
		}
	}
}

func TestTypeEqualHost(t *testing.T) {
	t.Parallel()

	server, req, res := HostOf(HostHTTPServer), HostOf(HostIncomingMessage), HostOf(HostServerResponse)

	if !req.Equal(HostOf(HostIncomingMessage)) {
		t.Errorf("%v.Equal(same host type) = false, want true", req)
	}
	for _, other := range []Type{server, res, Number, FuncType(nil, Void), Invalid} {
		if req.Equal(other) || other.Equal(req) {
			t.Errorf("%v.Equal(%v) = true, want false", req, other)
		}
	}
	// A host type is never a primitive, whatever its HostType value is.
	if HostOf(HostNone).Equal(Invalid) {
		t.Errorf("HostOf(HostNone).Equal(Invalid) = true, want false")
	}
}

// Equal exists because Sig is a pointer: two function types built separately
// from the same parts are the same type, and == would call them different.
func TestTypeEqualFunc(t *testing.T) {
	t.Parallel()

	handler := func() Type {
		return FuncType([]Type{HostOf(HostIncomingMessage), HostOf(HostServerResponse)}, Void)
	}
	a, b := handler(), handler()

	if a == b {
		t.Fatal("two separately built func types compared == ; the test no longer shows why Equal exists")
	}
	if !a.Equal(b) || !b.Equal(a) {
		t.Errorf("%v.Equal(%v) = false, want true", a, b)
	}

	higher := func(cb Type) Type { return FuncType([]Type{cb}, Number) }

	for _, tt := range []struct {
		name  string
		other Type
	}{
		{"fewer params", FuncType([]Type{HostOf(HostIncomingMessage)}, Void)},
		{"more params", FuncType([]Type{HostOf(HostIncomingMessage), HostOf(HostServerResponse), Number}, Void)},
		{"param order", FuncType([]Type{HostOf(HostServerResponse), HostOf(HostIncomingMessage)}, Void)},
		{"param type", FuncType([]Type{HostOf(HostIncomingMessage), HostOf(HostHTTPServer)}, Void)},
		{"result type", FuncType([]Type{HostOf(HostIncomingMessage), HostOf(HostServerResponse)}, Number)},
		{"primitive", Number},
		{"host", HostOf(HostServerResponse)},
		{"nil signature", Type{Kind: KindFunc}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if a.Equal(tt.other) || tt.other.Equal(a) {
				t.Errorf("%v.Equal(%v) = true, want false", a, tt.other)
			}
		})
	}

	// Equality recurses: a function that takes a function.
	if !higher(handler()).Equal(higher(handler())) {
		t.Error("func taking an equal func is not Equal")
	}
	if higher(handler()).Equal(higher(FuncType(nil, Void))) {
		t.Error("func taking a different func is Equal")
	}

	// Two function types with no signature at all are the same degenerate type.
	if !(Type{Kind: KindFunc}).Equal(Type{Kind: KindFunc}) {
		t.Error("two nil-signature func types are not Equal")
	}
	if !FuncType(nil, Void).Equal(FuncType([]Type{}, Void)) {
		t.Error("nil and empty parameter lists are not Equal")
	}
}

func TestFuncAndHostTypesAreNeitherExtensionNorNumeric(t *testing.T) {
	t.Parallel()

	for _, typ := range []Type{FuncType([]Type{Number}, Number), HostOf(HostHTTPServer)} {
		if typ.IsExtension() || typ.IsNumeric() {
			t.Errorf("%v: IsExtension() = %v, IsNumeric() = %v, want both false",
				typ, typ.IsExtension(), typ.IsNumeric())
		}
	}
}

func TestFuncAndHostTypeStrings(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in   Type
		want string
	}{
		{FuncType(nil, Void), "() => void"},
		{FuncType([]Type{Number, String}, Boolean), "(number, string) => boolean"},
		{FuncType([]Type{FuncType([]Type{Int32}, Void)}, Number), "((i32) => void) => number"},
		{HostOf(HostHTTPServer), "Server"},
		{HostOf(HostIncomingMessage), "IncomingMessage"},
		{HostOf(HostServerResponse), "ServerResponse"},
		{Type{Kind: KindFunc}, "() => invalid"},
	} {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}
