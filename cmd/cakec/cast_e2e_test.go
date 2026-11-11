package main

import (
	"os/exec"
	"strings"
	"testing"
)

// S1 step 3 (plan D4 (a), (c)): the checked cast. JSON.parse's and
// res.json()'s `any` filling a data type's slot, and `u as T` of an unknown,
// decode into T's shape. Where the value fits, Node 24.18 printed the same
// (castPrograms join the fetch goldens). Where it doesn't, Node carries on
// with it and cakebear throws a TypeError naming the path, an accepted
// difference, held by TestCastMismatches.

func init() {
	for name, src := range castPrograms {
		fetchPrograms[name] = src
	}
}

const castTypes = `interface Dims { w: number; h: number; unit: string }
interface Item { id: number; name: string; price: number; sale: number | null; tags: string[]; dims: Dims; note?: string }
interface Page { status: string; items: Item[] }
`

var castPrograms = map[string]string{
	// A typed slot and `as`, keys the shape doesn't name kept in place,
	// an optional property written after, the same object through a double
	// cast, and an Error out of a catch.
	"casts": castTypes + `const text: string = '{"status":"ok","count":2,"items":[{"id":1,"name":"a","price":2.5,"sale":null,"tags":["x"],"dims":{"w":1,"h":2,"unit":"cm"},"extra":true},{"name":"b","id":2,"price":3,"sale":1.5,"tags":[],"dims":{"unit":"in","w":3,"h":4},"note":"n"}]}';
const page: Page = JSON.parse(text);
console.log("items", page.items.length, page.items[0].name, page.items[1].sale, page.items[1].note, page.items[0].note);
console.log(page);
console.log(JSON.stringify(page));
const u: unknown = JSON.parse(text);
const p2 = u as Page;
console.log("cast", p2.status, p2.items[1].dims.unit);
page.items[0].note = "added";
console.log(page.items[0]);
const again = page as unknown as Page;
console.log("same", again === page);
try {
  JSON.parse("{bad");
} catch (e) {
  const err = e as Error;
  console.log("caught", err.name, err.message);
}
const list: number[] = JSON.parse("[1, 2, 3]");
const maybe: string | null = JSON.parse("null");
const flag = JSON.parse("true") as boolean;
console.log("primitives", list, maybe, flag);
const meta = JSON.parse('{"a": [1, {"b": null}]}') as { a: unknown };
console.log("unknown", meta.a);
export {};
`,
	// res.json() into a typed slot, cast, and returned from an async
	// function whose result is the type.
	"casts-fetch": castTypes + `async function load(body: string): Promise<Page> {
  const r = await fetch("BASE/data?body=" + encodeURIComponent(body));
  return await r.json();
}
const page = await load('{"status":"ok","items":[{"id":7,"name":"z","price":1,"sale":null,"tags":["t"],"dims":{"w":1,"h":1,"unit":"m"}}]}');
console.log("loaded", page.items[0].name, page.items[0]);
const r2 = await fetch("BASE/data?body=" + encodeURIComponent('{"status":"also","items":[]}'));
const p2 = (await r2.json()) as Page;
console.log("as", p2.status, p2.items.length);
export {};
`,
}

// TestCastMismatches: a value that doesn't fit its cast throws a TypeError
// naming the path, what it is and what the type wants there, at the cast;
// catchable, and reported as an uncaught throw where nothing catches it.
func TestCastMismatches(t *testing.T) {
	t.Parallel()
	cases := []struct{ json, want string }{
		{`{"status":"ok","items":[{"id":"x"}]}`, `$.items[0].id is a string, where the type Page wants a number`},
		{`{"status":"ok"}`, `$.items is missing, where the type Page wants an array`},
		{`{"status":null,"items":[]}`, `$.status is null, where the type Page wants a string`},
		{`{"status":"ok","items":[{"id":1,"name":"a","price":1,"sale":"no","tags":[],"dims":{"w":1,"h":1,"unit":"m"}}]}`, `$.items[0].sale is a string, where the type Page wants a number`},
		{`{"status":"ok","items":[{"id":1,"name":"a","price":1,"sale":null,"tags":[3],"dims":{"w":1,"h":1,"unit":"m"}}]}`, `$.items[0].tags[0] is a number, where the type Page wants a string`},
		{`{"status":"ok","items":{}}`, `$.items is an object, where the type Page wants an array`},
		{`[]`, `$ is an array, where the type Page wants an object`},
	}
	var src strings.Builder
	src.WriteString(castTypes)
	for _, c := range cases {
		src.WriteString("try {\n  const p: Page = JSON.parse(" + jsString(c.json) + ");\n  console.log(\"fit\", p.status);\n} catch (e) {\n  console.log(\"caught\", e instanceof TypeError, String(e));\n}\n")
	}
	src.WriteString("const p = JSON.parse('{\"status\":\"ok\",\"items\":5}') as Page;\nconsole.log(\"never\", p.status);\nexport {};\n")
	bin := buildProgram(t, "mismatch", src.String())
	stdout, stderr, code := runOutput(exec.Command(bin))
	var want strings.Builder
	for _, c := range cases {
		want.WriteString("caught true TypeError: " + c.want + "\n")
	}
	if stdout != want.String() || code != 1 {
		t.Fatalf("exit %d, printed\n%s\nwant exit 1 and\n%s", code, stdout, want.String())
	}
	// The line as Node strips its types, `as Page` blanked, and the caret at
	// the cast's value.
	if !strings.Contains(stderr, "const p = JSON.parse('{\"status\":\"ok\",\"items\":5}')        ;\n          ^\n\n[TypeError: $.items is a number, where the type Page wants an array]") {
		t.Errorf("the uncaught report doesn't point at the cast, or says otherwise:\n%s", stderr)
	}
}

// jsString is s as a single-quoted JavaScript string literal.
func jsString(s string) string { return "'" + strings.ReplaceAll(s, "'", "\\'") + "'" }
