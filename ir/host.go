package ir

// HostOp is an operation the host runtime implements: the part of node:http
// cakebear bundles (types/node-http.d.ts), whose members lower/ maps onto these
// and refuses the rest of, and the DOM timers TypeScript's own libs declare.
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
	HostSetTimeout                 // setTimeout(callback, delay): the callback is () => void
	HostClearTimeout               // clearTimeout(id)
	HostSpawn                      // spawn(fn), cakebear's: fn is () => T or () => Promise<T>
	// fetch (S1 plan D2). Fetch's Args are the input, then init's
	// properties in source order, each named by Keys (FetchKey*).
	HostFetch             // fetch(input, init?)
	HostResponseText      // res.text()
	HostResponseJSON      // res.json(): a promise of an unknown
	HostNewHeaders        // new Headers(init?): Args are values, Keys their names
	HostHeadersGet        // headers.get(name)
	HostHeadersHas        // headers.has(name)
	HostHeadersSet        // headers.set(name, value)
	HostHeadersAppend     // headers.append(name, value)
	HostHeadersDelete     // headers.delete(name)
	HostHeadersSetCookies // headers.getSetCookie()
	// Promise's statics (S1 plan D5).
	HostPromiseAll       // Promise.all(ps), an array of promises
	HostPromiseAllValues // Promise.all(xs), an array of values
	HostPromiseRace      // Promise.race(ps)
	HostPromiseResolve   // Promise.resolve(v) of a value
	HostPromiseReject    // Promise.reject(reason)
	HostPromiseAllOf     // Promise.all([pa, pb, …]) of promises of different types: a promise of nothing
	HostPromiseValue     // the value of Recv, a promise HostPromiseAllOf has seen fulfilled
	// The URL Standard's URL and URLSearchParams, and AbortSignal.timeout
	// (S1 plan D6, D2; url.go).
	HostNewURL             // new URL(input, base?): base a string or a URL
	HostURLCanParse        // URL.canParse(input, base?)
	HostURLParse           // URL.parse(input, base?): a URL or null
	HostURLSet             // u.<Keys[0]> = Args[0], a setter
	HostURLString          // u.toString(), u.toJSON(), String(u) and a template's
	HostNewSearchParams    // new URLSearchParams(init?): a string, or an object literal's values named by Keys
	HostParamsAppend       // params.append(name, value)
	HostParamsDelete       // params.delete(name, value?)
	HostParamsGet          // params.get(name): a string or null
	HostParamsGetAll       // params.getAll(name)
	HostParamsHas          // params.has(name, value?)
	HostParamsSet          // params.set(name, value)
	HostParamsSort         // params.sort()
	HostParamsString       // params.toString(), String(params) and a template's
	HostParamsForEach      // params.forEach(callback), over the live list
	HostAbortSignalTimeout // AbortSignal.timeout(ms)
	HostEnvGet             // process.env[Args[0]]: a string or undefined (S1 plan D8)
	HostEnvHas             // Args[0] in process.env
)

// The keys a HostFetch's init properties have: a header's is its name
// after FetchKeyHeader. A dropped property is one Node's fetch ignores,
// such as Next.js's `next`, evaluated for its effects alone.
const (
	FetchKeyMethod   = "method"
	FetchKeyHeader   = "header:"
	FetchKeyHeaders  = "headers" // a Headers value
	FetchKeyBody     = "body"
	FetchKeyRedirect = "redirect"
	FetchKeyCache    = "cache"
	FetchKeyDropped  = "dropped"
	FetchKeyFormBody = "formBody" // a URLSearchParams as the body, its form encoding the value
	FetchKeySignal   = "signal"
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
	case HostSetTimeout:
		return "setTimeout"
	case HostClearTimeout:
		return "clearTimeout"
	case HostSpawn:
		return "spawn"
	case HostFetch:
		return "fetch"
	case HostResponseText:
		return "Response.text"
	case HostResponseJSON:
		return "Response.json"
	case HostNewHeaders:
		return "new Headers"
	case HostHeadersGet:
		return "Headers.get"
	case HostHeadersHas:
		return "Headers.has"
	case HostHeadersSet:
		return "Headers.set"
	case HostHeadersAppend:
		return "Headers.append"
	case HostHeadersDelete:
		return "Headers.delete"
	case HostHeadersSetCookies:
		return "Headers.getSetCookie"
	case HostPromiseAll, HostPromiseAllValues, HostPromiseAllOf:
		return "Promise.all"
	case HostPromiseRace:
		return "Promise.race"
	case HostPromiseResolve:
		return "Promise.resolve"
	case HostPromiseReject:
		return "Promise.reject"
	case HostPromiseValue:
		return "a promise's value"
	default:
		if s, ok := urlOpNames[o]; ok {
			return s
		}
		return "invalid host operation"
	}
}

// IsTimer reports whether o is one of the timer operations, which are globals
// rather than part of node:http.
func (o HostOp) IsTimer() bool { return o == HostSetTimeout || o == HostClearTimeout }

// HostProperty is a property of a host value.
type HostProperty int

const (
	HostPropInvalid     HostProperty = iota // the zero value, never produced by lower/
	HostURL                                 // req.url
	HostMethod                              // req.method
	HostStatus                              // res.status, of a fetch's Response
	HostOK                                  // res.ok
	HostStatusText                          // res.statusText
	HostResponseURL                         // res.url
	HostRedirected                          // res.redirected
	HostResponseHeaders                     // res.headers
	HostBodyUsed                            // res.bodyUsed
	HostResponseType                        // res.type
	// A URL's getters, by their names (url.go), and a URLSearchParams' size.
	HostURLHref
	HostURLOrigin
	HostURLProtocol
	HostURLUsername
	HostURLPassword
	HostURLHost
	HostURLHostname
	HostURLPort
	HostURLPathname
	HostURLSearch
	HostURLHash
	HostURLSearchParams
	HostParamsSize
)

func (p HostProperty) String() string {
	switch p {
	case HostURL:
		return "IncomingMessage.url"
	case HostMethod:
		return "IncomingMessage.method"
	case HostStatus:
		return "Response.status"
	case HostOK:
		return "Response.ok"
	case HostStatusText:
		return "Response.statusText"
	case HostResponseURL:
		return "Response.url"
	case HostRedirected:
		return "Response.redirected"
	case HostResponseHeaders:
		return "Response.headers"
	case HostBodyUsed:
		return "Response.bodyUsed"
	case HostResponseType:
		return "Response.type"
	default:
		if s, ok := urlPropNames[p]; ok {
			return s
		}
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
	// Keys names each of Args, for the operations taking properties of an
	// object literal: HostFetch's init and HostNewHeaders' pairs.
	Keys []string
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
