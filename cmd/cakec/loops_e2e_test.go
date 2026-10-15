package main

import "testing"

// loopsProgram covers M4 step 1: type declarations as no-ops, ++ and --,
// compound assignment on numbers and strings, for (;;) with continue and
// break, two loop variables, nested loops, while (true), and timers that
// each capture their own iteration's i, which prints 0 1 2 only because Go
// 1.22's loop variables are JavaScript's per-iteration lets. It ends with two
// functions whose ends TypeScript knows are unreachable and Go doesn't, a
// `while (true)` and an `if (true)`; then two loop variables initialised
// from a variable and a call that changes it, which must be read in that
// order, and a loop variable nothing reads. Node 24.18 ran this same file
// and printed nodeLoops, the same in 5 of 5 runs.
const loopsProgram = `type Count = number;
interface Unused {
  x: number;
}
export type Label = string;
type Op = (a: number) => number;

const twice: Op = (a: number): number => a * 2;
console.log(twice(21));

let total: Count = 0;
for (let i: number = 0; i < 5; i++) {
  if (i === 1) {
    continue;
  }
  if (i === 4) {
    break;
  }
  total += i;
}
console.log(total);

let n: number = 10;
n -= 3;
n *= 2;
n /= 7;
console.log(n);

let s: string = "a";
s += "b";
s += "c";
console.log(s);

let k: number = 0;
++k;
k++;
--k;
console.log(k);

let loops: number = 0;
for (;;) {
  loops++;
  if (loops >= 3) {
    break;
  }
}
console.log(loops);

let iterations: number = 0;
for (let a: number = 0, b: number = 10; a < b; a++) {
  b--;
  iterations++;
}
console.log(iterations);

let pairs: number = 0;
for (let x: number = 0; x < 3; x++) {
  for (let y: number = 0; y < 3; y++) {
    if (y === x) {
      continue;
    }
    if (y > x) {
      break;
    }
    pairs++;
  }
}
console.log(pairs);

let w: number = 0;
let kept: number = 0;
let skip: boolean = false;
while (true) {
  w++;
  if (w > 6) {
    break;
  }
  skip = !skip;
  if (skip) {
    continue;
  }
  kept += w;
}
console.log(kept);

for (let i: number = 0; i < 3; i++) {
  setTimeout(() => {
    console.log(i);
  }, 1);
}
function first(n: number): number {
  let i: number = 0;
  while (true) {
    if (i * i > n) {
      return i;
    }
    i++;
  }
}
console.log(first(50));
function one(): number {
  if (true) {
    return 1;
  }
}
console.log(one());
let seed: number = 1;
function bump(): number {
  seed = seed + 10;
  return seed;
}
for (let a: number = seed, b: number = bump(); a < 2; a++) {
  console.log(a + b);
}
for (let k: number = 0, unused: number = 5; k < 1; k++) {
  console.log(k + 100);
}
`

const nodeLoops = "42\n5\n2\nabc\n1\n3\n5\n3\n12\n8\n1\n12\n100\n0\n1\n2\n"

func TestLoopsAndUpdatesMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "loops", loopsProgram), nodeLoops, 20)
}

// loopDeclarationsProgram: a for loop declaring several variables evaluates
// each initialiser after the one before it, so `j = i + 1` reads the loop's
// own i, not the outer one (a Go short declaration evaluates every value
// first, which printed 11 here until M4 step 8). Each iteration still has its
// own copies, which the closures show; one made in the initialiser itself
// sees the first i, as under Node. The same holds when the first clause is a
// destructuring pattern, which declares a held value and then the names.
// Node 24.18 ran this same file and printed nodeLoopDeclarations, the same in
// 5 of 5 runs.
const loopDeclarationsProgram = `export {};
let i: number = 10;
for (let i: number = 0, j: number = i + 1; i < 2; i++) {
  console.log(i, j);
}
console.log(i);
const fs: (() => number)[] = [];
for (let i: number = 0, k: number = 10; i < 3; i++) {
  fs.push((): number => i + k);
}
for (const f of fs) {
  console.log(f());
}
for (let i: number = 0, g: () => number = (): number => i; i < 3; i++) {
  console.log(i, g());
}
interface R { lo: number; hi: number }
const r: R = { lo: 1, hi: 4 };
for (let { lo, hi } = r; lo < hi; lo++) {
  console.log(lo, hi);
}
const later: (() => number)[] = [];
for (let { lo, hi } = r; lo <= 2; lo++) {
  later.push((): number => lo * hi);
}
for (const f of later) {
  console.log(f());
}
function sum({ lo, hi }: R): number {
  let t: number = 0;
  for (const { lo: a } = r; t < hi; t++) {
    console.log(a + t);
  }
  return t;
}
console.log(sum(r));
`

const nodeLoopDeclarations = "0 1\n1 1\n10\n10\n11\n12\n0 0\n1 0\n2 0\n1 4\n2 4\n3 4\n4\n8\n1\n2\n3\n4\n4\n"

func TestLoopDeclarationsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "loop_declarations", loopDeclarationsProgram), nodeLoopDeclarations, 20)
}
