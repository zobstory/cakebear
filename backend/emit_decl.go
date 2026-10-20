package backend

import (
	"fmt"

	"github.com/zobstory/cakebear/ir"
)

// varDecl emits a declaration. One with no value, Init nil, holds Go's zero
// value: TypeScript makes sure nothing reads it before an assignment, but for
// a closure called before it, which reads that zero (see CLAUDE.md's accepted
// differences).
func (e *emitter) varDecl(s *ir.VarDecl, pad string) {
	if e.hoisted[s] {
		// Declared at package level already, at its zero; see program.
		if s.Init != nil {
			fmt.Fprintf(&e.buf, "%s%s = %s\n", pad, e.top(e.cur, s.Name), e.full(s.Init))
		}
		return
	}
	switch {
	case s.Init == nil:
		fmt.Fprintf(&e.buf, "%svar %s %s\n", pad, mangle(s.Name), e.typ(s.Type, s.Span))
	case s.Type.Kind == ir.KindFunc:
		// Declared before it is assigned, so a function literal can call
		// itself. Go's scope for `var f = …` starts after the initialiser;
		// JavaScript's starts at the declaration.
		fmt.Fprintf(&e.buf, "%svar %s %s\n", pad, mangle(s.Name), e.typ(s.Type, s.Span))
		fmt.Fprintf(&e.buf, "%s%s = %s\n", pad, mangle(s.Name), e.full(s.Init))
	default:
		// An explicit type rather than := because TypeScript's annotation
		// is the authority here, and := would let Go's inference disagree.
		fmt.Fprintf(&e.buf, "%svar %s %s = %s\n", pad, mangle(s.Name), e.typ(s.Type, s.Span), e.full(s.Init))
	}
	// Go rejects unused locals; TypeScript does not. Referencing the
	// variable keeps a legal TypeScript program from failing to build.
	fmt.Fprintf(&e.buf, "%s_ = %s\n", pad, mangle(s.Name))
	if s.Appended {
		e.buf.WriteString(e.accDecl(pad, mangle(s.Name)))
	}
}
