package main

import "testing"

// unknownsProgram covers what M6 step 3's unknowns found missing once they
// met the rest of the subset, each of which printed or built wrong before:
//   - `x ?? d` and `x ??= d` on an unknown, which were taken for a value
//     that is never null or undefined, so d was never used;
//   - a function returning an unknown that returns nothing, or ends, whose
//     IR was invalid;
//   - an unknown[] read past its end, by index, at, find, pop and shift,
//     which an unknown holds as undefined, as an array of maybes does;
//   - `p?: unknown`, optional as it is, whose IR was invalid.
//
// Node 24.18 ran this same file and printed nodeUnknowns, the same in 3 of 3
// runs.
const unknownsProgram = `// ?? and ??= on an unknown, tested as it is.
function get(n: number): unknown {
  if (n > 0) return n;
}
function none(n: number): unknown {
  if (n > 0) return "some";
  return;
}
function calls(): unknown {
  console.log("calls, once");
  return null;
}
let u: unknown = get(0);
u ??= 5;
console.log("??=", u);
u ??= 6;
console.log("kept", u);
const d: unknown = get(0) ?? "d";
const kept: unknown = get(3) ?? "no";
const once: unknown = calls() ?? 1;
console.log("??", d, kept, once, none(1), none(0));
let later: unknown;
console.log("unset", later);
later = [1];
console.log("set", later);

// An unknown[] holds undefined itself, so a read past its end is undefined.
const ys: unknown[] = ["a", null];
console.log("reads", ys[5], ys[1], ys.at(-1), ys.find(() => false), ys.findLast(() => true));
console.log("pops", ys.pop(), ys.pop(), ys.pop(), ys.shift());
ys[0] ??= "appended";
ys[0] ??= "not";
console.log("elements", ys);

// p?: unknown is optional as it is.
interface R { id: number; data?: unknown }
const a: R = { id: 1 };
const b: R = { id: 2, data: undefined };
const c: R = { id: 3, data: { k: "v" } };
console.log("optional", a, b, c, "data" in a, "data" in b, a.data, c.data);
a.data = [1, 2];
a.data ??= "not";
b.data ??= 7;
delete c.data;
console.log("written", a, b, c, "data" in c, c.data);
console.log(JSON.stringify(a));
console.log(JSON.stringify(b));
console.log(JSON.stringify(c));
`

const nodeUnknowns = `??= 5
kept 5
calls, once
?? d 3 1 some undefined
unset undefined
set [ 1 ]
reads undefined null null undefined null
pops null a undefined undefined
elements [ 'appended' ]
optional { id: 1 } { id: 2, data: undefined } { id: 3, data: { k: 'v' } } false true undefined { k: 'v' }
written { id: 1, data: [ 1, 2 ] } { id: 2, data: 7 } { id: 3 } false undefined
{"id":1,"data":[1,2]}
{"id":2,"data":7}
{"id":3}
`

func TestUnknownsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "unknowns", unknownsProgram), nodeUnknowns, 20)
}
