package ir

// HostOp is an operation the host runtime implements. So far that is the part
// of node:http cakebear bundles (types/node-http.d.ts): lower/ maps each member
// declared there onto one of these, and refuses anything it does not list.
//
// The names are the API's, not Go's, so ir/ stays backend-agnostic. backend/
// decides what each one calls.
type HostOp int

const (
	HostOpInvalid    HostOp = iota // the zero value, never produced by lower/
	HostCreateServer               // createServer(listener)
	HostListen                     // server.listen(port, callback?)
	HostSetHeader                  // res.setHeader(name, value)
	HostWriteHead                  // res.writeHead(statusCode)
	HostEnd                        // res.end(chunk?)
)

func (o HostOp) String() string {
	switch o {
	case HostCreateServer:
		return "createServer"
	case HostListen:
		return "Server.listen"
	case HostSetHeader:
		return "ServerResponse.setHeader"
	case HostWriteHead:
		return "ServerResponse.writeHead"
	case HostEnd:
		return "ServerResponse.end"
	default:
		return "invalid host operation"
	}
}

// HostProperty is a property of a host value.
type HostProperty int

const (
	HostPropInvalid HostProperty = iota // the zero value, never produced by lower/
	HostURL                             // req.url
	HostMethod                          // req.method
)

func (p HostProperty) String() string {
	switch p {
	case HostURL:
		return "IncomingMessage.url"
	case HostMethod:
		return "IncomingMessage.method"
	default:
		return "invalid host property"
	}
}

// HostCall is a call into the host runtime, such as `res.end("ok")`.
//
// Recv is the value the method is called on, and nil for a free function such
// as createServer. The operation's arity was checked when it was lowered, so
// Args always fits it.
type HostCall struct {
	Op   HostOp
	Recv Expr
	Args []Expr
	Typ  Type
	Span Span
}

// HostProp reads a property of a host value, such as `req.url`.
type HostProp struct {
	Prop HostProperty
	Recv Expr
	Typ  Type
	Span Span
}

func (*HostCall) isExpr()          {}
func (*HostProp) isExpr()          {}
func (e *HostCall) ExprType() Type { return e.Typ }
func (e *HostProp) ExprType() Type { return e.Typ }
func (e *HostCall) ExprSpan() Span { return e.Span }
func (e *HostProp) ExprSpan() Span { return e.Span }
