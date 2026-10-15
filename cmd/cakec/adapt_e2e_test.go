package main

import "testing"

// TestFunctionWithFewerParametersPasses: a named function taking fewer
// parameters than the function type it fills, which TypeScript allows,
// failed go build until M4 step 7, since Go func types must match exactly.
// Lowering now wraps it in a literal of the slot's signature (lower/adapt.go).
// Node 24.18 prints 10.
func TestFunctionWithFewerParametersPasses(t *testing.T) {
	t.Parallel()

	const src = `function apply(g: (a: number, b: number) => number): number {
  return g(1, 2);
}
function one(a: number): number {
  return a * 10;
}
console.log(apply(one));
`
	matchesNode(t, buildProgram(t, "fewer", src), "10\n", 5)
}
