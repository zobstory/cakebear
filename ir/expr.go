package ir

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
	// Module is the File of the module whose top level declares Name, or
	// empty when Name is local to a function: a parameter, a binding in its
	// body, or one it captures from an enclosing function (Program).
	Module string
	// Stable marks a binding nothing ever assigns after it is declared: a
	// const, a function, or a parameter or let no code writes to. A read of
	// any other binding is ordered against the calls of the expression it is
	// in, as JavaScript orders it (backend/order.go). The zero value is the
	// safe one: a read not known to be stable is ordered.
	Stable bool
	// Appended marks a reference to a string binding the program appends
	// to (append.go).
	Appended bool
	Typ      Type
	Span     Span
}

// BinaryOp is the set of binary operators the Phase-1 subset lowers.
type BinaryOp int

const (
	OpAdd BinaryOp = iota // arithmetic on numbers, concatenation on strings
	OpSub
	OpMul
	OpDiv
	OpMod // JavaScript's %: the remainder with the dividend's sign, as math.Mod
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
	case OpMod:
		return "%"
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
	switch o {
	case OpNeg:
		return "-"
	case OpNot:
		return "!"
	}
	return "?"
}

type Unary struct {
	Op      UnaryOp
	Operand Expr
	Typ     Type
	Span    Span
}

// Call is a call to a named function: a top-level declaration, or a parameter
// or variable holding a function value. Go spells both the same way, so the
// callee stays a name. Calling an arbitrary expression is not in the subset.
type Call struct {
	Callee string
	// Module qualifies Callee as Ident.Module does Name.
	Module string
	Args   []Expr
	Typ    Type
	Span   Span
}

// FuncLit is an arrow function or function expression: a function value.
//
// Its signature is the one the surrounding code expects, when there is one,
// rather than the one the literal would infer alone. TypeScript lets `() => 5`
// stand in for `() => void`, and a callback declare fewer parameters than its
// type passes; Go function types must match exactly. Lowering settles the
// difference, so a Param with an empty Name is one the caller passes and the
// literal ignores.
type FuncLit struct {
	Params []Param
	Result Type
	Body   []Stmt
	// Captures are the enclosing bindings the body refers to, in first-use
	// order. Go closures capture by reference, as JavaScript's do, so the
	// backend needs nothing from this. It is for the checks that must know what
	// a function shares with its surroundings: a handler run on many goroutines
	// at once cannot safely write a binding they all share.
	Captures []Capture
	// Concurrent marks a literal the runtime calls on many goroutines at once,
	// such as createServer's request listener. lower/ has checked that such a
	// literal neither touches a mutable binding it shares with other calls nor
	// calls anything that does (lower/race.go).
	Concurrent bool
	// Async marks an `async` arrow or function expression, with Func.Async's
	// convention: Result is the promise, the Returns in Body carry its element.
	// The exception is a literal standing in for a void callback, such as an
	// async request handler: there Result is Void, the promise is created and
	// dropped, as JavaScript drops it, and the Returns carry nothing.
	Async bool
	Typ   Type
	Span  Span
}

// Await is `await v`. Value has a promise type, and Typ is that promise's
// element: what the expression evaluates to once the promise resolves.
//
// It stays an expression, not a split point in its function. That is what
// keeps the IR free of any one execution model: the Go backend blocks the
// goroutine running the async function, and a backend that rewrote async
// functions into state machines would change how Await emits, not the IR.
// It appears only in an async function's body.
type Await struct {
	Value Expr
	Typ   Type
	Span  Span
}

// NewPromise is `new Promise<T>(executor)`. Typ is PromiseOf(T), and
// Executor is a function value of ExecutorType(T), which the runtime calls
// once, at once, with the resolve function. Rejection is not modelled: M2
// refuses reject, and adopting another promise through resolve, in lowering.
type NewPromise struct {
	Executor Expr
	Typ      Type
	Span     Span
}

// Capture is one enclosing binding a FuncLit refers to.
type Capture struct {
	Name string
	// Module qualifies Name as Ident.Module does: a binding declared at a
	// module's top level, or one of an enclosing function's.
	Module  string
	Type    Type
	Mutable bool // let or a parameter, versus const
}

// ConsoleLog is `console.log(a, b, …)` as an intrinsic rather than a method
// call.
//
// Modelling it as a call would require a `console` binding and a method on
// it. Making it an IR node keeps the subset honest: the backend can lower
// exactly what the language currently supports and reject the rest.
//
// Args print as Node prints them: a string as it is, anything else
// inspected, separated by spaces. Node treats a first string followed by
// more arguments as a format, so lowering allows one there only if it is a
// literal with no `%` in it (M4 plan D12).
//
// It is console.info and console.debug too, which Node writes where log
// writes, and with Stderr, console.error and console.warn (M6 step 6).
type ConsoleLog struct {
	Args []Expr
	// Stderr writes the line to stderr, after whatever stdout holds, so the
	// two keep the program's order where they meet, as Node's do.
	Stderr bool
	Span   Span
}

// Convert is an explicit conversion to one of cakebear's extension types,
// written as `i32(x)` in source, or the widening of one to a number where it
// fills a number's slot, which TypeScript allows and Go doesn't.
//
// Narrowing is never inserted implicitly. TypeScript types `i32 + i32` as
// `number` because adding two 32-bit integers can overflow, so going back is
// something the author writes and the reader can see.
type Convert struct {
	Value Expr
	Typ   Type
	Span  Span
}

func (*Convert) isExpr()          {}
func (e *Convert) ExprType() Type { return e.Typ }
func (e *Convert) ExprSpan() Span { return e.Span }

func (*Await) isExpr()               {}
func (*NewPromise) isExpr()          {}
func (e *Await) ExprType() Type      { return e.Typ }
func (e *NewPromise) ExprType() Type { return e.Typ }
func (e *Await) ExprSpan() Span      { return e.Span }
func (e *NewPromise) ExprSpan() Span { return e.Span }

func (*NumberLit) isExpr()    {}
func (*StringLit) isExpr()    {}
func (*BoolLit) isExpr()      {}
func (*NullLit) isExpr()      {}
func (*UndefinedLit) isExpr() {}
func (*Ident) isExpr()        {}
func (*Binary) isExpr()       {}
func (*Unary) isExpr()        {}
func (*Call) isExpr()         {}
func (*FuncLit) isExpr()      {}
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
func (e *FuncLit) ExprType() Type      { return e.Typ }
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
func (e *FuncLit) ExprSpan() Span      { return e.Span }
func (e *ConsoleLog) ExprSpan() Span   { return e.Span }
