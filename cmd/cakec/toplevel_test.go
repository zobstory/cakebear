package main

import "testing"

// Top-level variables are visible inside top-level functions, which is
// ordinary TypeScript and used to fail in go build with "undefined". The
// expected output is what Node 24 prints for the same file.
//
// "first" before "second" is the other half: the variables are declared at
// package level but assigned in main, in source order. A Go package-level
// initialiser would have run log("second") before main printed "first".
func TestTopLevelVariablesAreVisibleInFunctions(t *testing.T) {
	t.Parallel()

	got, buildOut, code := compileAndRun(t, `function log(s: string): number {
  console.log(s);
  return 1;
}
const greeting: string = "hi";
let count: number = 0;
function bump(): void {
  count = count + 1;
}
function greet(): void {
  console.log(greeting);
}
console.log("first");
const one: number = log("second");
console.log(count);
bump();
bump();
console.log(count);
greet();
console.log(one);
`)
	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	if want := "first\nsecond\n0\n2\nhi\n1\n"; got != want {
		t.Errorf("output = %q, want %q (what Node prints)", got, want)
	}
}
