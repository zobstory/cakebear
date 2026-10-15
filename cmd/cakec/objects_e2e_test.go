package main

import (
	"strings"
	"testing"
)

// objectsProgram covers M4 step 5:
//   - key order per literal, Point and Vec as one shape, === as identity, a
//     console.log of several values;
//   - quoted keys and nested objects, printed as Node prints them;
//   - fields read and written through aliases and functions, and ++ and
//     compound assignment on fields;
//   - check 7: `counter.n + bump()` reads n before bump changes it, and a
//     literal's fields are evaluated in source order.
//
// Node 24.18 ran this same file and printed nodeObjects, the same in 5 of 5
// runs.
const objectsProgram = `interface Point { x: number; y: number }
type Vec = { y: number; x: number };
interface Labelled { name: string; at: Point; tags: { kind: string; "content-type": string; $id: number } }

const p: Point = { x: 1, y: 2 };
const q: Point = { y: 4, x: 3 };
const v: Vec = q;
console.log(p);
console.log(q, v);
const twin: Point = { x: 1, y: 2 };
console.log("same object:", q === v, "equal fields, other object:", p === twin, p !== twin);

const l: Labelled = { name: "it's", at: p, tags: { kind: "pin", "content-type": "text/plain", $id: -0 } };
console.log(l);

function moved(pt: Point, dx: number): Point {
  return { x: pt.x + dx, y: pt.y };
}
function shift(pt: Point): void {
  pt.x = pt.x + 10;
}
const r: Point = moved(p, 5);
shift(p);
console.log(p, r, l.at.x);

const alias: Point = r;
alias.y = 20;
alias.y++;
alias.x *= 2;
console.log(r);

const counter: { n: number; label: string } = { n: 0, label: "c" };
const bump = (): number => {
  counter.n = counter.n + 100;
  return 1;
};
const sum: number = counter.n + bump();
const lit: { a: number; b: number; c: number } = { a: counter.n, b: bump(), c: counter.n };
counter.label += "!";
console.log(sum, lit, counter);

const deep: { a: { b: { c: { d: number } } } } = { a: { b: { c: { d: 1 } } } };
console.log(deep);
const long: { first: string; second: string; third: string; fourth: string } = { first: "alpha alpha alpha", second: "beta beta beta beta", third: "gamma gamma gamma", fourth: "delta" };
console.log(long);
`

const nodeObjects = "{ x: 1, y: 2 }\n{ y: 4, x: 3 } { y: 4, x: 3 }\nsame object: true equal fields, other object: false true\n{\n  name: \"it's\",\n  at: { x: 1, y: 2 },\n  tags: { kind: 'pin', 'content-type': 'text/plain', '$id': -0 }\n}\n{ x: 11, y: 2 } { x: 6, y: 2 } 11\n{ x: 12, y: 21 }\n1 { a: 100, b: 1, c: 200 } { n: 200, label: 'c!' }\n{ a: { b: { c: [Object] } } }\n{\n  first: 'alpha alpha alpha',\n  second: 'beta beta beta beta',\n  third: 'gamma gamma gamma',\n  fourth: 'delta'\n}\n"

func TestObjectsMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "objects", objectsProgram), nodeObjects, 20)
}

// TestAggregateSharedStateRefused (check 6, plan D10): a request handler may
// use only the objects it creates, and the refusal says so through cakec.
func TestAggregateSharedStateRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeProject(t, dir, moduleProject(map[string]string{
		"main.ts": "import { createServer } from \"node:http\";\n\ninterface Page { body: string }\nconst home: Page = { body: \"hi\" };\n" +
			"createServer((req, res) => {\n  res.end(home.body);\n}).listen(3000);\n",
	}))
	code, stderr := projectBuild(t, dir, buildOptions{})
	want := "main.ts:6:11: cannot compile yet: this handler runs on many requests at once, so it can't use `home`, an object created outside it; an object can be shared between requests only if it is frozen (a const, readonly all the way down, built from literals), and this one isn't: its type isn't readonly all the way down: property `body` isn't readonly"
	if code != exitErrors || !strings.Contains(stderr, want) {
		t.Errorf("exit %d, want %d with\n%s\ngot:\n%s", code, exitErrors, want, stderr)
	}
}
