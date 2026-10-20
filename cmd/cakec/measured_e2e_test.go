package main

import "testing"

// fixesProgram holds the fixes M5's re-measurement of wx-next found (check
// 9): lets declared with no value, at the top level and in a function, a
// shorthand of a narrowed binding, and async functions returning into a maybe
// and into an object holding an empty array, all refused before. Node 24.18
// ran this same file and printed nodeFixes, the same in 5 of 5 runs.
const fixesProgram = `interface Box { label: string; size?: number }
interface Ids { ids: string[]; first?: string }

let total: number;
total = 0;
let shapeName: string;
if (total === 0) {
  shapeName = "empty";
} else {
  shapeName = "full";
}
let box: Box;
box = { label: "b" };
console.log("let", total, shapeName, box);
function sign(n: number): string {
  let out: string;
  if (n < 0) {
    out = "negative";
  } else {
    out = "not negative";
  }
  return out;
}
console.log("local let", sign(-1), sign(2));

function make(label: string | undefined): Box | undefined {
  if (label === undefined) {
    return undefined;
  }
  return { label };
}
console.log("shorthand", make("x"), make(undefined));

async function ids(n: number): Promise<Ids> {
  if (n > 0) {
    return { ids: ["a"], first: "a" };
  }
  return { ids: [] };
}
async function maybeCount(n: number): Promise<number | undefined> {
  if (n > 0) {
    return n;
  }
  return undefined;
}
async function main(): Promise<void> {
  console.log("async", await ids(1), await ids(0), await maybeCount(2), await maybeCount(0));
}
main();
`

const nodeFixes = `let 0 empty { label: 'b' }
local let negative not negative
shorthand { label: 'x' } undefined
async { ids: [ 'a' ], first: 'a' } { ids: [] } 2 undefined
`

func TestMeasuredFixesMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "fixes", fixesProgram), nodeFixes, 20)
}
