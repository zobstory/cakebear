package ir

import "strings"

// Kind says which family a Type belongs to.
//
// It is today's primitive enum, widened by KindFunc, KindHost, KindPromise,
// KindObject and KindArray.
// The order is load-bearing in one place: IsExtension tests the
// KindInt32..KindBase64 range, so new kinds are appended after KindBase64
// rather than inserted before it.
type Kind int

const (
	KindInvalid Kind = iota // the zero value, so an unset Type is Invalid
	KindNumber
	KindString
	KindBoolean
	KindVoid
	KindNull
	KindUndefined

	// cakebear's extension types. Unlike the above, these have no TypeScript
	// equivalent: they exist so the backend can use a native machine type
	// instead of float64. See types/cakebear.d.ts.
	KindInt32
	KindInt64
	KindUint32
	KindUint64
	KindFloat32
	KindBase64

	// KindFunc is a function value. Sig holds its parameters and result.
	KindFunc
	// KindHost is a value owned by the host runtime, such as a Node HTTP
	// request. Host says which one.
	KindHost
	// KindPromise is TypeScript's own Promise<T>, not a bundled copy. Elem is
	// T, what the promise resolves to; Promise<void> has Elem Void.
	KindPromise
	// KindObject is an object of an exact shape: Shape says which (M4 plan
	// D2, D3). An object is a reference, as in JavaScript.
	KindObject
	// KindArray is T[], and ReadonlyArray<T>, whose readonliness is checked
	// at compile time only (D7). Elem is T. An array is a reference too, so a
	// push is seen through every alias.
	KindArray
	// KindMaybe is `T | undefined`, `T | null` or both (M5 plan D2): a value
	// that is T, or is undefined, or is null. Elem is T. The value carries
	// which it is, and TypeScript's checker holds a program to the states its
	// types admit, so the IR keeps no record of them: `T | null` flows into a
	// `T | null | undefined` slot unchanged, as TypeScript allows.
	KindMaybe
	// KindUnknown is TypeScript's `unknown` (M6 plan D5): a value of any type
	// the subset holds, boxed (errors.go). It has no fields.
	KindUnknown
	// KindError is an Error of a built-in kind (M6 plan D4), which the value
	// carries. It has no fields: its properties are ErrorGet's.
	KindError
)

// HostType names a value the host runtime owns, such as the object a Node
// `createServer` call returns.
//
// It is a closed enum of abstract names, not Go type names: ir/ stays
// backend-agnostic, and backend/ is what maps each one onto a runtime type.
// Growing the set means adding a constant here and teaching backend/ about it.
type HostType int

const (
	HostNone HostType = iota // the zero value, for every non-host Type
	HostHTTPServer
	HostIncomingMessage
	HostServerResponse
)

func (h HostType) String() string {
	switch h {
	case HostHTTPServer:
		return "Server"
	case HostIncomingMessage:
		return "IncomingMessage"
	case HostServerResponse:
		return "ServerResponse"
	default:
		return "host"
	}
}

// Signature is the parameter and result types of a function. It carries no
// names or spans: those belong to a declaration, not to the type.
type Signature struct {
	Params []Type
	Result Type
}

// Equal reports whether s and o describe the same function type.
func (s *Signature) Equal(o *Signature) bool {
	if s == nil || o == nil {
		return s == o
	}
	if len(s.Params) != len(o.Params) || !s.Result.Equal(o.Result) {
		return false
	}
	for i, p := range s.Params {
		if !p.Equal(o.Params[i]) {
			return false
		}
	}
	return true
}

// Type is the type surface of the IR. TypeScript's full type system is far
// richer; this is only what the backend can currently give a representation to,
// and lower/ refuses anything outside it rather than guessing.
//
// Only the field that Kind selects is meaningful: Host for KindHost, Sig for
// KindFunc, Elem for KindPromise, KindArray and KindMaybe, Shape for
// KindObject, none for a primitive. The primitives are package-level values
// (Number, String, ...), so `t == ir.Number` and `switch t { case ir.Number: }`
// keep working. Between two function, promise, array or maybe types ==
// compares the pointers, so use Equal there.
type Type struct {
	Kind  Kind
	Host  HostType   // KindHost only
	Sig   *Signature // KindFunc only
	Elem  *Type      // KindPromise, KindArray and KindMaybe only
	Shape *Shape     // KindObject only: interned, so == on it is structural
}

var (
	Invalid   = Type{Kind: KindInvalid}
	Number    = Type{Kind: KindNumber}
	String    = Type{Kind: KindString}
	Boolean   = Type{Kind: KindBoolean}
	Void      = Type{Kind: KindVoid}
	Null      = Type{Kind: KindNull}
	Undefined = Type{Kind: KindUndefined}

	// cakebear's extension types. See Kind.
	Int32   = Type{Kind: KindInt32}
	Int64   = Type{Kind: KindInt64}
	Uint32  = Type{Kind: KindUint32}
	Uint64  = Type{Kind: KindUint64}
	Float32 = Type{Kind: KindFloat32}
	Base64  = Type{Kind: KindBase64}
)

// FuncType returns the type of a function with the given parameters and result.
func FuncType(params []Type, result Type) Type {
	return Type{Kind: KindFunc, Sig: &Signature{Params: params, Result: result}}
}

// HostOf returns the type of a value the host runtime owns.
func HostOf(h HostType) Type {
	return Type{Kind: KindHost, Host: h}
}

// PromiseOf returns Promise<elem>.
func PromiseOf(elem Type) Type {
	return Type{Kind: KindPromise, Elem: &elem}
}

// ArrayOf returns elem[].
func ArrayOf(elem Type) Type {
	return Type{Kind: KindArray, Elem: &elem}
}

// MaybeOf returns `elem | null | undefined` (KindMaybe). elem is never itself
// a Maybe, void, null or undefined: Validate holds lowering to that.
func MaybeOf(elem Type) Type {
	return Type{Kind: KindMaybe, Elem: &elem}
}

// ObjectOf returns the type of an object of shape s, which must come from the
// program's Shapes.
func ObjectOf(s *Shape) Type {
	return Type{Kind: KindObject, Shape: s}
}

// ResolveType is the type of a Promise executor's resolve parameter, for a
// promise of elem: `(elem) => void`, or `() => void` for Promise<void>.
//
// TypeScript types it `(value: T | PromiseLike<T>) => void`. The union is
// there for adopting another promise, which M2 refuses, so what is left is a
// plain function, the shape that lets `setTimeout(resolve, ms)` type-check
// against a `() => void` callback.
func ResolveType(elem Type) Type {
	if elem == Void {
		return FuncType(nil, Void)
	}
	return FuncType([]Type{elem}, Void)
}

// RejectType is the type of a Promise executor's reject parameter:
// `(reason: unknown) => void`. TypeScript types the reason `any`, which the
// subset doesn't hold; whatever it is, the promise is rejected with it, as
// an unknown holds it (M6 plan D7).
func RejectType() Type {
	return FuncType([]Type{Unknown}, Void)
}

// ExecutorType is the type of the function `new Promise` takes, for a promise
// of elem: `(resolve, reject) => void`. An executor declaring only resolve
// is padded to it, as any function literal is to its slot.
func ExecutorType(elem Type) Type {
	return FuncType([]Type{ResolveType(elem), RejectType()}, Void)
}

// Equal reports whether t and u are the same type, comparing structurally.
//
// Primitives are equal when their kinds are. Host types are equal when they
// name the same HostType. Function types are equal when their parameters and
// results are, however the two Signatures were built; == would compare the
// pointers and call two identical signatures different. Promise, array and
// maybe types are equal when their element types are, for the same reason. Object
// types are equal when they have the same shape, which interning makes the
// same as having the same fields.
func (t Type) Equal(u Type) bool {
	if t.Kind != u.Kind {
		return false
	}
	switch t.Kind {
	case KindHost:
		return t.Host == u.Host
	case KindFunc:
		return t.Sig.Equal(u.Sig)
	case KindPromise, KindArray, KindMaybe:
		if t.Elem == nil || u.Elem == nil {
			return t.Elem == u.Elem
		}
		return t.Elem.Equal(*u.Elem)
	case KindObject:
		return t.Shape == u.Shape
	default:
		return true
	}
}

// IsExtension reports whether t is one of cakebear's own types rather than a
// TypeScript primitive.
func (t Type) IsExtension() bool {
	return t.Kind >= KindInt32 && t.Kind <= KindBase64
}

// IsNumeric reports whether t holds a number, extension or not. Arithmetic and
// comparison apply to exactly these.
func (t Type) IsNumeric() bool {
	return t.Kind == KindNumber || (t.IsExtension() && t.Kind != KindBase64)
}

func (t Type) String() string {
	switch t.Kind {
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindBoolean:
		return "boolean"
	case KindVoid:
		return "void"
	case KindNull:
		return "null"
	case KindUndefined:
		return "undefined"
	case KindInt32:
		return "i32"
	case KindInt64:
		return "i64"
	case KindUint32:
		return "u32"
	case KindUint64:
		return "u64"
	case KindFloat32:
		return "f32"
	case KindBase64:
		return "base64"
	case KindFunc:
		return t.Sig.String()
	case KindHost:
		return t.Host.String()
	case KindPromise:
		if t.Elem == nil {
			return "Promise<invalid>"
		}
		return "Promise<" + t.Elem.String() + ">"
	case KindArray:
		if t.Elem == nil {
			return "invalid[]"
		}
		if t.Elem.Kind == KindFunc || t.Elem.Kind == KindMaybe {
			return "(" + t.Elem.String() + ")[]"
		}
		return t.Elem.String() + "[]"
	case KindObject:
		return t.Shape.String()
	case KindMaybe:
		if t.Elem == nil {
			return "invalid | null | undefined"
		}
		if t.Elem.Kind == KindFunc {
			return "(" + t.Elem.String() + ") | null | undefined"
		}
		return t.Elem.String() + " | null | undefined"
	case KindUnknown:
		return "unknown"
	case KindError:
		return "Error"
	default:
		return "invalid"
	}
}

// String spells the signature as TypeScript would: `(number, string) => void`.
func (s *Signature) String() string {
	if s == nil {
		return "() => invalid"
	}
	params := make([]string, len(s.Params))
	for i, p := range s.Params {
		params[i] = p.String()
	}
	return "(" + strings.Join(params, ", ") + ") => " + s.Result.String()
}
