package ir

import (
	"errors"
	"fmt"
	"slices"
)

// Errors (M6 plan D2-D5): throw, try/catch/finally, Error and its built-in
// kinds as values, and unknown, which is what a catch binds. Nothing in the
// IR says how a throw travels: the backend's representation (D2) is its own.

// Unknown is TypeScript's `unknown` (D5): a value of any type the subset
// holds, undefined and null included. Anything flows in, by a Box; nothing
// flows out but by narrowing, an Unbox, which is TypeScript's own rule.
var Unknown = Type{Kind: KindUnknown}

// Error is an Error of one of the built-in kinds (D4): TypeError and the rest
// are one type, since TypeScript's libs declare each as an Error with nothing
// added. The value carries its kind; InstanceOf tests it.
var Error = Type{Kind: KindError}

// ErrorKind is one of the built-in Error constructors. Each is a direct
// subclass of Error, so `instanceof Error` holds for all of them.
type ErrorKind int

const (
	ErrorBase      ErrorKind = iota // Error itself
	ErrorType                       // TypeError
	ErrorRange                      // RangeError
	ErrorURI                        // URIError
	ErrorSyntax                     // SyntaxError
	ErrorReference                  // ReferenceError
	ErrorEval                       // EvalError
)

// errorKindNames are the constructors' names, by kind, which is also what an
// error's name is.
var errorKindNames = []string{"Error", "TypeError", "RangeError", "URIError", "SyntaxError", "ReferenceError", "EvalError"}

func (k ErrorKind) String() string {
	if k < 0 || int(k) >= len(errorKindNames) {
		return fmt.Sprintf("ErrorKind(%d)", int(k))
	}
	return errorKindNames[k]
}

// ErrorKindNamed is the kind whose constructor is called name, if one is.
func ErrorKindNamed(name string) (ErrorKind, bool) {
	i := slices.Index(errorKindNames, name)
	return ErrorKind(i), i >= 0
}

// Throw is `throw v`, of a value of any type the subset holds.
type Throw struct {
	Value Expr
	Span  Span
}

// Try is `try { Body } catch (Name) { … } finally { … }`. Catch is nil with no
// catch clause, and Finally is nil with no finally clause; an empty finally is
// an empty, non-nil slice. Each of a try's ways out, falling off the end,
// return, break, continue and a throw, runs Finally first, and Finally's own
// way out replaces the one pending, as in JavaScript.
type Try struct {
	Body    []Stmt
	Catch   *Catch
	Finally []Stmt
	Span    Span
}

// Catch is a catch clause. Name is "" for `catch {}`, and is otherwise a
// binding of Unknown, the thrown value: `useUnknownInCatchVariables` is on
// under strict, which cakebear requires.
type Catch struct {
	Name string
	Body []Stmt
	Span Span
}

// NewError is `new Error(message)`, `Error(message)` or one of the built-in
// kinds. Message is nil for `new Error()` and `new Error(undefined)`, which
// leave the error with no message of its own, and a string otherwise:
// lowering converts any other value as JavaScript's ToString does.
type NewError struct {
	Kind    ErrorKind
	Message Expr
	Span    Span
}

// ErrorProp is a property an Error has.
type ErrorProp int

const (
	ErrorMessage ErrorProp = iota // .message, a string
	ErrorName                     // .name, a string
	ErrorStack                    // .stack, `string | undefined`
	ErrorCause                    // .cause, unknown
)

func (p ErrorProp) String() string {
	switch p {
	case ErrorMessage:
		return "message"
	case ErrorName:
		return "name"
	case ErrorStack:
		return "stack"
	case ErrorCause:
		return "cause"
	default:
		return fmt.Sprintf("ErrorProp(%d)", int(p))
	}
}

// errorPropType is the type of an Error's property p, as TypeScript's libs
// declare it.
func errorPropType(p ErrorProp) Type {
	switch p {
	case ErrorMessage, ErrorName:
		return String
	case ErrorStack:
		return MaybeOf(String)
	case ErrorCause:
		return Unknown
	default:
		return Invalid
	}
}

// ErrorGet is a read of an Error's property: `e.message`.
type ErrorGet struct {
	Error Expr
	Prop  ErrorProp
	Span  Span
}

// Box is a value where an unknown is wanted (D5). Value is of any type the
// subset holds but void, an extension type, which lowering widens to the
// number or string it stands for first, and unknown itself. A maybe is
// boxed as the value its state is.
//
// What an unknown holds is printed and serialised as it is at run time, where
// nothing can refuse it, so only data gets in: no function, promise or host
// value, nor anything holding one, the same as JSON.stringify takes. A Throw
// is held to the same, since a catch binds what it threw as an unknown.
type Box struct {
	Value Expr
	Span  Span
}

// Unbox is an unknown used as the T TypeScript has narrowed it to, by
// `typeof`, `instanceof` or a test against null or undefined. It is checked,
// as Unwrap is: a narrowing can be unsound.
type Unbox struct {
	Value Expr
	Typ   Type
	Span  Span
}

// InstanceOf is `v instanceof K` for a built-in Error kind. Value is an
// unknown, an Error, or a maybe of one: an Error is of that kind, or of a
// kind below it, and a maybe holds one.
type InstanceOf struct {
	Value Expr
	Kind  ErrorKind
	Span  Span
}

// TypeTag is what `typeof v` gives.
type TypeTag int

const (
	TagUndefined TypeTag = iota
	TagObject            // an object, an array, an Error, or null
	TagBoolean
	TagNumber
	TagString
	TagFunction
	TagSymbol
	TagBigint
)

var typeTagNames = []string{"undefined", "object", "boolean", "number", "string", "function", "symbol", "bigint"}

func (t TypeTag) String() string {
	if t < 0 || int(t) >= len(typeTagNames) {
		return fmt.Sprintf("TypeTag(%d)", int(t))
	}
	return typeTagNames[t]
}

// TypeTagNamed is the tag typeof gives as name, if it is one.
func TypeTagNamed(name string) (TypeTag, bool) {
	i := slices.Index(typeTagNames, name)
	return TypeTag(i), i >= 0
}

// TypeOf is `typeof v === tag`, for v an unknown: anything else's tag is
// known while compiling.
type TypeOf struct {
	Value Expr
	Tag   TypeTag
	Span  Span
}

func (*Throw) isStmt() {}
func (*Try) isStmt()   {}

func (s *Throw) StmtSpan() Span { return s.Span }
func (s *Try) StmtSpan() Span   { return s.Span }

func (*NewError) isExpr()   {}
func (*ErrorGet) isExpr()   {}
func (*Box) isExpr()        {}
func (*Unbox) isExpr()      {}
func (*InstanceOf) isExpr() {}
func (*TypeOf) isExpr()     {}

func (e *NewError) ExprType() Type   { return Error }
func (e *ErrorGet) ExprType() Type   { return errorPropType(e.Prop) }
func (e *Box) ExprType() Type        { return Unknown }
func (e *Unbox) ExprType() Type      { return e.Typ }
func (e *InstanceOf) ExprType() Type { return Boolean }
func (e *TypeOf) ExprType() Type     { return Boolean }

func (e *NewError) ExprSpan() Span   { return e.Span }
func (e *ErrorGet) ExprSpan() Span   { return e.Span }
func (e *Box) ExprSpan() Span        { return e.Span }
func (e *Unbox) ExprSpan() Span      { return e.Span }
func (e *InstanceOf) ExprSpan() Span { return e.Span }
func (e *TypeOf) ExprSpan() Span     { return e.Span }

// checkErrorNode checks the nodes of this file: each holds the types it says
// it carries.
func checkErrorNode(node any) error {
	switch n := node.(type) {
	case *Throw:
		if n.Value == nil {
			return errors.New("a throw of nothing")
		}
		t := n.Value.ExprType()
		if t == Void || t.Kind == KindInvalid {
			return fmt.Errorf("a throw of %s", article(t.String()))
		}
		if what := Unserializable(t, nil); what != "" {
			return fmt.Errorf("a throw of %s, which holds %s", article(t.String()), what)
		}
	case *Try:
		if n.Catch == nil && n.Finally == nil {
			return errors.New("a try with neither a catch nor a finally")
		}
	case *NewError:
		if n.Kind < 0 || int(n.Kind) >= len(errorKindNames) {
			return fmt.Errorf("a new error of %s", n.Kind)
		}
		if n.Message != nil && n.Message.ExprType() != String {
			return fmt.Errorf("a new %s with %s message", n.Kind, article(n.Message.ExprType().String()))
		}
	case *ErrorGet:
		if t := n.Error.ExprType(); t != Error {
			return fmt.Errorf("an error's %s read from %s", n.Prop, article(t.String()))
		}
		if errorPropType(n.Prop) == Invalid {
			return fmt.Errorf("an error's %s", n.Prop)
		}
	case *Box:
		t := n.Value.ExprType()
		switch {
		case t == Void, t == Unknown, t.Kind == KindInvalid, t.IsExtension():
			return fmt.Errorf("%s boxed as unknown", article(t.String()))
		}
		if what := Unserializable(t, nil); what != "" {
			return fmt.Errorf("%s boxed as unknown, which holds %s", article(t.String()), what)
		}
	case *Unbox:
		if t := n.Value.ExprType(); t != Unknown {
			return fmt.Errorf("%s unboxed as %s", article(t.String()), article(n.Typ.String()))
		}
		switch n.Typ.Kind {
		case KindInvalid, KindVoid, KindUnknown:
			return fmt.Errorf("an unknown unboxed as %s", article(n.Typ.String()))
		}
	case *InstanceOf:
		switch t := n.Value.ExprType(); {
		case t == Unknown, t == Error, t.Kind == KindMaybe && t.Elem != nil && *t.Elem == Error:
		default:
			return fmt.Errorf("%s tested for instanceof %s", article(t.String()), n.Kind)
		}
		if n.Kind < 0 || int(n.Kind) >= len(errorKindNames) {
			return fmt.Errorf("instanceof %s", n.Kind)
		}
	case *TypeOf:
		if t := n.Value.ExprType(); t != Unknown {
			return fmt.Errorf("typeof of %s, which is known while compiling", article(t.String()))
		}
		if n.Tag < 0 || int(n.Tag) >= len(typeTagNames) {
			return fmt.Errorf("typeof against %s", n.Tag)
		}
	}
	return nil
}
