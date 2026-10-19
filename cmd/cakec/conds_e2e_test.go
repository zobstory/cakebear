package main

import "testing"

// condsProgram covers M5 step 4, the conditional expressions:
//   - `?:`, nested, in a template, and with a call in one branch, which
//     runs only when chosen;
//   - `??` of a binding, of calls, held so each runs once, and chained;
//   - `?.` through fields, a string's length and methods, an array's elements,
//     length and map, from bindings and from calls, with null and undefined
//     receivers, and a linked list read past its end;
//   - `!` on a call, a field and a chain;
//   - `??=` on a let and on a field, twice each;
//   - evaluation order: `a + (flag ? bump() : 0)` reads a before bump runs;
//   - `??` and `?:` over awaits in an async function.
//
// Node 24.18 ran this same file and printed nodeConds, the same in 5 of 5
// runs.
const condsProgram = `interface Point { x: number; y: number }
interface Box { p: Point | undefined; label: string | null }
interface Link { v: number; next: Link | undefined }

let calls: number = 0;
function count(n: number): number {
  calls += 1;
  return n;
}
function maybe(i: number): number | undefined {
  calls += 1;
  return i > 0 ? i : undefined;
}
function box(i: number): Box | undefined {
  calls += 1;
  if (i === 0) {
    return undefined;
  }
  return { p: i > 1 ? { x: i, y: -i } : undefined, label: i > 2 ? "big" : null };
}

const flag: boolean = calls === 0;
console.log("ternary", flag ? "yes" : "no", !flag ? count(1) : count(2), calls);
const t1: number | undefined = flag ? undefined : 5;
const t2: string = flag ? (calls > 0 ? "a" : "b") : "c";
console.log("nested", t1, t2, ` + "`" + `${flag ? 1 : 2}` + "`" + `);

calls = 0;
const d1: number = maybe(3) ?? count(100);
const d2: number = maybe(0) ?? count(100);
console.log("coalesce", d1, d2, calls);
const m: number | null = null;
console.log("null", m ?? -1, maybe(0) ?? maybe(4) ?? 0);

calls = 0;
const b0: Box | undefined = box(0);
const b1: Box | undefined = box(1);
const b3: Box | undefined = box(3);
console.log("chain", b0?.label, b1?.label, b3?.label, b3?.p?.x, b1?.p?.x, calls);
console.log("call chain", box(2)?.p?.y, box(0)?.p?.y ?? -1, calls);
console.log("lengths", b3?.label?.length, b1?.label?.length, b3?.label?.toUpperCase());
const xs: number[] | undefined = flag ? [1, 2, 3] : undefined;
console.log("elements", xs?.[1], xs?.length, xs?.map((x) => x * 2));

const v: number = maybe(7)!;
console.log("asserted", v, b3!.label!, b3!.p!.x);

let z: number | undefined = maybe(0);
z ??= 10;
z ??= 20;
const o: Box = { p: undefined, label: null };
o.label ??= "set";
o.label ??= "again";
console.log("assign", z, o.label);

const list: Link = { v: 1, next: { v: 2, next: undefined } };
console.log("list", list.next?.v, list.next?.next?.v, list.next?.next?.next?.v);

let a: number = 1;
function bump(): number {
  a = 10;
  return 0;
}
console.log("order", a + (flag ? bump() : 0), a);

async function later(n: number): Promise<number | undefined> {
  return n > 0 ? n : undefined;
}
async function main(): Promise<void> {
  const r: number = (await later(0)) ?? (await later(5)) ?? 0;
  console.log("async", r, flag ? await later(1) : 0);
}
main();
console.log("sync done");
`

const nodeConds = `ternary yes 2 1
nested undefined a 1
coalesce 3 100 3
null -1 4
chain undefined null big 3 undefined 3
call chain -2 -1 5
lengths 3 undefined BIG
elements 2 3 [ 2, 4, 6 ]
asserted 7 big 3
assign 10 set
list 2 undefined undefined
order 1 10
sync done
async 5 1
`

func TestCondsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "conds", condsProgram), nodeConds, 20)
}
