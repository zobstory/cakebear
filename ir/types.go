package ir

import "strings"

// Kind says which family a Type belongs to.
//
// It is today's primitive enum, widened by KindFunc and KindHost. The order is
// load-bearing in one place: IsExtension tests the KindInt32..KindBase64 range,
// so new kinds are appended after KindBase64 rather than inserted before it.
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
// KindFunc, neither for a primitive. The primitives are package-level values
// (Number, String, ...), so `t == ir.Number` and `switch t { case ir.Number: }`
// keep working. Between two function types == compares the Sig pointers, so use
// Equal there.
type Type struct {
	Kind Kind
	Host HostType   // KindHost only
	Sig  *Signature // KindFunc only
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

// Equal reports whether t and u are the same type, comparing structurally.
//
// Primitives are equal when their kinds are. Host types are equal when they
// name the same HostType. Function types are equal when their parameters and
// results are, however the two Signatures were built; == would compare the
// pointers and call two identical signatures different.
func (t Type) Equal(u Type) bool {
	if t.Kind != u.Kind {
		return false
	}
	switch t.Kind {
	case KindHost:
		return t.Host == u.Host
	case KindFunc:
		return t.Sig.Equal(u.Sig)
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
