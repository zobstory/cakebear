// Package ir is cakebear's lowered intermediate representation: the contract
// between the TypeScript frontend and whatever backend compiles it.
//
// It imports nothing but the standard library, and that is load-bearing rather
// than tidy. backend/ consumes ir/ and must stay free of any dependency on the
// forked TypeScript compiler, so that swapping the Go emitter for an LLVM one
// touches nothing upstream of this package, and so that backend/ can later be
// extracted to a standalone github.com/zobstory/buildbinary. If ir/ ever
// imports internal/..., both of those properties are gone.
//
// Lowering from the checked AST into these types lives in lower/, which is the
// only package that sees both worlds.
package ir

import "fmt"

// Span locates a node in the original TypeScript source.
//
// Every node carries one. Phase 1 does not report runtime errors, but a
// backend that cannot say which line produced a fault is one that has to be
// retrofitted later, and retrofitting spans through an IR is miserable.
type Span struct {
	File  string
	Start int
	End   int
	Line  int // 1-based
	Col   int // 1-based
}

func (s Span) String() string {
	return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Col)
}

// Type is the Phase-1 type surface. TypeScript's full type system is far
// richer; this is only what the backend can currently give a representation to,
// and lower/ refuses anything outside it rather than guessing.
type Type int

const (
	Invalid Type = iota
	Number
	String
	Boolean
	Void
	Null
	Undefined

	// cakebear's extension types. Unlike the above, these have no TypeScript
	// equivalent: they exist so the backend can use a native machine type
	// instead of float64. See types/cakebear.d.ts.
	Int32
	Int64
	Uint32
	Uint64
	Float32
	Base64
)

// IsExtension reports whether t is one of cakebear's own types rather than a
// TypeScript primitive.
func (t Type) IsExtension() bool {
	return t >= Int32 && t <= Base64
}

// IsNumeric reports whether t holds a number, extension or not. Arithmetic and
// comparison apply to exactly these.
func (t Type) IsNumeric() bool {
	return t == Number || (t.IsExtension() && t != Base64)
}

func (t Type) String() string {
	switch t {
	case Number:
		return "number"
	case String:
		return "string"
	case Boolean:
		return "boolean"
	case Void:
		return "void"
	case Null:
		return "null"
	case Undefined:
		return "undefined"
	case Int32:
		return "i32"
	case Int64:
		return "i64"
	case Uint32:
		return "u32"
	case Uint64:
		return "u64"
	case Float32:
		return "f32"
	case Base64:
		return "base64"
	default:
		return "invalid"
	}
}

// Module is one compiled program.
type Module struct {
	// Name is the source basename, used for the output binary.
	Name string
	// Funcs are the top-level function declarations.
	Funcs []*Func
	// Main is the top-level statement sequence, in source order.
	Main []Stmt
}

// Func is a top-level function declaration.
type Func struct {
	Name   string
	Params []Param
	Result Type
	Body   []Stmt
	Span   Span
}

type Param struct {
	Name string
	Type Type
	Span Span
}

// Stmt is a lowered statement.
//
// The interface is deliberately closed: an unexported marker method means only
// this package can add cases, so a backend switch that handles every type here
// is exhaustive by construction rather than by hope.
type Stmt interface {
	isStmt()
	StmtSpan() Span
}

// VarDecl is `const`/`let` with an initialiser. Phase 1 requires an explicit
// type annotation, so Type is always resolved rather than inferred.
type VarDecl struct {
	Name    string
	Type    Type
	Init    Expr
	Mutable bool // let, versus const
	Span    Span
}

// Assign is assignment to an existing binding.
type Assign struct {
	Name  string
	Value Expr
	Span  Span
}

type If struct {
	Cond Expr
	Then []Stmt
	Else []Stmt // nil when there is no else
	Span Span
}

type While struct {
	Cond Expr
	Body []Stmt
	Span Span
}

// Return carries a nil Value for a bare `return`.
type Return struct {
	Value Expr
	Span  Span
}

// ExprStmt is an expression evaluated for its effect, such as a call.
type ExprStmt struct {
	Expr Expr
	Span Span
}

func (*VarDecl) isStmt()  {}
func (*Assign) isStmt()   {}
func (*If) isStmt()       {}
func (*While) isStmt()    {}
func (*Return) isStmt()   {}
func (*ExprStmt) isStmt() {}

func (s *VarDecl) StmtSpan() Span  { return s.Span }
func (s *Assign) StmtSpan() Span   { return s.Span }
func (s *If) StmtSpan() Span       { return s.Span }
func (s *While) StmtSpan() Span    { return s.Span }
func (s *Return) StmtSpan() Span   { return s.Span }
func (s *ExprStmt) StmtSpan() Span { return s.Span }

// Expr is a lowered expression. Every expression knows its own type, so the
// backend never has to consult the checker.
type Expr interface {
	isExpr()
	ExprType() Type
	ExprSpan() Span
}

// NumberLit holds the parsed value, not the source text. TypeScript numeric
// literals are IEEE-754 doubles, and preserving the text would let the backend
// re-parse it under Go's rules instead of TypeScript's.
type NumberLit struct {
	Value float64
	Span  Span
}

type StringLit struct {
	Value string
	Span  Span
}

type BoolLit struct {
	Value bool
	Span  Span
}

type NullLit struct{ Span Span }

type UndefinedLit struct{ Span Span }

// Ident is a reference to a parameter or variable.
type Ident struct {
	Name string
	Typ  Type
	Span Span
}

// BinaryOp is the set of binary operators the Phase-1 subset lowers.
type BinaryOp int

const (
	OpAdd BinaryOp = iota // arithmetic on numbers, concatenation on strings
	OpSub
	OpMul
	OpDiv
	OpLess
	OpLessEq
	OpGreater
	OpGreaterEq
	OpEqual    // === only; == is not in the subset
	OpNotEqual // !==
	OpAnd
	OpOr
)

func (o BinaryOp) String() string {
	switch o {
	case OpAdd:
		return "+"
	case OpSub:
		return "-"
	case OpMul:
		return "*"
	case OpDiv:
		return "/"
	case OpLess:
		return "<"
	case OpLessEq:
		return "<="
	case OpGreater:
		return ">"
	case OpGreaterEq:
		return ">="
	case OpEqual:
		return "==="
	case OpNotEqual:
		return "!=="
	case OpAnd:
		return "&&"
	case OpOr:
		return "||"
	default:
		return "?"
	}
}

type Binary struct {
	Op    BinaryOp
	Left  Expr
	Right Expr
	Typ   Type
	Span  Span
}

type UnaryOp int

const (
	OpNeg UnaryOp = iota // -x
	OpNot                // !x
)

func (o UnaryOp) String() string {
	if o == OpNeg {
		return "-"
	}
	return "!"
}

type Unary struct {
	Op      UnaryOp
	Operand Expr
	Typ     Type
	Span    Span
}

// Call is a call to a top-level function declared in this module. Phase 1 has
// no first-class functions, so the callee is a name rather than an expression.
type Call struct {
	Callee string
	Args   []Expr
	Typ    Type
	Span   Span
}

// ConsoleLog is `console.log(x)` as an intrinsic rather than a method call.
//
// Modelling it as a call would require an object model, a `console` binding and
// property access, none of which exist in the Phase-1 subset. Making it an IR
// node keeps the subset honest: the backend can lower exactly what the language
// currently supports and reject the rest.
type ConsoleLog struct {
	Arg  Expr
	Span Span
}

// Convert is an explicit conversion to one of cakebear's extension types,
// written as `i32(x)` in source.
//
// It is never inserted implicitly. TypeScript types `i32 + i32` as `number`
// because adding two 32-bit integers can overflow, so widening back is
// something the author writes and the reader can see.
type Convert struct {
	Value Expr
	Typ   Type
	Span  Span
}

func (*Convert) isExpr()          {}
func (e *Convert) ExprType() Type { return e.Typ }
func (e *Convert) ExprSpan() Span { return e.Span }

func (*NumberLit) isExpr()    {}
func (*StringLit) isExpr()    {}
func (*BoolLit) isExpr()      {}
func (*NullLit) isExpr()      {}
func (*UndefinedLit) isExpr() {}
func (*Ident) isExpr()        {}
func (*Binary) isExpr()       {}
func (*Unary) isExpr()        {}
func (*Call) isExpr()         {}
func (*ConsoleLog) isExpr()   {}

func (e *NumberLit) ExprType() Type    { return Number }
func (e *StringLit) ExprType() Type    { return String }
func (e *BoolLit) ExprType() Type      { return Boolean }
func (e *NullLit) ExprType() Type      { return Null }
func (e *UndefinedLit) ExprType() Type { return Undefined }
func (e *Ident) ExprType() Type        { return e.Typ }
func (e *Binary) ExprType() Type       { return e.Typ }
func (e *Unary) ExprType() Type        { return e.Typ }
func (e *Call) ExprType() Type         { return e.Typ }
func (e *ConsoleLog) ExprType() Type   { return Void }

func (e *NumberLit) ExprSpan() Span    { return e.Span }
func (e *StringLit) ExprSpan() Span    { return e.Span }
func (e *BoolLit) ExprSpan() Span      { return e.Span }
func (e *NullLit) ExprSpan() Span      { return e.Span }
func (e *UndefinedLit) ExprSpan() Span { return e.Span }
func (e *Ident) ExprSpan() Span        { return e.Span }
func (e *Binary) ExprSpan() Span       { return e.Span }
func (e *Unary) ExprSpan() Span        { return e.Span }
func (e *Call) ExprSpan() Span         { return e.Span }
func (e *ConsoleLog) ExprSpan() Span   { return e.Span }
