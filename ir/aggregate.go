package ir

// Objects and arrays (M4 plan D2–D9, D13, D16). An object or an array is a
// reference, as in JavaScript: an Ident holding one and a second Ident
// assigned from it are the same value, and `===` compares identity.

// ObjectLit is an object literal of an exact shape, Typ's. Fields are in the
// order the source writes them: the order they are evaluated in and, since
// JavaScript keeps insertion order, the order Node prints them and
// JSON.stringify writes them (D4). Each sets one field of the shape, and
// every field is set exactly once: M4's shapes have only required
// properties.
type ObjectLit struct {
	Fields []FieldInit
	Typ    Type
	Span   Span
}

// FieldInit sets one field of a literal: the shape's field at Field, its
// index in Shape.Fields.
type FieldInit struct {
	Field int
	Value Expr
	Span  Span
}

// ArrayLit is an array literal. Typ comes from the slot it fills, since
// TypeScript types `[]` alone as never[].
type ArrayLit struct {
	Elems []Expr
	Typ   Type
	Span  Span
}

// FieldGet is `obj.f`: the field at Field in Object's shape.
type FieldGet struct {
	Object Expr
	Field  int
	Typ    Type
	Span   Span
}

// IndexGet is `xs[i]`. An index outside the array panics (D8), where Node
// reads undefined, unless OrUndefined is set: then the read is undefined
// there, as Node's is, and Typ is OrUndefined of the element type (M5 plan
// D9). Lowering sets it where the read may be undefined: under
// noUncheckedIndexedAccess, which types it `T | undefined`, and for an array
// of maybes, which can hold the undefined.
type IndexGet struct {
	Array       Expr
	Index       Expr
	Typ         Type
	OrUndefined bool
	Span        Span
}

// ArrayMethod is one of the array members M4 lowers (D16). Each is
// recognised by where its symbol is declared, as node:http's members are.
type ArrayMethod int

const (
	ArrayMethodInvalid ArrayMethod = iota // the zero value, never lowered
	ArrayLength                           // xs.length
	ArraySetLength                        // xs.length = n, which may only shrink the array (D9)
	ArrayPush                             // xs.push(a, …), the new length
	ArrayPop                              // xs.pop(): its value is OrUndefined(T), or void as a statement
	ArrayMap
	ArrayFilter
	ArrayJoin
	ArrayIncludes
	ArraySlice
	ArraySort // with a comparator only: the default sort compares as strings
	ArraySome
	ArrayEvery
	ArrayForEach
	ArrayReduce // with an initial value only
	ArrayFindIndex
	ArrayIndexOf
	ArrayFind // OrUndefined(T), M5's (D9), as are the three below
	ArrayFindLast
	ArrayAt
	ArrayShift // xs.shift(), as pop: or void as a statement
)

var arrayMethodNames = map[ArrayMethod]string{
	ArrayLength: "length", ArraySetLength: "length =", ArrayPush: "push", ArrayPop: "pop",
	ArrayMap: "map", ArrayFilter: "filter", ArrayJoin: "join", ArrayIncludes: "includes",
	ArraySlice: "slice", ArraySort: "sort", ArraySome: "some", ArrayEvery: "every",
	ArrayForEach: "forEach", ArrayReduce: "reduce", ArrayFindIndex: "findIndex", ArrayIndexOf: "indexOf",
	ArrayFind: "find", ArrayFindLast: "findLast", ArrayAt: "at", ArrayShift: "shift",
}

func (m ArrayMethod) String() string {
	if name, ok := arrayMethodNames[m]; ok {
		return name
	}
	return "invalid array method"
}

// ArrayOp is a member of an array, read or called: `xs.length`,
// `xs.push(x)`, `xs.map(f)`. A callback is a function value of the
// signature its slot has, (T, number, T[]) => U, padded by lowering as any
// callback is; Program.Validate holds each method to its own (checkArrayOp).
type ArrayOp struct {
	Method ArrayMethod
	Array  Expr
	Args   []Expr
	Typ    Type
	Span   Span
}

// FieldSet is `obj.f = v`. Object is evaluated before Value, as in
// JavaScript. When f is optional and obj lacks it, obj gains it, its key
// last, as Node orders a property assigned after the object was made (M5
// plan D3).
type FieldSet struct {
	Object Expr
	Field  int
	Value  Expr
	Span   Span
}

// FieldDelete is `delete obj.f`, f an optional field, which TypeScript
// allows deleting (D3). obj no longer has f, and reads it as undefined.
type FieldDelete struct {
	Object Expr
	Field  int
	Span   Span
}

// HasField is `"f" in obj`: whether obj has f. It has a required field
// always, and an optional one once it is given one, even undefined (D3).
type HasField struct {
	Object Expr
	Field  int
	Span   Span
}

// IndexSet is `xs[i] = v`, evaluated in that order. i may be the array's
// length, which appends; beyond it, below zero or fractional panics (D9).
type IndexSet struct {
	Array Expr
	Index Expr
	Value Expr
	Span  Span
}

// ForOf is `for (const x of xs) body`, x being Name. Each iteration has its
// own binding, as in JavaScript. The length is read again before every
// iteration, so an element pushed inside the loop is visited, as Node visits
// it (D13): a backend must not iterate over a snapshot.
type ForOf struct {
	Name    string
	Type    Type // the element type, x's
	Mutable bool // let, versus const
	Array   Expr
	Body    []Stmt
	Span    Span
}

func (*ObjectLit) isExpr()          {}
func (*ArrayLit) isExpr()           {}
func (*FieldGet) isExpr()           {}
func (*IndexGet) isExpr()           {}
func (*ArrayOp) isExpr()            {}
func (*HasField) isExpr()           {}
func (e *ObjectLit) ExprType() Type { return e.Typ }
func (e *ArrayLit) ExprType() Type  { return e.Typ }
func (e *FieldGet) ExprType() Type  { return e.Typ }
func (e *IndexGet) ExprType() Type  { return e.Typ }
func (e *ArrayOp) ExprType() Type   { return e.Typ }
func (e *HasField) ExprType() Type  { return Boolean }
func (e *ObjectLit) ExprSpan() Span { return e.Span }
func (e *ArrayLit) ExprSpan() Span  { return e.Span }
func (e *FieldGet) ExprSpan() Span  { return e.Span }
func (e *IndexGet) ExprSpan() Span  { return e.Span }
func (e *ArrayOp) ExprSpan() Span   { return e.Span }
func (e *HasField) ExprSpan() Span  { return e.Span }

func (*FieldSet) isStmt()             {}
func (*IndexSet) isStmt()             {}
func (*ForOf) isStmt()                {}
func (*FieldDelete) isStmt()          {}
func (s *FieldSet) StmtSpan() Span    { return s.Span }
func (s *IndexSet) StmtSpan() Span    { return s.Span }
func (s *ForOf) StmtSpan() Span       { return s.Span }
func (s *FieldDelete) StmtSpan() Span { return s.Span }
