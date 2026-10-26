package main

import "testing"

// jsonProgram covers M4 step 9 (plan D15), JSON.stringify:
//   - objects in their literal's key order, arrays, nesting, and a shared
//     object serialised at each place it appears;
//   - a space as a number (clamped to 10, truncated, compact at 0 and
//     below) or a string (its first 10 characters), and empty containers
//     under one. A number strictly between 0 and 1 is V8's own: lines and
//     `": "` but no indent, where the specification says compact;
//   - -0 as 0, NaN and the infinities as null, and numbers as JavaScript
//     prints them;
//   - strings escaped as JSON escapes them, and no more: DEL, U+2028 and
//     non-ASCII text go through as they are;
//   - keys that need quoting, `null` and `undefined` as the replacer, a
//     result inside a larger expression evaluated in order, an object
//     literal argument whose field reads are ordered against a call in it,
//     and object literals written in place, typed by what they hold.
//
// Node 24.18 ran this same file and printed nodeJSON, the same in 5 of 5
// runs.
const jsonProgram = `export {};
interface Point { x: number; y: number }
interface Item { name: string; price: number; tags: string[]; at: Point }
const p: Point = { y: 2, x: 1 };
console.log(JSON.stringify(p));
const items: Item[] = [
  { name: "tea", price: 4.5, tags: ["hot", "green"], at: { x: 0, y: -0 } },
  { tags: [], at: p, price: 0.1 + 0.2, name: "quote \" back\\slash\nnewline\ttab" },
];
console.log(JSON.stringify(items));
console.log(JSON.stringify(items, null, 2));
console.log(JSON.stringify(items[0], undefined, "--"));
const shell: { empty: number[]; nested: { a: string[]; o: { k: boolean } } } = { empty: [], nested: { a: [], o: { k: false } } };
console.log(JSON.stringify(shell, null, 4));
const weird: { nan: number; inf: number; neg: number; big: number; tiny: number; zero: number } = {
  nan: 0 / 0, inf: 1 / 0, neg: -1 / 0, big: 1e21, tiny: 1e-7, zero: -0,
};
console.log(JSON.stringify(weird));
console.log(JSON.stringify("日本 \u0001 \u001f \u007f   é 😀"));
console.log(JSON.stringify(42) + " " + JSON.stringify(true) + " " + JSON.stringify(-0) + " " + JSON.stringify(5e-324));
const space: number = 3;
const grid: number[][] = [[1], [2, 3], []];
console.log(JSON.stringify(grid, null, space));
console.log(JSON.stringify(p, null, 20));
console.log(JSON.stringify(p, null, 0));
console.log(JSON.stringify(p, null, 1.9));
console.log(JSON.stringify(p, null, 0.5));
const tiny: number = 1e-9;
console.log(JSON.stringify(grid, null, tiny));
console.log(JSON.stringify(p, null, -0.5));
console.log(JSON.stringify(p, null, -2));
console.log(JSON.stringify(p, null, "abcdefghijklmnop"));
console.log(JSON.stringify(p, null, ""));
const shared: Point = { x: 5, y: 5 };
const pair: { a: Point; b: Point } = { a: shared, b: shared };
console.log(JSON.stringify(pair));
const counter: { n: number } = { n: 0 };
function bump(): number {
  counter.n = counter.n + 1;
  return counter.n;
}
const both: string = JSON.stringify(counter) + JSON.stringify(bump()) + JSON.stringify(counter);
console.log(both);
const snap: string = JSON.stringify({ before: counter.n, bumped: bump(), after: counter.n });
console.log(snap);
const label: string = "k";
const keyed: { "content-type": string; $id: number; "quo\"te": string } = { "content-type": "text/plain", $id: 1, "quo\"te": label };
console.log(JSON.stringify(keyed));
console.log(JSON.stringify(items.map((it: Item): string => it.name)));
const names: string[] = ["a", "b"];
console.log(JSON.stringify({ ok: true, names, count: names.length }));
console.log(JSON.stringify([{ id: 1 }, { id: 2 }]));
console.log(JSON.stringify({ nested: { deep: [1, 2] } }, null, 2));
`

const nodeJSON = "{\"y\":2,\"x\":1}\n[{\"name\":\"tea\",\"price\":4.5,\"tags\":[\"hot\",\"green\"],\"at\":{\"x\":0,\"y\":0}},{\"tags\":[],\"at\":{\"y\":2,\"x\":1},\"price\":0.30000000000000004,\"name\":\"quote \\\" back\\\\slash\\nnewline\\ttab\"}]\n[\n  {\n    \"name\": \"tea\",\n    \"price\": 4.5,\n    \"tags\": [\n      \"hot\",\n      \"green\"\n    ],\n    \"at\": {\n      \"x\": 0,\n      \"y\": 0\n    }\n  },\n  {\n    \"tags\": [],\n    \"at\": {\n      \"y\": 2,\n      \"x\": 1\n    },\n    \"price\": 0.30000000000000004,\n    \"name\": \"quote \\\" back\\\\slash\\nnewline\\ttab\"\n  }\n]\n{\n--\"name\": \"tea\",\n--\"price\": 4.5,\n--\"tags\": [\n----\"hot\",\n----\"green\"\n--],\n--\"at\": {\n----\"x\": 0,\n----\"y\": 0\n--}\n}\n{\n    \"empty\": [],\n    \"nested\": {\n        \"a\": [],\n        \"o\": {\n            \"k\": false\n        }\n    }\n}\n{\"nan\":null,\"inf\":null,\"neg\":null,\"big\":1e+21,\"tiny\":1e-7,\"zero\":0}\n\"\u65e5\u672c \\u0001 \\u001f \u007f \u2028 \u00e9 \U0001f600\"\n42 true 0 5e-324\n[\n   [\n      1\n   ],\n   [\n      2,\n      3\n   ],\n   []\n]\n{\n          \"y\": 2,\n          \"x\": 1\n}\n{\"y\":2,\"x\":1}\n{\n \"y\": 2,\n \"x\": 1\n}\n{\n\"y\": 2,\n\"x\": 1\n}\n[\n[\n1\n],\n[\n2,\n3\n],\n[]\n]\n{\"y\":2,\"x\":1}\n{\"y\":2,\"x\":1}\n{\nabcdefghij\"y\": 2,\nabcdefghij\"x\": 1\n}\n{\"y\":2,\"x\":1}\n{\"a\":{\"x\":5,\"y\":5},\"b\":{\"x\":5,\"y\":5}}\n{\"n\":0}1{\"n\":1}\n{\"before\":1,\"bumped\":2,\"after\":2}\n{\"content-type\":\"text/plain\",\"$id\":1,\"quo\\\"te\":\"k\"}\n[\"tea\",\"quote \\\" back\\\\slash\\nnewline\\ttab\"]\n{\"ok\":true,\"names\":[\"a\",\"b\"],\"count\":2}\n[{\"id\":1},{\"id\":2}]\n{\n  \"nested\": {\n    \"deep\": [\n      1,\n      2\n    ]\n  }\n}\n"

func TestJSONStringifyMatchesNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "json", jsonProgram), nodeJSON, 20)
}

// cycleProgram serialises a structure that refers to itself. Node throws a
// TypeError naming the path around the circle, and so does cakebear (M6
// plan D9), which nothing catches here.
const cycleProgram = `export {};
interface Link { name: string; next: Link[] }
const a: Link = { name: "a", next: [] };
const b: Link = { name: "b", next: [a] };
a.next.push(b);
console.log("before");
console.log(JSON.stringify(a));
console.log("after");
`

// nodeCycle is the message Node 24.18 threw for cycleProgram.
const nodeCycle = "Converting circular structure to JSON\n" +
	"    --> starting at object with constructor 'Object'\n" +
	"    |     property 'next' -> object with constructor 'Array'\n" +
	"    |     index 0 -> object with constructor 'Object'\n" +
	"    |     property 'next' -> object with constructor 'Array'\n" +
	"    --- index 0 closes the circle"

func TestJSONCycleThrowsAsNode(t *testing.T) {
	t.Parallel()
	checkUncaught(t, buildProgram(t, "cycle", cycleProgram), "before\n", "[TypeError: "+nodeCycle+"]")
}
