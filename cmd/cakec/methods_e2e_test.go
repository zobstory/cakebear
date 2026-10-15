package main

import "testing"

// methodsProgram covers M4 step 7, the array methods (D16):
// - map, filter, some, every, findIndex, reduce and forEach over objects;
// - slice, then a stable sort by a numeric key;
// - NaN and -0 in includes (SameValueZero) and indexOf (strict equality);
// - join of numbers with -0 and NaN, of nested arrays, booleans and objects;
// - slice with negative, fractional and out-of-range bounds;
// - named functions as callbacks, adapted to (value, index, array);
// - a push during forEach, not visited; a write during map, seen;
// - stability with equal keys, and a comparator returning NaN.
//
// Node 24.18 ran this same file and printed nodeMethods, the same in 5 of 5
// runs.
const methodsProgram = `interface Item { name: string; price: number; tags: string[] }

const items: Item[] = [
  { name: "tea", price: 4, tags: ["hot"] },
  { name: "cake", price: 6, tags: ["sweet", "baked"] },
  { name: "bun", price: 4, tags: ["baked"] },
  { name: "jam", price: 3, tags: [] },
];

const names: string[] = items.map((it) => it.name);
const cheap: Item[] = items.filter((it) => it.price < 5);
console.log(names, cheap.length, names.join(" | "));
console.log(items.some((it) => it.tags.includes("hot")), items.every((it) => it.price > 2));
console.log(items.findIndex((it) => it.name === "bun"), items.findIndex((it) => it.name === "pie"));
const total: number = items.reduce((sum, it) => sum + it.price, 0);
console.log(total);

const byPrice: Item[] = items.slice().sort((a, b) => a.price - b.price);
console.log(byPrice.map((it) => it.name), items.map((it) => it.name));

const nan: number = 0 / 0;
const nums: number[] = [5, -0, 0, 3, nan, 1];
console.log(nums.includes(nan), nums.indexOf(nan), nums.includes(-0), nums.indexOf(0), nums.indexOf(-0));
const pairs: number[][] = [[1, 2], [3, 4], []];
const flags: boolean[] = [true, false];
console.log("joined:", nums.join(), pairs.join(";"), flags.join("-"), items.join());
console.log(nums.slice(-2), nums.slice(1, -1), nums.slice(1.7, 3.2), nums.slice(10), nums.slice(-100, 2));

function isBig(n: number): boolean {
  return n > 2;
}
function show(n: number): void {
  console.log("each", n);
}
const five: number[] = [1, 2, 3, 4, 5];
const odd: number[] = five.filter(isBig);
[7, 8].forEach(show);
console.log(odd);

const grow: number[] = [1, 2, 3];
grow.forEach((x) => {
  if (x < 3) {
    grow.push(x * 10);
  }
});
console.log(grow);

const live: number[] = [1, 2, 3];
const seen: number[] = live.map((x, i, arr) => {
  if (i === 0) {
    arr[2] = 30;
  }
  return x + i;
});
console.log(seen);

interface Tie { k: number; id: string }
const ties: Tie[] = [{ k: 2, id: "b2" }, { k: 1, id: "a1" }, { k: 2, id: "b1" }, { k: 1, id: "a2" }, { k: 3, id: "c1" }];
console.log(ties.sort((x, y) => x.k - y.k).map((t) => t.id));
const odd2: number[] = [3, 1, 2];
const desc: number[] = [10, 9, 1];
console.log(odd2.sort((a, b) => nan), desc.sort((a, b) => a - b));
console.log(five.reduce((acc, x, i) => acc + x * i, 100));
`

const nodeMethods = "[ 'tea', 'cake', 'bun', 'jam' ] 3 tea | cake | bun | jam\ntrue true\n2 -1\n17\n[ 'jam', 'tea', 'bun', 'cake' ] [ 'tea', 'cake', 'bun', 'jam' ]\ntrue -1 true 1 1\njoined: 5,0,0,3,NaN,1 1,2;3,4; true-false [object Object],[object Object],[object Object],[object Object]\n[ NaN, 1 ] [ -0, 0, 3, NaN ] [ -0, 0 ] [] [ 5, -0 ]\neach 7\neach 8\n[ 3, 4, 5 ]\n[ 1, 2, 3, 10, 20 ]\n[ 1, 3, 32 ]\n[ 'a1', 'a2', 'b2', 'b1', 'c1' ]\n[ 3, 1, 2 ] [ 1, 9, 10 ]\n140\n"

func TestArrayMethodsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "methods", methodsProgram), nodeMethods, 20)
}

// TestConstantArithmeticIsIEEE: Go evaluates arithmetic on constants exactly,
// at compile time, and JavaScript rounds every step as a double. Until M4
// step 7, `0.1 + 0.2` printed 0.3 and `0.1 + 0.2 === 0.3` was true, silently,
// and `1e308 * 10` and `x / 0` failed go build. Node 24.18 prints the want.
func TestConstantArithmeticIsIEEE(t *testing.T) {
	t.Parallel()

	const src = `console.log(0.1 + 0.2);
const x: number = 1;
console.log(x + (0.1 + 0.2));
console.log(0.1 + 0.2 === 0.3);
console.log(1e308 * 10, -1e308 * 10);
console.log(0 / 0, 1 / 0, -1 / 0, x / 0, x / -0);
console.log(1 / 3, 2 / 3 * 3, 0.1 * 3);
console.log(-(0.1) + -(0.2), -(1) / 0);
`
	matchesNode(t, buildProgram(t, "constants", src), "0.30000000000000004\n1.3\nfalse\nInfinity -Infinity\nNaN Infinity -Infinity Infinity -Infinity\n0.3333333333333333 2 0.30000000000000004\n-0.30000000000000004 -Infinity\n", 5)
}
