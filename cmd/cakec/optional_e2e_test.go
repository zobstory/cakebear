package main

import "testing"

// optionalProgram covers M5 step 5, optional properties:
//   - literals leaving one out, setting one, and setting one to undefined,
//     which makes it present: Node prints it, and JSON leaves it out;
//   - reads through ?? and ?., and `in`;
//   - a write adding the key last, delete removing it, and adding it again
//     putting it last again;
//   - a nested object's, through !, and an array of objects', in for…of;
//   - ??= and += on one;
//   - a frozen array of objects with a readonly optional property;
//   - a write whose value reads the object, before it has the key.
//
// Node 24.18 ran this same file and printed nodeOptional, the same in 5 of 5
// runs.
const optionalProgram = `interface Inner { id: number; note?: string }
interface Opts { name: string; limit?: number; tags?: string[]; inner?: Inner }

function describe(o: Opts): string {
  const limit: number = o.limit ?? 10;
  return ` + "`" + `${o.name}:${limit}:${o.tags?.length ?? 0}:${o.inner?.note ?? "-"}` + "`" + `;
}

const a: Opts = { name: "a" };
const b: Opts = { name: "b", limit: 3, tags: ["x", "y"] };
const c: Opts = { limit: undefined, name: "c", inner: { id: 1 } };
console.log(a, b, c);
console.log("json", JSON.stringify(a), JSON.stringify(b), JSON.stringify(c));
console.log("describe", describe(a), describe(b), describe(c));
console.log("in", "limit" in a, "limit" in b, "limit" in c, "name" in a);

a.tags = ["z"];
a.limit = 1;
console.log("added", a, JSON.stringify(a));
delete a.tags;
console.log("deleted", a, "tags" in a, a.tags);
a.tags = [];
console.log("re-added", a);
c.inner!.note = "hi";
console.log("nested", c, JSON.stringify(c));
b.limit = undefined;
console.log("undefined", b, JSON.stringify(b), "limit" in b);
b.limit ??= 7;
b.limit += 1;
console.log("assign", b.limit, b);

const list: Opts[] = [{ name: "p" }, { name: "q", limit: 2 }];
for (const o of list) {
  if (o.limit === undefined) {
    o.limit = 0;
  }
}
console.log("list", list);

interface Point { readonly x: number; readonly y?: number }
const frozen: readonly Point[] = [{ x: 1 }, { x: 2, y: 3 }];
console.log("frozen", frozen, frozen[0].y ?? -1);

const me: Opts = { name: "me" };
me.limit = JSON.stringify(me).length;
console.log("me", me);
`

const nodeOptional = `{ name: 'a' } { name: 'b', limit: 3, tags: [ 'x', 'y' ] } { limit: undefined, name: 'c', inner: { id: 1 } }
json {"name":"a"} {"name":"b","limit":3,"tags":["x","y"]} {"name":"c","inner":{"id":1}}
describe a:10:0:- b:3:2:- c:10:0:-
in false true true true
added { name: 'a', tags: [ 'z' ], limit: 1 } {"name":"a","tags":["z"],"limit":1}
deleted { name: 'a', limit: 1 } false undefined
re-added { name: 'a', limit: 1, tags: [] }
nested { limit: undefined, name: 'c', inner: { id: 1, note: 'hi' } } {"name":"c","inner":{"id":1,"note":"hi"}}
undefined { name: 'b', limit: undefined, tags: [ 'x', 'y' ] } {"name":"b","tags":["x","y"]} true
assign 8 { name: 'b', limit: 8, tags: [ 'x', 'y' ] }
list [ { name: 'p', limit: 0 }, { name: 'q', limit: 2 } ]
frozen [ { x: 1 }, { x: 2, y: 3 } ] -1
me { name: 'me', limit: 13 }
`

func TestOptionalPropertiesMatchNode(t *testing.T) {
	t.Parallel()
	matchesNode(t, buildProgram(t, "optional", optionalProgram), nodeOptional, 20)
}
