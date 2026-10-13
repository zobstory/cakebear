package backend

import (
	"fmt"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// Emission for M2: domains, async functions, await, promises and timers. The
// runtime they call is backend/runtime's domain.go and promise.go.
//
// Every compiled function and function value takes the domain it runs in as
// its first parameter, and every call passes its own (M2 plan, D8). Go has no
// goroutine-local storage, and the domain is the caller's, not the one the
// function was declared in: a top-level function runs in main when main calls
// it and in a request when a handler does. An async function then looks like:
//
//	func f(ts_domain *cakebearrt.Domain, n float64) *cakebearrt.Promise[float64] {
//		return cakebearrt.Async(ts_domain, func(ts_co *cakebearrt.Co) float64 {
//			return cakebearrt.Await(ts_co, g(ts_domain, n)) + 1
//		})
//	}
//
// ts_domain and ts_co cannot collide with a user's identifier: mangle only
// produces a ts_ name from a Go keyword or an identifier that already starts
// with ts_, and neither "domain" nor "co" is a keyword.

const (
	domainVar = "ts_domain"
	coVar     = "ts_co"
)

// domainParam is the parameter every compiled function opens with.
var domainParam = domainVar + " *" + rtPkg + ".Domain"

// dom refers to the current domain, noting that the code needs one: main
// declares its own only when its body refers to it, as Go rejects an unused
// local.
func (e *emitter) dom() string {
	e.usesDomain = true
	return domainVar
}

// goPromiseType spells Promise<T>. Promise<void> holds struct{}, Go's empty
// value.
func goPromiseType(t ir.Type) string {
	return "*" + rtPkg + ".Promise[" + goElem(t) + "]"
}

// goElem is the Go type a promise of t holds, or an async body returns.
func goElem(t ir.Type) string {
	if t.Kind != ir.KindPromise || t.Elem == nil || *t.Elem == ir.Void {
		return "struct{}"
	}
	return goType(*t.Elem)
}

// asyncBody writes an async function's body as the call to cakebearrt.Async
// that runs it on its own goroutine. A function whose result is a promise
// returns that call's promise. A literal in a void slot, such as an async
// request handler, makes the call and drops the promise, as JavaScript does.
//
// An async call alone needs no Run: with no await or timer in it, it finishes
// before Async returns, and an await of its promise sets needsRun itself.
func (e *emitter) asyncBody(result ir.Type, body []ir.Stmt, depth int) {
	e.usesRuntime = true
	pad := strings.Repeat("\t", depth)
	ret := ""
	if result.Kind == ir.KindPromise {
		ret = "return "
	}
	fmt.Fprintf(&e.buf, "%s%s%s.Async(%s, func(%s *%s.Co) %s {\n", pad, ret, rtPkg, e.dom(), coVar, rtPkg, goElem(result))

	// The body's returns carry the promise's element. For Promise<void>, and
	// for a dropped promise, that is struct{}, which the source never spells.
	saved := e.asyncVoid
	e.asyncVoid = goElem(result) == "struct{}"
	e.stmts(body, depth+1)
	if e.asyncVoid {
		fmt.Fprintf(&e.buf, "%s\treturn struct{}{}\n", pad)
	}
	e.asyncVoid = saved

	fmt.Fprintf(&e.buf, "%s})\n", pad)
	e.depth = depth
}

// await resumes in a microtask, which only Run drains: a program that awaits
// needs Run even with no timer or server.
func (e *emitter) await(x *ir.Await) string {
	e.usesRuntime, e.needsRun = true, true
	return fmt.Sprintf("%s.Await(%s, %s)", rtPkg, coVar, e.expr(x.Value))
}

// newPromise needs no Run of its own: its executor runs at once, and what
// settles the promise later (a timer) or waits on it (an await) sets needsRun.
func (e *emitter) newPromise(x *ir.NewPromise) string {
	e.usesRuntime = true
	ctor := "NewPromise"
	if goElem(x.Typ) == "struct{}" {
		ctor = "NewPromiseVoid" // resolve takes no value (ir.ResolveType)
	}
	return fmt.Sprintf("%s.%s(%s, %s)", rtPkg, ctor, e.dom(), e.expr(x.Executor))
}

// timerCall emits setTimeout and clearTimeout, which run in the current
// domain: a timer a request sets fires in that request.
func (e *emitter) timerCall(x *ir.HostCall) string {
	e.usesRuntime, e.needsRun = true, true
	args := []string{e.dom()}
	for _, a := range x.Args {
		args = append(args, e.expr(a))
	}
	name := "SetTimeout"
	if x.Op == ir.HostClearTimeout {
		name = "ClearTimeout"
	}
	return fmt.Sprintf("%s.%s(%s)", rtPkg, name, strings.Join(args, ", "))
}
