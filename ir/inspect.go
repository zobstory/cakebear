package ir

import "fmt"

// Inspect calls visit on every statement and expression in m, each before its
// children, in source order: the functions, then the top-level code. That
// includes what is inside function literals. visit's argument is a Stmt or an
// Expr.
//
// It knows every node type. One it doesn't, added since, panics here rather
// than be skipped, so a check built on Inspect can't silently miss what the
// new node holds.
func Inspect(m *Module, visit func(node any)) {
	for _, fn := range m.Funcs {
		inspectStmts(fn.Body, visit)
	}
	inspectStmts(m.Main, visit)
}

func inspectStmts(stmts []Stmt, visit func(node any)) {
	for _, s := range stmts {
		inspectStmt(s, visit)
	}
}

func inspectStmt(s Stmt, visit func(node any)) {
	visit(s)
	switch s := s.(type) {
	case *VarDecl:
		inspectExpr(s.Init, visit)
	case *Assign:
		inspectExpr(s.Value, visit)
	case *If:
		inspectExpr(s.Cond, visit)
		inspectStmts(s.Then, visit)
		inspectStmts(s.Else, visit)
	case *While:
		inspectExpr(s.Cond, visit)
		inspectStmts(s.Body, visit)
	case *Return:
		inspectExpr(s.Value, visit)
	case *ExprStmt:
		inspectExpr(s.Expr, visit)
	case *For:
		inspectStmts(s.Init, visit)
		inspectExpr(s.Cond, visit)
		inspectStmts(s.Post, visit)
		inspectStmts(s.Body, visit)
	case *Break, *Continue:
	case *FieldSet:
		inspectExpr(s.Object, visit)
		inspectExpr(s.Value, visit)
	case *IndexSet:
		inspectExpr(s.Array, visit)
		inspectExpr(s.Index, visit)
		inspectExpr(s.Value, visit)
	case *ForOf:
		inspectExpr(s.Array, visit)
		inspectStmts(s.Body, visit)
	default:
		panic(fmt.Sprintf("ir.Inspect: no case for statement %T", s))
	}
}

// inspectExpr visits e, which may be nil: a bare return, or a host call with
// no receiver.
func inspectExpr(e Expr, visit func(node any)) {
	if e == nil {
		return
	}
	visit(e)
	switch e := e.(type) {
	case *NumberLit, *StringLit, *BoolLit, *NullLit, *UndefinedLit, *Ident:
	case *Binary:
		inspectExpr(e.Left, visit)
		inspectExpr(e.Right, visit)
	case *Unary:
		inspectExpr(e.Operand, visit)
	case *Call:
		inspectExprs(e.Args, visit)
	case *FuncLit:
		inspectStmts(e.Body, visit)
	case *Await:
		inspectExpr(e.Value, visit)
	case *NewPromise:
		inspectExpr(e.Executor, visit)
	case *ConsoleLog:
		inspectExprs(e.Args, visit)
	case *Convert:
		inspectExpr(e.Value, visit)
	case *HostCall:
		inspectExpr(e.Recv, visit)
		inspectExprs(e.Args, visit)
	case *HostProp:
		inspectExpr(e.Recv, visit)
	case *ObjectLit:
		for _, f := range e.Fields {
			inspectExpr(f.Value, visit)
		}
	case *ArrayLit:
		inspectExprs(e.Elems, visit)
	case *FieldGet:
		inspectExpr(e.Object, visit)
	case *IndexGet:
		inspectExpr(e.Array, visit)
		inspectExpr(e.Index, visit)
	case *ArrayOp:
		inspectExpr(e.Array, visit)
		inspectExprs(e.Args, visit)
	default:
		panic(fmt.Sprintf("ir.Inspect: no case for expression %T", e))
	}
}

func inspectExprs(es []Expr, visit func(node any)) {
	for _, e := range es {
		inspectExpr(e, visit)
	}
}
