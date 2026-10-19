package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// membersProgram covers M5 step 6, the members that may be undefined:
//   - find and findLast, on objects, with the index and array their callback
//     gets, finding and not;
//   - at, negative, past either end, and fractional;
//   - pop and shift as values and statements, down to an empty array, and an
//     alias seeing them;
//   - a string's at, codePointAt at a pair's lead and trail, and s[i];
//   - an array of maybes, read past its end, whichever the flag;
//   - `xs[0] ??= v` on an empty array, which appends.
//
// Node 24.18 ran this same file and printed nodeMembers, the same in 5 of 5
// runs.
const membersProgram = `interface User { id: number; name: string }

const users: User[] = [{ id: 1, name: "ada" }, { id: 2, name: "bo" }, { id: 3, name: "cy" }];
const found: User | undefined = users.find((u) => u.id === 2);
const missing: User | undefined = users.find((u) => u.id === 9);
console.log("find", found, missing, users.findLast((u) => u.id < 3)?.name, users.find((u, i, all) => i === all.length - 1)?.name);
console.log("at", users.at(0)?.name, users.at(-1)?.name, users.at(5), users.at(-9), [1, 2, 3].at(1.9));

const stack: number[] = [1, 2, 3];
const last: number | undefined = stack.pop();
stack.pop();
const first: number | undefined = stack.shift();
const alias: number[] = stack;
console.log("pop shift", last, first, stack, alias.length, stack.pop(), stack.shift());

const s: string = "a😀b";
console.log("string at", s.at(0), s.at(-1), s.at(9), s.codePointAt(1), s.codePointAt(2), s.codePointAt(9), s[3], "x".codePointAt(0));

const maybes: (number | undefined)[] = [1, undefined];
console.log("maybe elements", maybes[0], maybes[1], maybes[5], maybes.at(-1), maybes.find((m) => m === undefined));

const sum: number = users.reduce((acc, u) => acc + (u.name.at(0) === "a" ? 1 : 0), 0);
console.log("sum", sum);
const cs: number[] = [];
cs[0] ??= 7;
cs[0] ??= 8;
console.log("??=", cs);
`

const nodeMembers = `find { id: 2, name: 'bo' } undefined bo cy
at ada cy undefined undefined 2
pop shift 3 1 [] 0 undefined undefined
string at a b undefined 128512 56832 undefined b 120
maybe elements 1 undefined undefined undefined undefined
sum 1
??= [ 7 ]
`

func TestMembersMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "members", membersProgram), nodeMembers, 20)
}

// uncheckedProgram reads elements under noUncheckedIndexedAccess (D9): past
// the end, negative and fractional they are undefined, as Node reads them,
// a string's too; narrowed, a read is the element; and ?. and ?? take them.
// Node 24.18 ran the same file and printed nodeUnchecked, 5 of 5 runs.
const uncheckedProgram = `const xs: number[] = [10, 20];
const i: number = 5;
const a = xs[0];
const b = xs[i];
console.log("unchecked", a, b, xs[-1], xs[1.5], a ?? -1, b ?? -1);
const s: string = "hi";
console.log("chars", s[0], s[9], s[0] ?? "?", s[9] ?? "?");
if (xs[1] !== undefined) {
  console.log("narrowed", xs[1] + 1);
}
for (let k = 0; k < 3; k++) {
  const v: number | undefined = xs[k];
  console.log("k", k, v === undefined ? -1 : v * 2);
}
const grid: number[][] = [[1, 2], [3]];
console.log("grid", grid[1]?.[0], grid[1]?.[5], grid[7]?.[0] ?? -1);
`

const nodeUnchecked = `unchecked 10 undefined undefined undefined 10 -1
chars h undefined h ?
narrowed 21
k 0 20
k 1 40
k 2 -1
grid 3 undefined -1
`

const uncheckedConfig = `{
  "compilerOptions": {
    "target": "esnext",
    "module": "esnext",
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "noEmit": true
  },
  "include": ["src"],
  "cakebear": { "main": "src/main.ts", "output": "bin/unchecked" }
}
`

func TestUncheckedIndexReadsMatchNode(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "unchecked")
	writeProject(t, dir, map[string]string{"tsconfig.json": uncheckedConfig, "src/main.ts": uncheckedProgram})
	if code, stderr := projectBuild(t, dir, buildOptions{}); code != exitOK {
		t.Fatalf("build: exit %d\n%s", code, stderr)
	}
	bin := filepath.Join(dir, "bin", "unchecked")
	if _, err := exec.LookPath(bin); err != nil {
		t.Fatalf("no binary: %v", err)
	}
	matchesNode(t, bin, nodeUnchecked, 20)
}
