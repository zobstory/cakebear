package runtime

import (
	"fmt"
	"strconv"
)

// Join is xs.join(sep), each element converted as ToString converts it: a
// number by Number::toString, so -0 is "0" where console.log prints "-0", a
// boolean as "true" or "false", and an object as "[object Object]". An
// element that is itself an array is its own join with ",", which is what
// its toString does. The backend passes "," when the call gives no
// separator.
//
// The spec has no answer for an array that contains itself, and would
// recurse until the stack ran out. V8 keeps a stack of the arrays being
// joined, and one already on it contributes "", so cakebear does the same:
// `c = [1, 2]; c.push(c); c.join("-")` is "1-2-".
//
// The pieces are joined as StrConcat joins them: a high half at the end of
// one, an element or the separator, and a low half at the start of the next
// make their character, so `["\uD83D", "\uDE00"].join("")` is "😀".
func (a *Array[T]) Join(sep string) string {
	return a.join(sep, nil)
}

// joiner is an array of any element type, as a join reaching one inside
// another sees it.
type joiner interface {
	join(sep string, stack []any) string
}

// join is Join with the stack of the arrays already being joined, outermost
// first.
func (a *Array[T]) join(sep string, stack []any) string {
	for _, s := range stack {
		if s == any(a) {
			return ""
		}
	}
	// The common element types skip the conversion to any, which would
	// allocate for each number.
	switch items := any(a.items).(type) {
	case []string:
		return joinStrings(items, sep)
	case []float64:
		return joinEach(items, sep, NumberToString)
	}
	stack = append(stack, a)
	return joinEach(a.items, sep, func(v T) string { return joinString(v, stack) })
}

func joinEach[T any](items []T, sep string, str func(T) string) string {
	var b strBuilder
	for i, v := range items {
		if i > 0 {
			b.str(sep)
		}
		b.str(str(v))
	}
	return b.string()
}

// joinString is ToString of an element, for join. The backend refuses to
// join the elements this has no case for, a function, a promise or a host
// value, and an object whose shape has a toString of its own, which
// ToString would call.
func joinString(v any, stack []any) string {
	switch x := v.(type) {
	case float64:
		return NumberToString(x)
	case float32:
		return NumberToString(float64(x))
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case bool:
		return BoolToString(x)
	case string:
		return x
	case nil, jsNull:
		return "" // an unknown[]'s undefined or null, as join writes them
	case maybe:
		// Lowering refuses a join over maybes, but String() of an unknown
		// can reach an array of them all the same.
		if state, value := x.maybeState(); state == stateValue {
			return joinString(value, stack)
		}
		return ""
	case *Error:
		return ErrorToString(x)
	case Object:
		return "[object Object]"
	case joiner:
		return x.join(",", stack)
	}
	panic(fmt.Sprintf("cakebear: join has no way to convert a %T to a string", v))
}
