package lower

import (
	"strings"
	"testing"

	"github.com/zobstory/cakebear/ir"
)

// TestArrayLowering: literals typed by their slot, element reads and writes,
// the members step 6 lowers, and for…of.
func TestArrayLowering(t *testing.T) {
	t.Parallel()

	p := lowerProgramClean(t, `const xs: number[] = [1, 2];
const none: string[] = [];
xs[0] = 5;
xs[1]++;
const n: number = xs.length + xs.push(3, 4);
xs.pop();
xs.length = 1;
for (const x of xs) {
  console.log(x);
}
`)
	counts := map[string]int{}
	var loop *ir.ForOf
	ir.Inspect(p.EntryModule(), func(node any) {
		switch n := node.(type) {
		case *ir.ArrayLit:
			counts["literal "+n.Typ.String()]++
		case *ir.IndexSet:
			counts["set"]++
		case *ir.IndexGet:
			counts["get"]++
		case *ir.ArrayOp:
			counts[n.Method.String()]++
		case *ir.ForOf:
			loop = n
		}
	})
	for key, want := range map[string]int{"literal number[]": 1, "literal string[]": 1, "set": 2, "get": 1, "length": 1, "push": 1, "pop": 1, "length =": 1} {
		if counts[key] != want {
			t.Errorf("%d of %q, want %d (all: %v)", counts[key], key, want, counts)
		}
	}
	if loop == nil || loop.Name != "x" || loop.Type != ir.Number || loop.Mutable {
		t.Errorf("for…of = %+v, want a const x of number", loop)
	}
}

func TestArrayRefusals(t *testing.T) {
	t.Parallel()

	const xs = "const xs: number[] = [1, 2];\n"
	for _, tt := range []struct {
		name, src, want string
	}{
		{"a spread", xs + "const b: number[] = [...xs];\n", "spreading into an array literal is not supported yet"},
		{"a hole", "const a: number[] = [1, , 2];\n", "an array literal with a hole is not supported"},
		{"a tuple", "const t: [number, string] = [1, \"a\"];\n", "tuples are not supported yet"},
		{"elements of a union", "const u: (number | string)[] = [1];\n", "its elements: union types are not supported yet"},
		{"an empty literal with no slot", "console.log([]);\n", "an empty array literal needs its type from its slot"},
		{"for…of over a string", "for (const c of \"abc\") {\n}\n", "for…of over a string is not supported yet; only over an array"},
		{"for await", "async function f(ys: number[]): Promise<void> {\n  for await (const y of ys) {\n  }\n}\n", "for await is not supported yet"},
		{"for…of array destructuring", "const ps: number[][] = [[1]];\nfor (const [a] of ps) {\n}\n", "array destructuring is not supported yet"},
		{"for…of over an existing variable", xs + "let x: number = 0;\nfor (x of xs) {\n}\n", "a for…of loop needs its own const or let"},
		{"pop as a value", xs + "console.log(xs.pop());\n", "pop's value is an element or undefined, which waits for M5"},
		{"a method refused by name", xs + "const ys: number[] = xs.concat(xs);\n", "the array method concat is not supported yet"},
		{"push as a value", xs + "console.log(xs.push);\n", "an array's push can only be called"},
		{"element access on an object", "const o: { a: number } = { a: 1 };\nconst v: number = o[\"a\"];\n", "an element access on a { a: number } is not supported yet"},
		{"a string index", xs + "const k: string = \"0\";\nconsole.log(xs[k]);\n", "an array's index must be a number, not a string"},
		{"an update with a call for its index", xs + "function i(): number {\n  return 0;\n}\nxs[i()]++;\n", "an element indexed by a name or a number"},
		{"a member a program adds to Array", "interface Array<T> {\n  total(): number;\n}\n" + xs + "console.log(xs.total());\n", "total is added to Array outside TypeScript's libs, which is not supported"},
		{"an array shared with a handler", "import { createServer } from \"node:http\";\nconst routes: string[] = [\"/\"];\ncreateServer((req, res) => {\n  res.end(routes[0]);\n}).listen(3000);\n",
			"can't use `routes`, an array created outside it; an array can be shared between requests only if it is frozen (a const, readonly all the way down, built from literals), and this one isn't: its type isn't readonly all the way down: a string[] is mutable; write `readonly string[]`"},
	} {
		_, errs := lowerProgram(t, tt.src)
		if msgs := joinMsgs(errs); !strings.Contains(msgs, tt.want) {
			t.Errorf("%s: errors = %q, want one containing %q", tt.name, msgs, tt.want)
		}
	}
}
