package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// arraysProgram covers M4 step 6:
//   - literals typed by their slot, the empty one included, and element reads
//     and writes, ++ and += on an element;
//   - push of several values, pop, and length set to shrink;
//   - an alias seeing a push, and === as identity;
//   - arrays through functions, for…of with continue and break, and elements
//     pushed inside a for…of, which Node visits (D13);
//   - arrays of objects and of arrays, and a long array grouped in columns as
//     Node groups it;
//   - timers made in a for…of, each with its own iteration's value.
//
// Node 24.18 ran this same file and printed nodeArrays, the same in 5 of 5
// runs.
const arraysProgram = `interface User { id: number; name: string }

const nums: number[] = [3, 1, 2];
const empty: string[] = [];
console.log(nums, empty, nums.length, empty.length);

nums[1] = 10;
nums[3] = 4;
nums[0]++;
nums[2] += 5;
const len: number = nums.push(5, 6);
console.log(nums, len);
nums.pop();
nums.length = 4;
console.log(nums, nums[nums.length - 1]);

const words: string[] = ["a", "it's", "c"];
const alias: string[] = words;
alias.push("d");
console.log(words, words === alias);

function total(xs: number[]): number {
  let sum: number = 0;
  for (const x of xs) {
    if (x > 5) {
      continue;
    }
    sum += x;
  }
  return sum;
}
function doubled(xs: number[]): number[] {
  const out: number[] = [];
  for (const x of xs) {
    out.push(x * 2);
  }
  return out;
}
console.log(total(nums), doubled(nums));

const grow: number[] = [1];
for (const g of grow) {
  if (grow.length < 5) {
    grow.push(g + 1);
  }
}
console.log(grow);

let seen: number = 0;
for (const w of words) {
  seen++;
  if (w === "c") {
    break;
  }
}
console.log(seen);

const users: User[] = [{ id: 1, name: "Ann" }, { name: "Bob", id: 2 }];
users[1].name = "Bobby";
users.push({ id: 3, name: "Cy" });
console.log(users, users[0].name);

const grid: number[][] = [[1, 2], [3, 4], []];
grid[2].push(5);
console.log(grid, grid[1][0]);

const many: number[] = [];
for (let i: number = 0; i < 30; i++) {
  many.push(i * i);
}
console.log(many);

const later: number[] = [10, 20, 30];
for (const v of later) {
  setTimeout(() => {
    console.log("later", v);
  }, 1);
}
`

const nodeArrays = "[ 3, 1, 2 ] [] 3 0\n[ 4, 10, 7, 4, 5, 6 ] 6\n[ 4, 10, 7, 4 ] 4\n[ 'a', \"it's\", 'c', 'd' ] true\n8 [ 8, 20, 14, 8 ]\n[ 1, 2, 3, 4, 5 ]\n3\n[\n  { id: 1, name: 'Ann' },\n  { name: 'Bobby', id: 2 },\n  { id: 3, name: 'Cy' }\n] Ann\n[ [ 1, 2 ], [ 3, 4 ], [ 5 ] ] 3\n[\n    0,   1,   4,   9,  16,  25,  36,  49,\n   64,  81, 100, 121, 144, 169, 196, 225,\n  256, 289, 324, 361, 400, 441, 484, 529,\n  576, 625, 676, 729, 784, 841\n]\nlater 10\nlater 20\nlater 30\n"

func TestArraysMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "arrays", arraysProgram), nodeArrays, 20)
}

// TestArrayBoundsPanic (D8, D9): what Node answers with undefined or a hole
// panics, saying what Node does instead, and the program exits 2, as a Go
// panic does.
func TestArrayBoundsPanic(t *testing.T) {
	t.Parallel()

	const xs = "const xs: number[] = [1, 2, 3];\n"
	for _, tt := range []struct {
		name, src, want string
	}{
		{"a read past the end", xs + "console.log(xs[9]);\n", "cakebear: index 9 is out of range for an array of length 3 (Node reads undefined)"},
		{"a fractional index", xs + "const i: number = 1.5;\nconsole.log(xs[i]);\n", "cakebear: 1.5 is not an array index (Node reads undefined)"},
		{"a write leaving a hole", xs + "xs[5] = 1;\n", "cakebear: writing index 5 of an array of length 3 would leave a hole (Node allows one)"},
		{"growing the length", xs + "xs.length = 10;\n", "cakebear: setting the length of an array of length 3 to 10 would leave holes (Node allows them)"},
	} {
		bin := buildProgram(t, "bounds", tt.src)
		var stderr strings.Builder
		cmd := exec.Command(bin)
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(stderr.String(), tt.want) {
			t.Errorf("%s: %v, stderr\n%s\nwant exit 2 and %q", tt.name, err, stderr.String(), tt.want)
		}
	}
}

// TestSpawnReturnsAnArray: a spawned function's array result reaches its
// caller, copied when the spawn left work behind (D11), and prints as Node
// would print the same array.
func TestSpawnReturnsAnArray(t *testing.T) {
	t.Parallel()

	const src = `export {};
const xs: number[] = await spawn((): number[] => {
  const out: number[] = [1, 2];
  setTimeout(() => {
    out.push(3);
  }, 1);
  return out;
});
console.log(xs);
`
	matchesNode(t, buildProgram(t, "spawned", src), "[ 1, 2 ]\n", 5)
}

// TestIndexWriteOrder: in `xs[i] = bump()`, JavaScript reads i before bump
// runs, though bump changes it; Go orders only calls, so the read goes
// through ts_read. Node 24.18 prints the want, 5 of 5 runs.
func TestIndexWriteOrder(t *testing.T) {
	t.Parallel()

	const src = "const xs: number[] = [0, 0];\nlet i: number = 0;\nfunction bump(): number {\n  i = 1;\n  return 5;\n}\nxs[i] = bump();\nconsole.log(xs, i);\n"
	matchesNode(t, buildProgram(t, "order", src), "[ 5, 0 ] 1\n", 20)
}

// TestForOfEvaluatesItsArrayOnce: JavaScript evaluates a for…of's iterable
// once, so rebinding the variable it named, inside the loop, changes nothing
// about the iteration, and a call there runs once. Node 24.18 prints the
// want, 5 of 5 runs.
func TestForOfEvaluatesItsArrayOnce(t *testing.T) {
	t.Parallel()

	const src = "let cur: number[] = [1, 2, 3];\nfor (const x of cur) {\n  cur = [10, 20, 30, 40];\n  console.log(x);\n}\nlet calls: number = 0;\nfunction make(): number[] {\n  calls++;\n  return [7, 8];\n}\nfor (const y of make()) {\n  console.log(y, calls);\n}\n"
	matchesNode(t, buildProgram(t, "once", src), "1\n2\n3\n7 1\n8 1\n", 20)
}
