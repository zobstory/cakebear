package main

import "testing"

// paramsProgram covers M5 step 7b, optional and default parameters and
// defaults in destructuring:
//   - a function's optional and default parameters, left out, passed
//     undefined and passed a value;
//   - defaults that read earlier parameters and call a function, each only
//     where its argument is undefined;
//   - a default of a parameter that may be null, null kept;
//   - an arrow's;
//   - defaults in a parameter's pattern, and a pattern parameter's own
//     default;
//   - a defaulted string parameter appended to;
//   - defaults in a declaration's pattern and a for…of's.
//
// Node 24.18 ran this same file and printed nodeParams, the same in 5 of 5
// runs.
const paramsProgram = `interface Opts { size?: number; label?: string | null }

function greet(who: string, greeting: string = "hello", punct?: string): string {
  return ` + "`" + `${greeting}, ${who}${punct ?? "."}` + "`" + `;
}
let calls: number = 0;
function next(): number {
  calls += 1;
  return calls * 10;
}
function scaled(a: number, b: number = a * 2, c: number = next()): number {
  return a + b + c;
}
function nullable(x: number | null = 7): string {
  return ` + "`" + `${x}` + "`" + `;
}
const area = (w: number, h: number = w): number => w * h;
const opt = (n?: number): string => (n === undefined ? "none" : ` + "`" + `n=${n}` + "`" + `);
function describe({ size = 1, label = "x" }: Opts): string {
  return ` + "`" + `${label}:${size}` + "`" + `;
}
function whole({ size }: Opts = { size: 3 }): number {
  return size ?? -1;
}
function repeated(s: string = ""): string {
  for (let i = 0; i < 3; i++) {
    s += "ab";
  }
  return s;
}

console.log("greet", greet("ada"), greet("bo", "hi"), greet("cy", undefined, "!"), greet("di", "hey", "?"));
console.log("scaled", scaled(1), scaled(1, 1), scaled(1, undefined, 0), calls);
console.log("nullable", nullable(), nullable(null), nullable(undefined), nullable(2));
console.log("arrows", area(3), area(3, 4), opt(), opt(5), opt(undefined));
console.log("patterns", describe({}), describe({ size: 2 }), describe({ label: null }), describe({ label: undefined, size: 0 }));
console.log("whole", whole(), whole({}), whole({ size: 4 }));
const { size = 10, label: title = "anon" }: Opts = { size: undefined, label: "set" };
console.log("decl", size, title, repeated(), repeated("x"));
const items: Opts[] = [{ size: 1 }, {}, { label: "z" }];
for (const { size: s = 0, label = "-" } of items) {
  console.log("loop", s, label);
}
`

const nodeParams = `greet hello, ada. hi, bo. hello, cy! hey, di?
scaled 13 22 3 2
nullable 7 null 7 2
arrows 9 12 none n=5 none
patterns x:1 x:2 null:1 x:0
whole 3 -1 4
decl 10 set ababab xababab
loop 1 -
loop 0 -
loop 0 z
`

func TestParamsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "params", paramsProgram), nodeParams, 20)
}
