package backend

import (
	"fmt"
	"strings"

	"github.com/zobstory/cakebear/ir"
)

// throw and try (M6 plan D2; m6-go spike §3). A throw panics a *Thrown. A
// try block is a function literal run by the runtime's Try, or TryK when the
// block can leave by return, break or continue: it then returns a code
// saying how, and the code is acted on once the finally has run, in the
// try's own place. A catch before a finally is a literal too, so a throw
// from it still runs the finally. The finally, and a catch with none after
// it, are emitted in place: their own return, break and continue are Go's,
// which is exactly JavaScript's rule that a finally's way out replaces the
// one pending.

// How a try's literal was left.
const (
	codeFell     = 0 // its end
	codeReturn   = 1 // a return: the value waits in errorState.retVar
	codeBreak    = 2 // a break of a loop outside the literal
	codeContinue = 3 // a continue of one
)

// errorState is what emitting throw and try needs, held in the emitter.
type errorState struct {
	frames []tryFrame // the try literals open in the function, innermost last
	loops  int        // the loops open in the function
	ret    ir.Type    // what the function's returns carry
	retVar string     // where a return's value waits while finallys run, once a try declares it
	tries  int        // the trys emitted so far, for their names

	sites     []ir.Span      // the site table (emit_site.go)
	siteIndex map[string]int // by place
	lines     map[string][]string
}

// tryFrame is a literal open around the statements being emitted.
type tryFrame struct {
	loops int // the loops open where it begins
}

// enterFunction starts a function's own try state, returning what to restore.
func (e *emitter) enterFunction(result ir.Type) func() {
	frames, loops, ret, retVar := e.frames, e.loops, e.ret, e.retVar
	e.frames, e.loops, e.ret, e.retVar = nil, 0, result, ""
	return func() { e.frames, e.loops, e.ret, e.retVar = frames, loops, ret, retVar }
}

// isLoop reports whether s is a loop, which a break or continue leaves.
func isLoop(s ir.Stmt) bool {
	switch s.(type) {
	case *ir.While, *ir.For, *ir.ForOf:
		return true
	}
	return false
}

// errorStmt emits a throw or a try.
func (e *emitter) errorStmt(s ir.Stmt, pad string, depth int) {
	e.usesRuntime = true
	switch s := s.(type) {
	case *ir.Throw:
		fmt.Fprintf(&e.buf, "%spanic(&%s.Thrown{V: %s, Site: %s})\n", pad, rtPkg, e.boxAs(s.Value.ExprType(), e.full(s.Value)), e.siteArg(s.Span))
	case *ir.Try:
		e.tryStmt(s, pad, depth)
	}
}

func (e *emitter) tryStmt(s *ir.Try, pad string, depth int) {
	e.tries++
	k, t := fmt.Sprintf("ts_k%d", e.tries), fmt.Sprintf("ts_t%d", e.tries)
	codes := exits(s.Body, false)
	if s.Catch != nil && s.Finally != nil {
		codes |= exits(s.Catch.Body, false)
	}
	// The first try a return can leave declares where the value waits, and
	// any try inside it uses that: one in its block or catch, and one in a
	// finally, emitted in place after the literal, whose return replaces
	// the pending one.
	owns := codes&(1<<codeReturn) != 0 && e.ret != ir.Void && e.retVar == ""
	if owns {
		e.retVar = fmt.Sprintf("ts_r%d", e.tries)
		fmt.Fprintf(&e.buf, "%svar %s %s\n", pad, e.retVar, e.typ(e.ret, s.Span))
	}

	run := func(assign, open string, body []ir.Stmt, bind *ir.Catch, depth int) {
		lead := strings.Repeat("\t", depth-1)
		if codes == 0 {
			fmt.Fprintf(&e.buf, "%s%s %s %s.Try(func() {\n", lead, t, assign, rtPkg)
		} else {
			fmt.Fprintf(&e.buf, "%s%s, %s %s %s.TryK(func() int {\n", lead, k, t, assign, rtPkg)
		}
		e.bindCatch(bind, open, depth)
		e.inLiteral(body, depth, codes != 0)
		fmt.Fprintf(&e.buf, "%s})\n", lead)
	}
	run(":=", "", s.Body, nil, depth+1)

	switch {
	case s.Catch != nil && s.Finally == nil:
		fmt.Fprintf(&e.buf, "%sif %s != nil {\n", pad, t)
		e.bindCatch(s.Catch, t, depth+1)
		e.stmts(s.Catch.Body, depth+1)
		fmt.Fprintf(&e.buf, "%s}\n", pad)
	case s.Catch != nil:
		fmt.Fprintf(&e.buf, "%sif %s != nil {\n", pad, t)
		run("=", t, s.Catch.Body, s.Catch, depth+2)
		fmt.Fprintf(&e.buf, "%s}\n", pad)
	}
	if s.Finally != nil {
		e.stmts(s.Finally, depth)
		fmt.Fprintf(&e.buf, "%s%s.Rethrow(%s)\n", pad, rtPkg, t)
	}
	for _, code := range []int{codeReturn, codeBreak, codeContinue} {
		if codes&(1<<code) != 0 {
			fmt.Fprintf(&e.buf, "%sif %s == %d {\n", pad, k, code)
			e.leave(code, pad+"\t")
			fmt.Fprintf(&e.buf, "%s}\n", pad)
		}
	}
	if owns {
		e.retVar = ""
	}
}

// bindCatch declares a catch's binding from the throw in t, an unknown,
// used or not (Go refuses an unused variable; TypeScript doesn't).
func (e *emitter) bindCatch(c *ir.Catch, t string, depth int) {
	if c == nil || c.Name == "" || t == "" {
		return
	}
	pad, name := strings.Repeat("\t", depth), mangle(c.Name)
	fmt.Fprintf(&e.buf, "%s%s := %s.V\n%s_ = %s\n", pad, name, t, pad, name)
}

// inLiteral emits body as a try literal's, which, when coded, ends by
// returning the code for its end.
func (e *emitter) inLiteral(body []ir.Stmt, depth int, coded bool) {
	e.frames = append(e.frames, tryFrame{loops: e.loops})
	e.stmts(body, depth)
	e.frames = e.frames[:len(e.frames)-1]
	if coded {
		fmt.Fprintf(&e.buf, "%sreturn %d\n", strings.Repeat("\t", depth), codeFell)
	}
}

// leaves emits s when it is a way out of the try literal being emitted: a
// return, or a break or continue of a loop outside it, which the literal
// returns the code for, a return's value stored first, as JavaScript takes
// it before the finally runs. It reports whether s was one.
func (e *emitter) leaves(s ir.Stmt, pad string) bool {
	if len(e.frames) == 0 {
		return false
	}
	outside := e.loops == e.frames[len(e.frames)-1].loops
	switch s := s.(type) {
	case *ir.Return:
		switch {
		case s.Value != nil && e.retVar == "":
			// exits found every return under the try that declared
			// retVar: this is a bug, refused rather than dropped.
			fmt.Fprintf(&e.buf, "%s%s\n", pad, e.cannot(s.Span, "the backend lost a return's value inside a try"))
		case s.Value != nil:
			fmt.Fprintf(&e.buf, "%s%s = %s\n", pad, e.retVar, e.full(s.Value))
		}
		fmt.Fprintf(&e.buf, "%sreturn %d\n", pad, codeReturn)
	case *ir.Break:
		if !outside {
			return false
		}
		fmt.Fprintf(&e.buf, "%sreturn %d\n", pad, codeBreak)
	case *ir.Continue:
		if !outside {
			return false
		}
		fmt.Fprintf(&e.buf, "%sreturn %d\n", pad, codeContinue)
	default:
		return false
	}
	return true
}

// leave acts on a code a try's literal returned, in the try's own place:
// passed on, where that place is inside another literal, or done.
func (e *emitter) leave(code int, pad string) {
	switch code {
	case codeReturn:
		switch {
		case len(e.frames) > 0:
			fmt.Fprintf(&e.buf, "%sreturn %d\n", pad, codeReturn)
		case e.ret != ir.Void:
			fmt.Fprintf(&e.buf, "%sreturn %s\n", pad, e.retVar)
		case e.asyncVoid:
			fmt.Fprintf(&e.buf, "%sreturn struct{}{}\n", pad)
		default:
			fmt.Fprintf(&e.buf, "%sreturn\n", pad)
		}
	case codeBreak:
		if !e.leaves(&ir.Break{}, pad) {
			fmt.Fprintf(&e.buf, "%sbreak\n", pad)
		}
	case codeContinue:
		if !e.leaves(&ir.Continue{}, pad) {
			fmt.Fprintf(&e.buf, "%scontinue\n", pad)
		}
	}
}

// exits is the set of codes, as bits, by which stmts can leave a try's
// literal: a return anywhere, and a break or continue not inside a loop of
// their own. A function literal's returns are its own, and it is an
// expression, which this walks none of.
func exits(stmts []ir.Stmt, inLoop bool) int {
	codes := 0
	for _, s := range stmts {
		switch s := s.(type) {
		case *ir.Return:
			codes |= 1 << codeReturn
		case *ir.Break:
			if !inLoop {
				codes |= 1 << codeBreak
			}
		case *ir.Continue:
			if !inLoop {
				codes |= 1 << codeContinue
			}
		case *ir.If:
			codes |= exits(s.Then, inLoop) | exits(s.Else, inLoop)
		case *ir.While:
			codes |= exits(s.Body, true)
		case *ir.For:
			codes |= exits(s.Body, true)
		case *ir.ForOf:
			codes |= exits(s.Body, true)
		case *ir.Try:
			codes |= exits(s.Body, inLoop) | exits(s.Finally, inLoop)
			if s.Catch != nil {
				codes |= exits(s.Catch.Body, inLoop)
			}
		}
	}
	return codes
}
