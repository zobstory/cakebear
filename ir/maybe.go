package ir

import (
	"errors"
	"fmt"
	"strings"
)

// Null, undefined and the values that may be either (M5 plan D2-D6). A value
// of `T | null | undefined` (KindMaybe) is in one of three states: T, null or
// undefined. These are the nodes that move between them, and Cond, the
// conditional that `?:` is, and that `??` and `?.` are made of, with Let.

// Wrap is a T where a `T | null | undefined` is wanted, in T's state (D6): an
// i32 filling a number's slot is converted, and a T filling a Maybe's is
// wrapped.
type Wrap struct {
	Value Expr
	Typ   Type // MaybeOf(Value's type), or of a string for a base64
	Span  Span
}

// Unwrap is a `T | null | undefined` used as the T TypeScript has narrowed it
// to (D4), or that the program asserts it is, with `x!` (Asserted). It is
// checked: TypeScript's narrowing can be unsound, as when a callback empties
// a property between the check and the use, and an assertion can be wrong.
// Where Node would carry on with undefined, or throw reading a property of
// it, an Unwrap of an empty value fails, saying which.
type Unwrap struct {
	Value    Expr
	Typ      Type // Value's element type
	Asserted bool
	Span     Span
}

// Let is Value, held in a local for Body to read, so that an expression
// tested and then used is evaluated once (D5): `f() ?? d` holds what f
// returned, and `f()?.x` its receiver. Name is lowering's own, with a space in
// it, so it meets none of the program's names, and Body reads it as an Ident
// of Value's type.
type Let struct {
	Name  string
	Value Expr
	Body  Expr
	Span  Span
}

// Empty is undefined, or null when Null is set, as a value of Typ, the Maybe
// its slot wants (D6). A bare `undefined` or `null` has no type of its own to
// be stored in, which is what NullLit and UndefinedLit are: they stand for
// themselves, in a comparison or a log.
type Empty struct {
	Null bool
	Typ  Type // a Maybe
	Span Span
}

// IsEmpty is true when Value, a Maybe or an unknown, is undefined (with Undefined set),
// null (with Null set), or either (both): `x === undefined`, `x === null` and
// `x == null` (D12). Its negation is `!==` and `!= null`.
type IsEmpty struct {
	Value     Expr
	Undefined bool
	Null      bool
	Span      Span
}

// Cond is `Test ? Then : Else`: Test, then one of the two, and only that one
// (D5). `x ?? d` is a Cond on x being empty, and `a?.b` one on a, with the
// rest of the chain inside it.
type Cond struct {
	Test Expr // a boolean
	Then Expr
	Else Expr
	Typ  Type // both branches'
	Span Span
}

func (*Wrap) isExpr()    {}
func (*Unwrap) isExpr()  {}
func (*Empty) isExpr()   {}
func (*IsEmpty) isExpr() {}
func (*Cond) isExpr()    {}
func (*Let) isExpr()     {}

func (e *Wrap) ExprType() Type    { return e.Typ }
func (e *Unwrap) ExprType() Type  { return e.Typ }
func (e *Empty) ExprType() Type   { return e.Typ }
func (e *IsEmpty) ExprType() Type { return Boolean }
func (e *Cond) ExprType() Type    { return e.Typ }
func (e *Let) ExprType() Type     { return e.Body.ExprType() }

func (e *Wrap) ExprSpan() Span    { return e.Span }
func (e *Unwrap) ExprSpan() Span  { return e.Span }
func (e *Empty) ExprSpan() Span   { return e.Span }
func (e *IsEmpty) ExprSpan() Span { return e.Span }
func (e *Cond) ExprSpan() Span    { return e.Span }
func (e *Let) ExprSpan() Span     { return e.Span }

// OrUndefined is t, or a maybe of it: the type of a read that is undefined
// where there is no t to read, past an array's end, say (D9). A maybe is one
// already, its undefined the read's, and so is an unknown, which holds
// undefined itself.
func OrUndefined(t Type) Type {
	if t.Kind == KindMaybe || t == Unknown {
		return t
	}
	return MaybeOf(t)
}

// checkMaybe holds a Maybe's element to a type that can be one of its states'
// values: not void, null or undefined, which a state of their own already is,
// and not another Maybe, which would hold a fourth state no value has.
func (p *Program) checkMaybe(t Type) error {
	if t.Elem == nil {
		return errors.New("a maybe with no element type")
	}
	switch t.Elem.Kind {
	case KindInvalid, KindVoid, KindNull, KindUndefined, KindMaybe:
		return fmt.Errorf("a maybe of %s", t.Elem)
	}
	return p.checkType(*t.Elem)
}

// checkMaybeNode checks the nodes of this file: each holds the types it says
// it moves between.
func checkMaybeNode(node any) error {
	switch n := node.(type) {
	case *Wrap:
		// A base64 may fill a string's maybe: it is a string.
		got := n.Value.ExprType()
		if n.Typ.Kind != KindMaybe || n.Typ.Elem == nil || !got.Equal(*n.Typ.Elem) && !(*n.Typ.Elem == String && got == Base64) {
			return fmt.Errorf("a %s wrapped as a %s", got, n.Typ)
		}
	case *Unwrap:
		// Inspect visits a node before its operand, so the operand's type
		// may not have been checked yet.
		vt := n.Value.ExprType()
		if vt.Kind != KindMaybe || vt.Elem == nil || !n.Typ.Equal(*vt.Elem) {
			return fmt.Errorf("a %s unwrapped as a %s", vt, n.Typ)
		}
	case *Empty:
		if n.Typ.Kind != KindMaybe {
			return fmt.Errorf("an empty %s", n.Typ)
		}
	case *IsEmpty:
		// An unknown is tested as it is: it holds undefined and null as
		// values of their own (M6 plan D5).
		if vt := n.Value.ExprType(); vt.Kind != KindMaybe && vt != Unknown {
			return fmt.Errorf("a %s tested for null or undefined", vt)
		}
		if !n.Undefined && !n.Null {
			return errors.New("a test for neither null nor undefined")
		}
	case *Let:
		if !strings.Contains(n.Name, " ") {
			return fmt.Errorf("a let of %q, a name a program could declare too", n.Name)
		}
	case *Cond:
		switch {
		case n.Test.ExprType() != Boolean:
			return fmt.Errorf("a conditional testing a %s", n.Test.ExprType())
		case !n.Then.ExprType().Equal(n.Typ), !n.Else.ExprType().Equal(n.Typ):
			return fmt.Errorf("a %s conditional choosing between a %s and a %s", n.Typ, n.Then.ExprType(), n.Else.ExprType())
		}
	}
	return nil
}
