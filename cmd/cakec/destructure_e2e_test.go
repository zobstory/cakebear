package main

import "testing"

// destructureProgram covers M4 step 8 (plan D14): shorthand properties, and
// object destructuring in const and let declarations, nested and renamed, in
// the parameters of functions and closures, and in for…of, plus a shorthand
// holding a function.
//
// destructureEdgesProgram covers the corners:
//   - names a pattern gives that nothing reads;
//   - a destructured let that a closure changes;
//   - the value destructured, evaluated once;
//   - an object read out of a field, shared rather than copied;
//   - a pattern in a block shadowing one outside it;
//   - names Go reserves (func, type);
//   - callbacks declaring fewer parameters than their slot;
//   - a shorthand of a top-level let, read after it changes;
//   - a shorthand naming a function with fewer parameters than its field.
//
// Node 24.18 ran these same files and printed nodeDestructure and
// nodeDestructureEdges, the same in 5 of 5 runs.
const destructureProgram = `export {};
interface Point { x: number; y: number }
interface Item { name: string; price: number; at: Point }

const origin: Point = { x: 0, y: 0 };
const name: string = "tea";
const price: number = 4;
const tea: Item = { name, price, at: origin };
console.log(tea);

function make(name: string, x: number): Item {
  const at: Point = { y: x * 2, x };
  return { at, price: x + 1, name };
}
console.log(make("cake", 3));

const { x, y: down } = make("bun", 5).at;
console.log(x, down);

const { name: label, at: { y } } = tea;
console.log(y, label);

let { price: cost } = tea;
cost = cost * 10;
console.log(cost, tea.price);

function describe({ name, price }: Item): number {
  console.log(name + " costs");
  return price * 2;
}
const total = ({ at: { x, y } }: Item): number => x + y;
console.log(total(make("jam", 2)), describe(tea));

const items: Item[] = [tea, make("pie", 1)];
for (const { name, at: { y } } of items) {
  console.log(y, name);
}

const describeAll: (xs: Item[]) => string[] = (xs: Item[]): string[] => xs.map(({ name }) => name);
console.log(describeAll(items));

const log = (n: number): void => {
  console.log("log", n);
};
const holder: { log: (n: number) => void } = { log };
const f: (n: number) => void = holder.log;
f(7);
`

const nodeDestructure = "{ name: 'tea', price: 4, at: { x: 0, y: 0 } }\n{ at: { y: 6, x: 3 }, price: 4, name: 'cake' }\n5 10\n0 tea\n40 4\ntea costs\n6 8\n0 tea\n2 pie\n[ 'tea', 'pie' ]\nlog 7\n"

const destructureEdgesProgram = `export {};
interface P { x: number; y: number }
function f(p: P): number {
  const { x, y } = p;
  return x;
}
console.log(f({ x: 1, y: 2 }));
function g({ x, y }: P, k: number): number {
  return k;
}
console.log(g({ x: 1, y: 2 }, 9));
function h(p: P): number {
  let { x } = p;
  const inc = (): void => {
    x = x + 1;
  };
  inc();
  inc();
  return x + p.x;
}
console.log(h({ x: 5, y: 0 }));
let calls: number = 0;
function mk(): P {
  calls = calls + 1;
  return { x: calls, y: calls * 10 };
}
const { x: a1, y: b1 } = mk();
console.log(a1, b1, calls);
interface Box { p: P; n: number }
const box: Box = { p: { x: 1, y: 2 }, n: 3 };
const { p: inner, n } = box;
inner.x = 100;
box.n = 4;
console.log(box, n, inner);
const { x: sx } = box.p;
if (sx > 0) {
  const other: P = { x: -1, y: 0 };
  const { x: sx } = other;
  console.log(sx);
}
console.log(sx);
const twice = (k: number): number => k * 2;
const ops: { twice: (k: number) => number } = { twice };
const tw: (k: number) => number = ops.twice;
console.log(tw(21));
const { twice: t2 } = ops;
console.log(t2(4));
const pts: P[] = [{ x: 1, y: 2 }, { x: 3, y: 4 }];
console.log(pts.map(({ y }) => y));
console.log(pts.filter(({ x }, i) => x + i > 1));
for (let { x } of pts) {
  x = x * 2;
  console.log(x);
}
console.log(pts);
const { x: func, y: type } = pts[0];
console.log(func, type);
let count: number = 1;
function snap(): { count: number } {
  return { count };
}
count = 2;
console.log(snap());
function nested({ p: { x }, n }: Box, scale: number): number {
  return (x + n) * scale;
}
console.log(nested(box, 2));
const go = (): void => {
  const { n: m } = box;
  console.log(m);
};
go();
function first(a: number): number {
  return a * 3;
}
const pair: { first: (a: number, b: number) => number } = { first };
const pf: (a: number, b: number) => number = pair.first;
console.log(pf(5, 6));
`

const nodeDestructureEdges = "1\n9\n12\n1 10 1\n{ p: { x: 100, y: 2 }, n: 4 } 3 { x: 100, y: 2 }\n-1\n100\n42\n8\n[ 2, 4 ]\n[ { x: 3, y: 4 } ]\n2\n6\n[ { x: 1, y: 2 }, { x: 3, y: 4 } ]\n1 2\n{ count: 2 }\n208\n4\n15\n"

func TestDestructuringMatchesNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "destructure", destructureProgram), nodeDestructure, 20)
}

func TestDestructuringEdgesMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "destructure_edges", destructureEdgesProgram), nodeDestructureEdges, 20)
}
