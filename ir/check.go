package ir

import (
	"errors"
	"fmt"
)

// The checks Validate makes of shapes, types and the object and array nodes.
// A field index is what the backend reads a struct field by, so one that
// is off by one reads the wrong field, silently if the two have one type.
// That is why these are checked rather than trusted.

// validateShapes checks Program.Shapes: each at its own ID, defined, its
// fields in canonical order with types the program can hold, and no
// structure twice.
func (p *Program) validateShapes() []error {
	var errs []error
	seen := map[string]int{}
	for i, s := range p.Shapes {
		switch {
		case s == nil:
			errs = append(errs, fmt.Errorf("shape %d is nil", i))
			continue
		case s.ID != i:
			errs = append(errs, fmt.Errorf("shape %d (%s) has ID %d", i, s, s.ID))
		case s.pending:
			errs = append(errs, fmt.Errorf("shape %d (%s) was reserved and never defined", i, s))
		}
		for j, f := range s.Fields {
			if j > 0 && s.Fields[j-1].Name >= f.Name {
				errs = append(errs, fmt.Errorf("shape %s has fields %q and %q out of order, or one twice", s, s.Fields[j-1].Name, f.Name))
			}
			if err := p.checkType(f.Type); err != nil {
				errs = append(errs, fmt.Errorf("field %s of %s: %w", f.Name, s, err))
			}
			if f.Optional && f.Type.Kind != KindMaybe {
				errs = append(errs, fmt.Errorf("optional field %s of %s is a %s, where an optional property is a maybe", f.Name, s, f.Type))
			}
		}
		key := shapeKey(s.Fields)
		if j, ok := seen[key]; ok {
			errs = append(errs, fmt.Errorf("shapes %d and %d have the same fields, %s, where interning makes them one", j, i, s))
		}
		seen[key] = i
	}
	return errs
}

// checkType checks that t is well formed, and that an object type's shape is
// one of the program's.
func (p *Program) checkType(t Type) error {
	switch t.Kind {
	case KindObject:
		if s := t.Shape; s == nil || s.ID < 0 || s.ID >= len(p.Shapes) || p.Shapes[s.ID] != s {
			return fmt.Errorf("an object of %s, which is not one of the program's shapes", t.Shape)
		}
	case KindArray, KindPromise:
		if t.Elem == nil {
			return fmt.Errorf("%s has no element type", t)
		}
		return p.checkType(*t.Elem)
	case KindMaybe:
		return p.checkMaybe(t)
	case KindFunc:
		if t.Sig == nil {
			return errors.New("a function type with no signature")
		}
		for _, param := range t.Sig.Params {
			if err := p.checkType(param); err != nil {
				return err
			}
		}
		return p.checkType(t.Sig.Result)
	}
	return nil
}

// checkNode checks one statement or expression: the types it carries, and
// for the object and array nodes, that their fields, elements and arguments
// fit the shapes and element types they name.
func (p *Program) checkNode(node any) error {
	if e, ok := node.(Expr); ok {
		if err := p.checkType(e.ExprType()); err != nil {
			return err
		}
	}
	switch n := node.(type) {
	case *VarDecl:
		if err := p.checkType(n.Type); err != nil {
			return err
		}
		return checkDecl(n)
	case *FuncLit:
		if err := p.checkParams(n.Params); err != nil {
			return err
		}
		return checkReturns(n.Body, returnType(n.Result, n.Async))
	case *ObjectLit:
		return p.checkObjectLit(n)
	case *ArrayLit:
		elem, err := elemOf(n.Typ)
		for i, x := range n.Elems {
			if err == nil && !x.ExprType().Equal(elem) {
				err = fmt.Errorf("element %d of a %s literal is a %s", i, n.Typ, x.ExprType())
			}
		}
		return err
	case *FieldGet:
		return checkField(n.Object, n.Field, n.Typ)
	case *FieldSet:
		return checkField(n.Object, n.Field, n.Value.ExprType())
	case *IndexGet:
		return checkIndex(n.Array, n.Index, n.Typ)
	case *IndexSet:
		return checkIndex(n.Array, n.Index, n.Value.ExprType())
	case *ArrayOp:
		return checkArrayOp(n)
	case *JSONStringify:
		return checkJSONStringify(n)
	case *BuiltinCall:
		return checkBuiltin(n)
	case *Concat:
		return checkConcat(n)
	case *Wrap, *Unwrap, *Empty, *IsEmpty, *Cond:
		return checkMaybeNode(n)
	case *Append:
		return checkAppend(n)
	case *ForOf:
		elem, err := elemOf(n.Array.ExprType())
		if err == nil && !n.Type.Equal(elem) {
			err = fmt.Errorf("for…of over a %s binds %s as a %s", n.Array.ExprType(), n.Name, n.Type)
		}
		return err
	}
	return nil
}

func (p *Program) checkParams(params []Param) error {
	for _, param := range params {
		if err := p.checkType(param.Type); err != nil {
			return fmt.Errorf("parameter %s: %w", param.Name, err)
		}
	}
	return nil
}

func (p *Program) checkObjectLit(x *ObjectLit) error {
	if x.Typ.Kind != KindObject {
		return fmt.Errorf("an object literal typed %s", x.Typ)
	}
	s := x.Typ.Shape
	set := make([]bool, len(s.Fields))
	for _, f := range x.Fields {
		if f.Field < 0 || f.Field >= len(s.Fields) {
			return fmt.Errorf("a %s literal sets field %d of %d", s, f.Field, len(s.Fields))
		}
		field := s.Fields[f.Field]
		if set[f.Field] {
			return fmt.Errorf("a %s literal sets %s twice", s, field.Name)
		}
		set[f.Field] = true
		if !f.Value.ExprType().Equal(field.Type) {
			return fmt.Errorf("a %s literal sets %s, a %s, to a %s", s, field.Name, field.Type, f.Value.ExprType())
		}
	}
	for i, ok := range set {
		if !ok && !s.Fields[i].Optional {
			return fmt.Errorf("a %s literal leaves %s unset", s, s.Fields[i].Name)
		}
	}
	return nil
}

// checkField checks a read or a write of field index of object, whose value
// is of type t.
func checkField(object Expr, index int, t Type) error {
	ot := object.ExprType()
	if ot.Kind != KindObject || ot.Shape == nil {
		return fmt.Errorf("a field of a %s", ot)
	}
	s := ot.Shape
	if index < 0 || index >= len(s.Fields) {
		return fmt.Errorf("field %d of %s, which has %d", index, s, len(s.Fields))
	}
	if f := s.Fields[index]; !t.Equal(f.Type) {
		return fmt.Errorf("%s of %s is a %s, used as a %s", f.Name, s, f.Type, t)
	}
	return nil
}

// checkIndex checks a read or a write of array[index], whose value is of
// type t.
func checkIndex(array, index Expr, t Type) error {
	elem, err := elemOf(array.ExprType())
	switch {
	case err != nil:
		return err
	case index.ExprType() != Number:
		return fmt.Errorf("an array indexed by a %s", index.ExprType())
	case !t.Equal(elem):
		return fmt.Errorf("an element of a %s used as a %s", array.ExprType(), t)
	}
	return nil
}

func elemOf(t Type) (Type, error) {
	if t.Kind != KindArray || t.Elem == nil {
		return Invalid, fmt.Errorf("an array operation on a %s", t)
	}
	return *t.Elem, nil
}

// checkArrayOp holds each method to its arguments and result (D16). A
// callback has the full signature its slot does, (T, number, T[]) => U,
// since lowering pads a literal declaring fewer parameters.
func checkArrayOp(x *ArrayOp) error {
	arr := x.Array.ExprType()
	elem, err := elemOf(arr)
	if err != nil {
		return err
	}
	args := func(types ...Type) error {
		if len(x.Args) != len(types) {
			return fmt.Errorf("%s takes %d argument(s), not %d", x.Method, len(types), len(x.Args))
		}
		for i, t := range types {
			if got := x.Args[i].ExprType(); !got.Equal(t) {
				return fmt.Errorf("%s's argument %d is a %s, not a %s", x.Method, i+1, got, t)
			}
		}
		return nil
	}
	visitor := func(result Type) Type { return FuncType([]Type{elem, Number, arr}, result) }

	var result Type
	switch x.Method {
	case ArrayLength, ArrayPop:
		result, err = Number, args()
		if x.Method == ArrayPop {
			result = Void
		}
	case ArraySetLength:
		result, err = Void, args(Number)
	case ArrayPush:
		result, err = Number, args(elem)
		if len(x.Args) > 1 {
			types := make([]Type, len(x.Args))
			for i := range types {
				types[i] = elem
			}
			err = args(types...)
		}
	case ArrayMap:
		var mapped Type
		if len(x.Args) == 1 && x.Args[0].ExprType().Kind == KindFunc && x.Args[0].ExprType().Sig != nil {
			mapped = x.Args[0].ExprType().Sig.Result
		}
		result, err = ArrayOf(mapped), args(visitor(mapped))
	case ArrayFilter:
		result, err = arr, args(visitor(Boolean))
	case ArraySome, ArrayEvery:
		result, err = Boolean, args(visitor(Boolean))
	case ArrayFindIndex:
		result, err = Number, args(visitor(Boolean))
	case ArrayForEach:
		result, err = Void, args(visitor(Void))
	case ArrayJoin:
		result, err = String, args()
		if len(x.Args) == 1 {
			err = args(String)
		}
	case ArrayIncludes:
		result, err = Boolean, args(elem)
	case ArrayIndexOf:
		result, err = Number, args(elem)
	case ArraySlice:
		types := []Type{Number, Number}[:min(len(x.Args), 2)]
		result, err = arr, args(types...)
		if len(x.Args) > 2 {
			err = fmt.Errorf("slice takes at most 2 arguments, not %d", len(x.Args))
		}
	case ArraySort:
		result, err = arr, args(FuncType([]Type{elem, elem}, Number))
	case ArrayReduce:
		var acc Type
		if len(x.Args) == 2 {
			acc = x.Args[1].ExprType()
		}
		result, err = acc, args(FuncType([]Type{acc, elem, Number, arr}, acc), acc)
	default:
		return fmt.Errorf("%s on a %s", x.Method, arr)
	}
	if err == nil && !x.Typ.Equal(result) {
		err = fmt.Errorf("%s on a %s gives a %s, typed %s", x.Method, arr, result, x.Typ)
	}
	return err
}

func nodeSpan(node any) Span {
	switch n := node.(type) {
	case Expr:
		return n.ExprSpan()
	case Stmt:
		return n.StmtSpan()
	}
	return Span{}
}
