package ir

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
