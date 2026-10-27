package main

import "testing"

// unknownSlotsProgram holds what M6's re-measurement of wx-next (check 10,
// step 7) found refused or built wrong, each in a shape of its own:
//   - ||, && and ?: as what a callback returns where a library's declaration
//     types the result unknown (filter's, find's, every's): M5 lowered them,
//     and M6 boxed each operand, so || and && were refused and ?: was
//     invalid IR;
//   - || and && into an unknown's slot, now boxed once, whole;
//   - an optional chain whose last link reads an unknown;
//   - a shorthand property into a field typed unknown;
//   - ?: choosing between a value and an unknown, and an array literal its
//     elements type unknown[], neither in an unknown's slot;
//   - typeof of a value whose type isn't unknown: a maybe's tag is its
//     state's, and a defensive check of a known type is common.
//
// Node 24.18 ran this same file and printed nodeUnknownSlots.
const unknownSlotsProgram = `// ||, && and ?: as what a callback returns, which a library types unknown.
const xs: string[] = ["ab", "c", "def"];
console.log("filter", xs.filter((x) => x.length > 1 || x === "c"));
console.log("block", xs.filter((x) => {
  return x.length > 2 && x !== "zzz";
}));
console.log("findIndex", xs.findIndex((x) => !(x === "c" || x === "q")));
console.log("every", xs.every((x) => (x.length > 0 ? x !== "q" : false)));
console.log("some", xs.some((x) => x === "c" || x === "q" || x === "r"));
console.log("find", xs.find((x) => (x.length > 2 ? true : x === "c")));
console.log("nested", xs.filter((x) => x === "ab" || (x.length > 2 ? x !== "q" : false)));
// || and && into an unknown's slot, boxed once, whole.
interface Flag {
  flag: unknown;
}
function flags(a: boolean, b: boolean): Flag[] {
  return [{ flag: a || b }, { flag: a && b }];
}
function label(name: string): unknown {
  return name || "anon";
}
console.log("slot", flags(true, false), flags(false, false), label(""), label("bo"));
// An optional chain whose last link reads an unknown.
interface Box {
  inner?: { payload?: unknown };
}
function payload(b: Box): unknown {
  return b.inner?.payload;
}
function show(b: Box): void {
  const p = b.inner?.payload;
  console.log("chain", p);
}
console.log("chain", payload({}), payload({ inner: {} }), payload({ inner: { payload: 3 } }), payload({ inner: { payload: null } }));
show({});
show({ inner: { payload: "x" } });
// A shorthand property into an unknown's field.
interface Out {
  ok: boolean;
  detail: unknown;
}
function wrap(detail: unknown): Out {
  if (detail instanceof Error) {
    return { ok: false, detail };
  }
  return { ok: true, detail };
}
function named(detail: string): Out {
  return { ok: true, detail };
}
console.log("shorthand", wrap(new RangeError("r")), wrap(7), named("n"));
// ?: choosing between a value and an unknown, in no unknown's slot.
function report(e: unknown): void {
  console.log("report", { error: e instanceof Error ? e.message : e });
}
report(new Error("boom"));
report("text");
report(undefined);
// An array literal its elements type unknown[], in no slot.
function parts(id: unknown): void {
  const expr = { split: [id, "/"] };
  console.log("parts", expr, expr.split.length);
}
parts("a/b");
parts(7);
// typeof of a value whose type isn't unknown.
function size(s: string | undefined): number {
  return typeof s === "string" ? s.length : -1;
}
function lengthOf(text: string): number {
  if (!text || typeof text !== "string") {
    return 0;
  }
  return text.length;
}
function tags(n: number | null, b: boolean, o: Flag, list: number[], e: Error): boolean[] {
  return [typeof n === "object", typeof n === "number", typeof b === "boolean", typeof o === "object", typeof list === "object", typeof e === "object", typeof e !== "function"];
}
console.log("typeof", size("abc"), size(undefined), lengthOf("hello"), lengthOf(""));
console.log("tags", tags(null, true, { flag: 1 }, [], new Error("e")), tags(4, false, { flag: null }, [1], new TypeError("t")));
`

const nodeUnknownSlots = `filter [ 'ab', 'c', 'def' ]
block [ 'def' ]
findIndex 0
every true
some true
find c
nested [ 'ab', 'def' ]
slot [ { flag: true }, { flag: false } ] [ { flag: false }, { flag: false } ] anon bo
chain undefined undefined 3 null
chain undefined
chain x
shorthand { ok: false, detail: [RangeError: r] } { ok: true, detail: 7 } { ok: true, detail: 'n' }
report { error: 'boom' }
report { error: 'text' }
report { error: undefined }
parts { split: [ 'a/b', '/' ] } 2
parts { split: [ 7, '/' ] } 2
typeof 3 -1 5 0
tags [
  true, false,
  true, true,
  true, true,
  true
] [
  false, true,
  true,  true,
  true,  true,
  true
]
`

func TestUnknownSlotsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "unknown_slots", unknownSlotsProgram), nodeUnknownSlots, 20)
}
