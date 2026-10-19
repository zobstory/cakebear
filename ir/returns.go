package ir

import "fmt"

// returnType is the type a function's returns carry: its result, or for an
// async function the T of its PromiseOf(T), which a `return v` resolves it
// with.
func returnType(result Type, async bool) Type {
	if async && result.Kind == KindPromise && result.Elem != nil {
		return *result.Elem
	}
	return result
}

// checkReturns holds each return in body, a function's own (a function
// literal inside it is checked on its own), to want: a value of exactly that
// type, or none for void. Go accepts nothing looser, so a mismatch that got
// past here would fail inside go build, pointing at generated code. A string
// may return a base64, which is one.
func checkReturns(body []Stmt, want Type) error {
	for _, s := range body {
		var err error
		switch s := s.(type) {
		case *Return:
			err = checkReturn(s, want)
		case *If:
			if err = checkReturns(s.Then, want); err == nil {
				err = checkReturns(s.Else, want)
			}
		case *While:
			err = checkReturns(s.Body, want)
		case *For:
			err = checkReturns(s.Body, want)
		case *ForOf:
			err = checkReturns(s.Body, want)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func checkReturn(r *Return, want Type) error {
	switch {
	case want == Void && r.Value != nil:
		return fmt.Errorf("%s: a return of a %s from a function that returns nothing", r.Span, r.Value.ExprType())
	case want != Void && r.Value == nil:
		return fmt.Errorf("%s: a return of nothing from a function that returns a %s", r.Span, want)
	case r.Value != nil:
		if got := r.Value.ExprType(); !got.Equal(want) && !(want == String && got == Base64) {
			return fmt.Errorf("%s: a return of a %s from a function that returns a %s", r.Span, got, want)
		}
	}
	return nil
}

// checkDecl holds a declaration's value to its type, as checkReturn holds a
// return's value to its function's result, and for the same reason.
func checkDecl(d *VarDecl) error {
	if d.Init == nil {
		return nil
	}
	if got := d.Init.ExprType(); !got.Equal(d.Type) && !(d.Type == String && got == Base64) {
		return fmt.Errorf("%s, a %s, is given a %s", d.Name, d.Type, got) // Validate adds d's span
	}
	return nil
}
