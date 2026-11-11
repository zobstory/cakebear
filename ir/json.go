package ir

import (
	"fmt"
	"strings"
)

// JSONStringify is `JSON.stringify(value)`, or with indentation,
// `JSON.stringify(value, null, space)` (M4 plan D15). Space is nil, or a
// number or a string, which the runtime turns into the gap JavaScript would.
// A replacer is refused in lowering, and so is a value holding a function, a
// promise or a host value: Node leaves out the first and serialises the others
// as objects of its own, none of which the subset can build.
type JSONStringify struct {
	Value Expr
	Space Expr
	Span  Span
}

func (*JSONStringify) isExpr()          {}
func (e *JSONStringify) ExprType() Type { return String }
func (e *JSONStringify) ExprSpan() Span { return e.Span }

// checkJSONStringify holds JSON.stringify to a value it can serialise and a
// space of a type JavaScript reads one from.
func checkJSONStringify(x *JSONStringify) error {
	if what := JSONUnserializable(x.Value.ExprType()); what != "" {
		return fmt.Errorf("JSON.stringify of a %s, which holds %s", x.Value.ExprType(), what)
	}
	if x.Space != nil {
		if t := x.Space.ExprType(); !t.IsNumeric() && t != String && t != Base64 {
			return fmt.Errorf("JSON.stringify's space is a %s; it must be a number or a string", t)
		}
	}
	return nil
}

// Unserializable names what in a value of type t isn't data, or returns
// "": a function, a promise, a host value, or a type the IR can't
// represent, directly or inside an object or array. Only data may be
// boxed into an unknown, thrown or rejected with.
func Unserializable(t Type, seen map[*Shape]bool) string { return unserializable(t, seen, false) }

// JSONUnserializable is Unserializable for JSON.stringify, which also
// serialises a URL, as its quoted href, and a URLSearchParams, as `{}`
// (S1 plan D6, R24, R25): host values, so no data.
func JSONUnserializable(t Type) string { return unserializable(t, nil, true) }

func unserializable(t Type, seen map[*Shape]bool, urls bool) string {
	switch t.Kind {
	case KindInvalid:
		return "a type the IR can't represent"
	case KindFunc:
		return "a function"
	case KindHost:
		if urls && (t.Host == HostURLValue || t.Host == HostSearchParams) {
			return ""
		}
		return article(t.String())
	case KindPromise:
		return "a promise"
	case KindUnknown, KindError:
		// An unknown holds only data, since Box lets in nothing else; an
		// Error is written as its own enumerable properties, `{}` for one
		// of the built-in kinds (M6 plan D4).
		return ""
	case KindArray, KindMaybe:
		// A Maybe's null is written as null, and its undefined as nothing at
		// all (a property) or null (an element), as JSON.stringify does.
		if t.Elem != nil {
			return unserializable(*t.Elem, seen, urls)
		}
	case KindObject:
		if t.Shape == nil || seen[t.Shape] {
			return ""
		}
		if seen == nil {
			seen = map[*Shape]bool{}
		}
		seen[t.Shape] = true
		for _, f := range t.Shape.Fields {
			if what := unserializable(f.Type, seen, urls); what != "" {
				return what
			}
		}
	}
	return ""
}

// article puts "a" or "an" before name, by its first letter.
func article(name string) string {
	if name != "" && strings.ContainsRune("AEIOUaeiou", rune(name[0])) && !strings.HasPrefix(name, "URL") { // "a URL"
		return "an " + name
	}
	return "a " + name
}
