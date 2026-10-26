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

// unknownFlowsProgram covers what agent m6-sweep found, once step 3's
// unknowns met library calls, function slots, awaits, chains and literals.
// Each built wrong, or was refused though CLAUDE.md lists it:
//   - push, includes and indexOf on an unknown[], whose T only the
//     instantiation makes unknown, took their arguments unboxed, as did a
//     callback declared `(…): unknown`: invalid IR;
//   - a literal returning its own type in a `=> unknown` slot, an optional
//     unknown parameter left out, and `return fail()` with fail returning
//     never, each invalid IR or a go build failure;
//   - `await` into an unknown's slot boxed the promise, not its value;
//   - `e?.message` on an Error | null, String() of one, join over unknowns
//     and Errors, and an array literal in an unknown's slot were refused.
//
// Node 24.18 ran this same file under --stack-trace-limit=0 and printed
// nodeUnknownFlows, the same in 3 of 3 runs. It has a template literal, so
// it is written as a quoted string.
const unknownFlowsProgram = "export {};\n" +
	"// Library slots an instantiation types unknown take their values boxed.\n" +
	"const xs: unknown[] = [];\n" +
	"xs.push(1, \"a\", null);\n" +
	"console.log(\"unknown[]\", xs, xs.includes(1), xs.includes(\"b\"), xs.indexOf(\"a\"), xs.indexOf(null));\n" +
	"const ns: number[] = [1, 2];\n" +
	"const doubled: unknown[] = ns.map((n: number): unknown => n * 2);\n" +
	"console.log(\"map\", doubled);\n" +
	"// A literal returning its own type into a `=> unknown` slot.\n" +
	"const g: (n: number) => unknown = (n: number): number => n + 1;\n" +
	"const h: (s: string) => unknown = (s: string): string => {\n" +
	"  if (s === \"\") return \"empty\";\n" +
	"  return s + \"!\";\n" +
	"};\n" +
	"console.log(\"literals\", g(1), h(\"\"), h(\"x\"));\n" +
	"// An optional unknown parameter, left out.\n" +
	"function described(u?: unknown): string {\n" +
	"  return (typeof u === \"undefined\" ? \"left out: \" : \"given: \") + String(u);\n" +
	"}\n" +
	"console.log(\"optional\", described(), described(undefined), described(null), described(2));\n" +
	"// An await's value, boxed, not its promise.\n" +
	"async function get(): Promise<number> {\n" +
	"  return 7;\n" +
	"}\n" +
	"const awaited: unknown = await get();\n" +
	"console.log(\"await\", awaited);\n" +
	"// `?.` reaching an Error's members, and String() of an Error that may be null.\n" +
	"function pick(i: number): Error | null {\n" +
	"  return i > 0 ? new RangeError(\"e\" + String(i)) : null;\n" +
	"}\n" +
	"const some: Error | null = pick(2);\n" +
	"const none: Error | null = pick(0);\n" +
	"console.log(\"chain\", some?.message, none?.message, some?.name, none?.name);\n" +
	"console.log(\"String\", String(some), String(none), `${some} and ${none}`);\n" +
	"// join over unknowns and Errors.\n" +
	"const mixed: unknown[] = [1, \"a\", null, undefined, true, -0, { k: 1 }, new TypeError(\"t\")];\n" +
	"const errs: Error[] = [new Error(\"a\"), new URIError(\"b\")];\n" +
	"console.log(\"join\", mixed.join(\"-\"), errs.join());\n" +
	"// A literal in an unknown's slot is typed by its own elements.\n" +
	"const lit: unknown = [\"a\", null];\n" +
	"const obj: unknown = { k: \"v\", n: [1] };\n" +
	"console.log(\"literal\", lit, obj, JSON.stringify([lit, obj]));\n" +
	"// `return fail()`, fail returning never.\n" +
	"function fail(msg: string): never {\n" +
	"  throw new RangeError(msg);\n" +
	"}\n" +
	"function checked(i: number): number {\n" +
	"  if (i > 10) {\n" +
	"    return fail(\"too big: \" + String(i));\n" +
	"  }\n" +
	"  return i;\n" +
	"}\n" +
	"const concise = (i: number): number => fail(\"concise \" + String(i));\n" +
	"try {\n" +
	"  console.log(\"never\", checked(1), checked(11));\n" +
	"} catch (e) {\n" +
	"  console.log(\"caught\", e);\n" +
	"}\n" +
	"try {\n" +
	"  console.log(\"never\", concise(3));\n" +
	"} catch (e) {\n" +
	"  console.log(\"caught\", e);\n" +
	"}\n"

const nodeUnknownFlows = `unknown[] [ 1, 'a', null ] true false 1 2
map [ 2, 4 ]
literals 2 empty x!
optional left out: undefined left out: undefined given: null given: 2
await 7
chain e2 undefined RangeError undefined
String RangeError: e2 null RangeError: e2 and null
join 1-a---true-0-[object Object]-TypeError: t Error: a,URIError: b
literal [ 'a', null ] { k: 'v', n: [ 1 ] } [["a",null],{"k":"v","n":[1]}]
caught [RangeError: too big: 11]
caught [RangeError: concise 3]
`

func TestUnknownFlowsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "unknown_flows", unknownFlowsProgram), nodeUnknownFlows, 20)
}
