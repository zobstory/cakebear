package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// maybesProgram covers M5 step 3, values that may be null or undefined:
//   - results, parameters, lets and fields of a maybe, of numbers, strings,
//     objects and arrays, and a let declared with no value;
//   - a function returning undefined where it ends or says `return;`;
//   - printing them, alone and in objects and arrays, in templates and by
//     String();
//   - ===, !==, == null and != null, a maybe against a value and against
//     another maybe, and NaN and -0 inside one;
//   - narrowing by a check, of a binding, a field and a for…of element, and a
//     linked list walked to its end;
//   - arrays of maybes: push, includes and indexOf of undefined, null and NaN,
//     map and findIndex;
//   - JSON.stringify leaving out an undefined field and writing an array's as
//     null.
//
// Node 24.18 ran this same file and printed nodeMaybes, the same in 5 of 5
// runs.
const maybesProgram = `interface Point { x: number; y: number }
interface Item { name: string; price: number | undefined; tag: string | null }
interface Link { v: number; next: Link | undefined }
interface Opt { a: number | undefined; b: string | null; c: number }

function pick(i: number): number | undefined {
  if (i > 0) {
    return i * 2;
  }
}
function label(s: string | null): string {
  if (s === null) {
    return "none";
  }
  return "tag " + s;
}
function first(xs: number[]): number | null {
  if (xs.length === 0) {
    return null;
  }
  return xs[0];
}
function where(p: Point | undefined): string {
  if (p !== undefined) {
    return ` + "`" + `at ${p.x},${p.y}` + "`" + `;
  }
  return "nowhere";
}
function nothing(n: number): string | undefined {
  while (n > 0) {
    if (n === 3) {
      return;
    }
    n = n - 1;
  }
  return "done";
}

const a: number | undefined = pick(2);
const b: number | undefined = pick(0);
const c: number | null = first([]);
const d: number | null = first([7, 8]);
console.log(a, b, c, d);
console.log("strings", ` + "`" + `${a} ${b} ${c} ${d}` + "`" + `, String(b), String(c));
console.log(a === 4, b === undefined, c === null, c === undefined, b === null);
console.log(a == null, b == null, c == null, d != null, b !== undefined);
console.log(a === b, b === pick(-1), a === pick(2), c === first([]));
console.log("calls", label(null), label("x"), where(undefined), where({ x: 1, y: 2 }));
console.log("returns", nothing(5), nothing(2));

let m: string | undefined;
console.log(m);
m = "set";
console.log("narrowed", m, m.length);
m = undefined;
console.log(m === undefined);

const items: Item[] = [
  { name: "a", price: 3, tag: null },
  { name: "b", price: undefined, tag: "sale" },
];
let total: number = 0;
for (const it of items) {
  if (it.price !== undefined) {
    total += it.price;
  }
  console.log("item", it.name, label(it.tag), it.price);
}
console.log(total, items);
items[1].price = 5;
items[0].tag = "new";
console.log(items[1], JSON.stringify(items));

const maybes: (number | undefined)[] = [1, undefined, 0 / 0];
maybes.push(undefined, 4);
console.log(maybes, maybes.length);
console.log(maybes.includes(undefined), maybes.includes(0 / 0), maybes.includes(2), maybes.indexOf(undefined), maybes.indexOf(4));
console.log(maybes.map((x) => ` + "`" + `${x}` + "`" + `), maybes.findIndex((x) => x === undefined));
const o: Opt = { a: undefined, b: null, c: 1 };
console.log("json", JSON.stringify(maybes), o, JSON.stringify(o));
o.a = 2;
console.log("json", JSON.stringify(o), JSON.stringify(o, null, 2));

const nulls: (string | null)[] = ["x", null];
console.log(nulls, JSON.stringify(nulls), nulls.indexOf(null));

const tail: Link = { v: 1, next: undefined };
const list: Link = { v: 2, next: { v: 3, next: tail } };
let cur: Link | undefined = list;
let sum: number = 0;
while (cur !== undefined) {
  sum += cur.v;
  cur = cur.next;
}
console.log(sum, list);

const z: number | undefined = pick(-0);
const negZero: number | undefined = -0;
console.log(z, negZero === 0);
`

const nodeMaybes = `4 undefined null 7
strings 4 undefined null 7 undefined null
true true true false false
false true true true false
false true true true
calls none tag x nowhere at 1,2
returns undefined done
undefined
narrowed set 3
true
item a none 3
item b tag sale undefined
3 [
  { name: 'a', price: 3, tag: null },
  { name: 'b', price: undefined, tag: 'sale' }
]
{ name: 'b', price: 5, tag: 'sale' } [{"name":"a","price":3,"tag":"new"},{"name":"b","price":5,"tag":"sale"}]
[ 1, undefined, NaN, undefined, 4 ] 5
true true false 1 4
[ '1', 'undefined', 'NaN', 'undefined', '4' ] 1
json [1,null,null,null,4] { a: undefined, b: null, c: 1 } {"b":null,"c":1}
json {"a":2,"b":null,"c":1} {
  "a": 2,
  "b": null,
  "c": 1
}
[ 'x', null ] ["x",null] 1
6 { v: 2, next: { v: 3, next: { v: 1, next: undefined } } }
undefined true
`

func TestMaybesMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "maybes", maybesProgram), nodeMaybes, 20)
}

// TestUnsoundNarrowingPanics (M5 D4): TypeScript keeps box.n narrowed across
// the call that empties it, and Node prints NaN. cakebear's unwrap is
// checked, so the program stops there instead, saying so, and exits 2, as a
// Go panic does.
func TestUnsoundNarrowingPanics(t *testing.T) {
	t.Parallel()

	bin := buildProgram(t, "unsound", `interface Box { n: number | undefined }
const box: Box = { n: 1 };
function clear(): void {
  box.n = undefined;
}
if (box.n !== undefined) {
  clear();
  console.log(box.n + 1);
}
`)
	var stderr strings.Builder
	cmd := exec.Command(bin)
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	const want = "is undefined where TypeScript had narrowed it to a value (Node carries on with undefined)"
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(stderr.String(), want) {
		t.Errorf("%v, stderr\n%s\nwant exit 2 and %q", err, stderr.String(), want)
	}
}

// TestSpawnReturnsAMaybe: a spawned function's maybe result reaches its
// caller, empty or not. The shim `spawn = fn => Promise.resolve().then(fn)`
// ran the same file under Node 24.18 and printed the same line.
func TestSpawnReturnsAMaybe(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "spawnmaybe", `export {};
const a: number | undefined = await spawn((): number | undefined => {
  return undefined;
});
const b: string | null = await spawn((): string | null => "s");
console.log(a, b);
`), "undefined s\n", 20)
}

// TestMaybeConversionsMatchNode: a base64 and an i32 into maybes, of a string
// and a number, as a value, an argument and a result, and null and undefined
// returned as written. A shim for base64 and i32, `s => s` and `x => x | 0`,
// ran the same file under Node 24.18 and printed the same lines.
func TestMaybeConversionsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "conversions", `function show(s: string | undefined): void {
  console.log("show", s);
}
const b: base64 = base64("aGk=");
const s: string | undefined = b;
show(b);
const n: i32 = i32(3);
const m: number | undefined = n;
function back(): number | undefined {
  return n;
}
function none(): string | null {
  return null;
}
function u(): number | undefined {
  return undefined;
}
console.log("probe", s, m, back(), none(), u());
`), "show aGk=\nprobe aGk= 3 3 null undefined\n", 20)
}
