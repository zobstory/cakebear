package runtime

import (
	"math"
	"strings"
	"testing"
)

// TestJoin: each element as ToString gives it, which for -0 is "0", nested
// arrays joined with "," whatever the separator, and an array already being
// joined contributing "". Every want is Node 24.18's but the extension
// types', which are decimal integers, as their loggers print them.
//
//	[1, -0, NaN, 2.5, 1e21, 1e-7].join()   // 1,0,NaN,2.5,1e+21,1e-7
//	["a", "b", "c"].join(" - "), ["a", "b"].join("")
//	[true, false].join("|")
//	[[1, 2], [3, [4, 5]], []].join(";")   // 1,2;3,4,5;
//	[{ a: 1 }, { b: 2 }].join("+")        // [object Object]+[object Object]
//	c = [1, 2]; c.push(c); c.join("-")    // 1-2-
//	outer = [1]; inner = [outer, 2]; outer.push(inner); outer.join(";")  // 1;,2
//	shared = [1, 2]; [shared, shared].join("|")  // 1,2|1,2
//	[].join(), [7].join("xx")             // "", 7
func TestJoin(t *testing.T) {
	t.Parallel()

	negZero, nan := math.Copysign(0, -1), math.NaN()
	numbers := []float64{1, negZero, nan, 2.5, 1e21, 1e-7}
	anyNumbers := make([]any, len(numbers))
	for i, n := range numbers {
		anyNumbers[i] = n
	}
	cyc := NewArray[any](1.0, 2.0)
	cyc.Push(cyc)
	outer := NewArray[any](1.0)
	inner := NewArray[any](outer, 2.0)
	outer.Push(inner)
	// Not a float64[], which joins without the stack: an array joined twice,
	// one after the other, is no cycle.
	shared := NewArray[any](1.0, 2.0)
	objs := NewArray(newTestObject([]string{"a"}, []any{1.0}), newTestObject([]string{"b"}, []any{2.0}))

	for _, tt := range []struct {
		name string
		got  string
		want string
	}{
		{"numbers", NewArray(numbers...).Join(","), "1,0,NaN,2.5,1e+21,1e-7"},
		{"numbers, not as float64[]", NewArray(anyNumbers...).Join(","), "1,0,NaN,2.5,1e+21,1e-7"},
		{"strings", NewArray("a", "b", "c").Join(" - "), "a - b - c"},
		{"an empty separator", NewArray("a", "b").Join(""), "ab"},
		{"booleans", NewArray(true, false).Join("|"), "true|false"},
		{"nested arrays", NewArray[any](NewArray(1.0, 2.0), NewArray[any](3.0, NewArray(4.0, 5.0)), NewArray[float64]()).Join(";"), "1,2;3,4,5;"},
		{"objects", objs.Join("+"), "[object Object]+[object Object]"},
		{"a cycle", cyc.Join("-"), "1-2-"},
		{"a cycle through another array", outer.Join(";"), "1;,2"},
		{"an array twice", NewArray(shared, shared).Join("|"), "1,2|1,2"},
		{"nothing", NewArray[float64]().Join(","), ""},
		{"one element", NewArray(7.0).Join("xx"), "7"},
		{"the extension types", NewArray[any](int32(-7), int64(-1<<62), uint32(1<<31), uint64(1<<63), float32(0.1), "s").Join(" "),
			"-7 -4611686018427387904 2147483648 9223372036854775808 0.10000000149011612 s"},
	} {
		if tt.got != tt.want {
			t.Errorf("join of %s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	if got := panicOf(func() { NewArray[any](func() {}).Join(",") }); !strings.Contains(got, "no way to convert a func()") {
		t.Errorf("joining a function panicked with %q", got)
	}
}
