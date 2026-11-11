package runtime

import (
	"strings"
	"testing"
)

// TestUnknownObjectsReadAsNode: `in`, reads, Array.isArray, length and
// indexing on what JSON.parse made, each as Node 24.18 gave it (measured).
func TestUnknownObjectsReadAsNode(t *testing.T) {
	t.Parallel()
	v := JSONParse(`{"a":1,"b":null,"c":[1,"x",{"d":true}],"2":"two","":"empty"}`)
	c := UnknownGet(v, "c", nil)
	e := NewError(ErrorType, "m", nil)
	show := func(f func() any) string {
		var out any
		if th := Try(func() { out = f() }); th != nil {
			return UnknownToString(th.V)
		}
		if out == nil {
			return "undefined"
		}
		return TypeOf(out).String() + " " + JSONStringify(out, JSONGap{}, nil)
	}
	got := []string{}
	for _, f := range []func() any{
		func() any { return UnknownIn("a", v, nil) }, func() any { return UnknownIn("z", v, nil) },
		func() any { return UnknownIn("toString", v, nil) }, func() any { return UnknownIn("2", v, nil) },
		func() any { return UnknownIn("", v, nil) }, func() any { return UnknownIn("b", v, nil) },
		func() any { return UnknownGet(v, "a", nil) }, func() any { return UnknownGet(v, "z", nil) },
		func() any { return UnknownGet(v, "b", nil) }, func() any { return UnknownGet(v, "", nil) },
		func() any { return UnknownGet(v, "2", nil) },
		func() any { return UnknownIsArray(c) }, func() any { return UnknownLength(c, nil) },
		func() any { return UnknownIndex(c, 1, nil) }, func() any { return UnknownIndex(c, 5, nil) },
		func() any { return UnknownIndex(c, -1, nil) }, func() any { return UnknownIndex(c, 1.5, nil) },
		func() any { return UnknownIn("length", c, nil) }, func() any { return UnknownIn("0", c, nil) },
		func() any { return UnknownIn("3", c, nil) }, func() any { return UnknownIn("map", c, nil) },
		func() any { return UnknownIn("x", c, nil) },
		func() any { return UnknownIn("k", 5.0, nil) }, func() any { return UnknownIn("k", "str", nil) },
		func() any { return UnknownIn("k", Null, nil) },
		func() any { return UnknownGet(Null, "k", nil) }, func() any { return UnknownGet(nil, "k", nil) },
		func() any { return UnknownIn("message", e, nil) }, func() any { return UnknownIn("cause", e, nil) },
		func() any { return UnknownIn("stack", e, nil) }, func() any { return UnknownIn("code", e, nil) },
		func() any { return UnknownGet(e, "message", nil) }, func() any { return UnknownGet(e, "name", nil) },
	} {
		got = append(got, show(f))
	}
	want := `boolean true
boolean false
boolean true
boolean true
boolean true
boolean true
number 1
undefined
object null
string "empty"
string "two"
boolean true
number 3
string "x"
undefined
undefined
undefined
boolean true
boolean true
boolean false
boolean true
boolean false
TypeError: Cannot use 'in' operator to search for 'k' in 5
TypeError: Cannot use 'in' operator to search for 'k' in str
TypeError: Cannot use 'in' operator to search for 'k' in null
TypeError: Cannot read properties of null (reading 'k')
TypeError: Cannot read properties of undefined (reading 'k')
boolean true
boolean false
boolean true
boolean false
string "m"
string "TypeError"`
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got\n%s\nwant, as Node gave them\n%s", g, want)
	}
}
