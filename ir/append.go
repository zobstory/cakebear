package ir

import (
	"errors"
	"fmt"
)

// Appending to a string binding (M5 plan D13).
//
// `s += x` on a string, `s = s + x`, and a template beginning with `${s}`
// assigned to s are each an Append: the binding's value with more pieces
// after it. A string binding the program appends to is marked Appended, at
// its declaration (VarDecl, Param) and at every reference (Ident), so a
// backend can keep what it needs to make a loop of appends linear rather than
// copy the whole string each time, as JavaScript engines do with ropes. The
// marks change nothing about what the program means: an Appended binding is a
// string like any other.

// Append is `Target = Target + Parts[0] + …`, where Target is the binding
// read and then written: it is read before the parts are evaluated, as
// JavaScript reads the left of `+=` first.
type Append struct {
	Target *Ident
	Parts  []Expr
	Span   Span
}

func (*Append) isStmt()          {}
func (s *Append) StmtSpan() Span { return s.Span }

// StringParts is the pieces a string concatenation joins, in order: x's
// operands when it is `+` on two strings, or a template's parts, flattened
// through both; and x itself when it is anything else.
func StringParts(x Expr) []Expr {
	switch x := x.(type) {
	case *Binary:
		if x.Op == OpAdd && stringLike(x.Left.ExprType()) && stringLike(x.Right.ExprType()) {
			return append(StringParts(x.Left), StringParts(x.Right)...)
		}
	case *Concat:
		var parts []Expr
		for _, p := range x.Parts {
			parts = append(parts, StringParts(p)...)
		}
		return parts
	}
	return []Expr{x}
}

func checkAppend(s *Append) error {
	switch {
	case s.Target == nil:
		return errors.New("an append with no binding")
	case s.Target.Typ != String:
		return fmt.Errorf("an append to %s, a %s", s.Target.Name, s.Target.Typ)
	case !s.Target.Appended:
		return fmt.Errorf("an append to %s, which is not marked Appended", s.Target.Name)
	case len(s.Parts) == 0:
		return fmt.Errorf("an append to %s of nothing", s.Target.Name)
	}
	for i, p := range s.Parts {
		if !stringLike(p.ExprType()) {
			return fmt.Errorf("part %d appended to %s is a %s", i, s.Target.Name, p.ExprType())
		}
	}
	return nil
}
