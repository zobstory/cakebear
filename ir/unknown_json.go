package ir

import "fmt"

// JSON.parse and the reads of an unknown narrowed to an object or an array
// (S1 plan D3, D4 (b)). What JSON.parse makes is held as an unknown, so these
// are the unknown's operations: reading a property, `in`, Array.isArray and
// an element. TypeScript narrows the unknown to an object (`object & Record<
// "k", unknown>`) or an array (`any[]`) for them, types the IR doesn't hold,
// so lowering takes the value at its stored type, unknown.

// JSONParse is `JSON.parse(text)`, an unknown, or V8's SyntaxError thrown.
type JSONParse struct {
	Text Expr
	Span Span
}

// UnknownGet is `u.key` of an unknown: the property's value, an unknown.
type UnknownGet struct {
	Value Expr
	Key   string
	Span  Span
}

// UnknownIn is `key in u` of an unknown.
type UnknownIn struct {
	Key   Expr
	Value Expr
	Span  Span
}

// UnknownIsArray is `Array.isArray(u)` of an unknown.
type UnknownIsArray struct {
	Value Expr
	Span  Span
}

// UnknownIndex is `u[i]` of an unknown narrowed to an array: the element,
// an unknown.
type UnknownIndex struct {
	Value Expr
	Index Expr
	Span  Span
}

func (*JSONParse) isExpr()      {}
func (*UnknownGet) isExpr()     {}
func (*UnknownIn) isExpr()      {}
func (*UnknownIsArray) isExpr() {}
func (*UnknownIndex) isExpr()   {}

func (*JSONParse) ExprType() Type      { return Unknown }
func (*UnknownGet) ExprType() Type     { return Unknown }
func (*UnknownIn) ExprType() Type      { return Boolean }
func (*UnknownIsArray) ExprType() Type { return Boolean }
func (*UnknownIndex) ExprType() Type   { return Unknown }

func (e *JSONParse) ExprSpan() Span      { return e.Span }
func (e *UnknownGet) ExprSpan() Span     { return e.Span }
func (e *UnknownIn) ExprSpan() Span      { return e.Span }
func (e *UnknownIsArray) ExprSpan() Span { return e.Span }
func (e *UnknownIndex) ExprSpan() Span   { return e.Span }

// checkUnknownJSON holds the nodes above to their operands' types.
func checkUnknownJSON(n Expr) error {
	want := func(e Expr, t Type, what string) error {
		if got := e.ExprType(); got != t {
			return fmt.Errorf("%s of %s, not %s", what, article(got.String()), article(t.String()))
		}
		return nil
	}
	switch n := n.(type) {
	case *JSONParse:
		return want(n.Text, String, "JSON.parse")
	case *UnknownGet:
		return want(n.Value, Unknown, "a read of "+n.Key)
	case *UnknownIn:
		if err := want(n.Key, String, "in with a key"); err != nil {
			return err
		}
		return want(n.Value, Unknown, "in")
	case *UnknownIsArray:
		return want(n.Value, Unknown, "Array.isArray")
	case *UnknownIndex:
		if err := want(n.Index, Number, "an element with an index"); err != nil {
			return err
		}
		return want(n.Value, Unknown, "an element")
	}
	return nil
}
