package main

import "testing"

// closuresProgram exercises closures end to end. Its expected output was taken
// from Node 24 running the same file (--experimental-strip-types), not written
// by hand, so a semantic difference from JavaScript shows up as a failure.
const closuresProgram = `function apply(f: (x: number) => number, v: number): number {
  return f(v);
}
function double(x: number): number {
  return x * 2;
}
function makeCounter(): () => number {
  let count: number = 0;
  return () => {
    count = count + 1;
    return count;
  };
}
function each(f: (x: number) => void): void {
  f(1);
  f(2);
}

const add = (a: number, b: number): number => a + b;
console.log(add(2, 3));
console.log(apply((x) => x + 10, 1));
console.log(apply(double, 4));

const next = makeCounter();
console.log(next());
console.log(next());
const other = makeCounter();
console.log(other());

const fact = (n: number): number => {
  if (n <= 1) {
    return 1;
  }
  return n * fact(n - 1);
};
console.log(fact(5));

let label: string = "before";
const show = (): void => {
  console.log(label);
};
label = "after";
show();

const greet = function (who: string): string {
  return "hi " + who;
};
console.log(greet("cake"));

each((x) => x * 2);
each(() => console.log("tick"));
each((x) => {
  return x * 3;
});
`

// What each part proves:
//   - 5, 11, 8: a closure value, one typed from context, a function passed by name
//   - 1, 2, 1: each counter closes over its own count, by reference
//   - 120: a closure calls itself through the variable it is assigned to
//   - after: a captured let is read when called, not when captured
//   - tick, tick: a callback ignoring its parameter; x * 2 and x * 3 print nothing
const closuresOutput = "5\n11\n8\n1\n2\n1\n120\nafter\nhi cake\ntick\ntick\n"

func TestCompileClosuresMatchesNode(t *testing.T) {
	t.Parallel()

	got, buildOut, code := compileAndRun(t, closuresProgram)
	if code != exitOK {
		t.Fatalf("build failed (%d):\n%s", code, buildOut)
	}
	if got != closuresOutput {
		t.Errorf("output = %q, want %q (what Node prints)", got, closuresOutput)
	}
}
