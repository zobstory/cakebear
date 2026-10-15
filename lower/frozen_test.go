package lower

import (
	"strings"
	"testing"
)

// frozenTypes declares a route table's types, readonly all the way down, and
// a mutable twin of a route for the flow refusals.
const frozenTypes = `interface Page { readonly title: string; readonly items: readonly string[] }
interface Route { readonly path: string; readonly page: Page }
interface MutRoute { path: string; page: Page }
`

// TestFrozenConstsAreShared (D10b): a const readonly all the way down and
// built from literals may be read by a request handler or a spawned
// function, directly or through what it calls, and so may a frozen const it
// holds, a name destructured from one, a Readonly<T>, and one imported from
// another module.
func TestFrozenConstsAreShared(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, src string }{
		{"a route table in a handler", `const home: Page = { title: "home", items: ["a"] };
const routes: readonly Route[] = [{ path: "/", page: home }, { path: "/x", page: { title: "x", items: [] } }];
createServer((req, res) => {
  for (const r of routes) {
    if (r.path === req.url) {
      res.end(JSON.stringify(r.page));
    }
  }
}).listen(3000);
`},
		{"through a function the handler calls", `const routes: readonly Route[] = [{ path: "/", page: { title: "t", items: [] } }];
function first(): string {
  return routes[0].path;
}
createServer((req, res) => {
  res.end(first());
}).listen(3000);
`},
		{"a destructured name in a loop", `const routes: readonly Route[] = [{ path: "/", page: { title: "t", items: [] } }];
createServer((req, res) => {
  for (const { path, page } of routes) {
    if (path === req.url) {
      res.end(page.title);
    }
  }
}).listen(3000);
`},
		{"a destructured name", `const site: { readonly pages: readonly Page[]; readonly name: string } = { pages: [], name: "s" };
const { pages } = site;
createServer((req, res) => {
  res.end(JSON.stringify(pages));
}).listen(3000);
`},
		{"Readonly<T>", `interface Conf { port: number; host: string }
const conf: Readonly<Conf> = { port: 1, host: "h" };
createServer((req, res) => {
  res.end(conf.host);
}).listen(3000);
`},
		{"in a spawned function", `const routes: readonly Route[] = [{ path: "/", page: { title: "t", items: [] } }];
async function main(): Promise<void> {
  const n: number = await spawn((): number => routes.length);
  console.log(n);
}
main();
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, errs := lowerSource(t, raceImport+frozenTypes+tt.src); len(errs) > 0 {
				t.Errorf("errors: %v", joinMsgs(errs))
			}
		})
	}

	p, errs := lowerFiles(t, map[string]string{
		"routes.ts": "export interface Route { readonly path: string; readonly body: string }\n" +
			"export const routes: readonly Route[] = [{ path: \"/\", body: \"home\" }];\n",
		"main.ts": raceImport + "import { routes } from \"./routes\";\ncreateServer((req, res) => {\n  res.end(routes[0].body);\n}).listen(3000);\n",
	}, "main.ts")
	if len(errs) > 0 {
		t.Errorf("an imported frozen const: errors: %v", joinMsgs(errs))
	} else if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestNotFrozen: each way a const fails to be frozen is named in the race
// rule's refusal, so the fix is in the message.
func TestNotFrozen(t *testing.T) {
	t.Parallel()

	const use = "createServer((req, res) => {\n  res.end(JSON.stringify(routes));\n}).listen(3000);\n"
	const shared = "can't use `routes`, an array created outside it; an array can be shared between requests only if it is frozen (a const, readonly all the way down, built from literals), and this one isn't: "
	for _, tt := range []struct{ name, src, want string }{
		{"a mutable property", "interface Loose { path: string }\nconst routes: readonly Loose[] = [{ path: \"/\" }];\n", "its type isn't readonly all the way down: property `path` isn't readonly"},
		{"a mutable array", "const routes: Route[] = [];\n", "a Route[] is mutable; write `readonly Route[]`"},
		{"a mutable array, an", "interface Item { readonly id: number }\nconst routes: Item[] = [];\n", "an Item[] is mutable; write `readonly Item[]`"},
		{"a mutable array inside", "interface Deep { readonly tags: string[] }\nconst routes: readonly Deep[] = [];\n", "a string[] is mutable; write `readonly string[]`"},
		{"a function inside", "interface H { readonly f: () => number }\nconst routes: readonly H[] = [];\n", "it holds a function, whose captures can change"},
		{"a call", "function make(): readonly Route[] {\n  return [];\n}\nconst routes: readonly Route[] = make();\n", "it holds a value from a call expression, which something else may hold too; build it from literals"},
		{"a const that isn't frozen", "function page(): Page {\n  return { title: \"t\", items: [] };\n}\nconst p: Page = page();\nconst routes: readonly Route[] = [{ path: \"/\", page: p }];\n",
			"it holds `p`, which isn't frozen: it holds a value from a call expression"},
		{"a shorthand of one that isn't", "function page(): Page {\n  return { title: \"t\", items: [] };\n}\nconst page1: Page = page();\nconst routes: readonly { readonly page1: Page }[] = [{ page1 }];\n",
			"it holds `page1`, which isn't frozen"},
		{"behind an as", "const mut: Route[] = [];\nconst routes: readonly Route[] = mut as readonly Route[];\n", "it holds `mut`, which isn't frozen: its type isn't readonly all the way down"},
		{"a let", "let routes: readonly Route[] = [];\n", "it is a let"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, raceImport+frozenTypes+tt.src+use)
			if msgs := joinMsgs(errs); !strings.Contains(msgs, shared+tt.want) && !strings.Contains(msgs, tt.want) || !strings.Contains(msgs, "can't use `routes`") {
				t.Errorf("errors = %q, want the refusal of `routes`, saying %q", msgs, tt.want)
			}
		})
	}
}

// TestReadonlyIntoMutableRefused: a value of a readonly type can't fill a
// mutable slot, which TypeScript allows for properties, wherever the value
// flows: a variable, an argument, a result, a field, an element, a push, a
// shorthand, and a callback, written in place or named.
func TestReadonlyIntoMutableRefused(t *testing.T) {
	t.Parallel()

	const routes = "const routes: readonly Route[] = [{ path: \"/\", page: { title: \"t\", items: [] } }];\n"
	const want = "`path` is readonly in Route and not in MutRoute; cakebear refuses a readonly value where a mutable one is wanted"
	for _, tt := range []struct{ name, src string }{
		{"a variable", "const m: MutRoute = routes[0];\n"},
		{"an argument", "function rename(r: MutRoute): void {\n  r.path = \"/y\";\n}\nrename(routes[0]);\n"},
		{"a result", "function first(): MutRoute {\n  return routes[0];\n}\n"},
		{"a field", "const box: { r: MutRoute } = { r: routes[0] };\n"},
		{"an element", "const ms: MutRoute[] = [routes[0]];\n"},
		{"a push", "const ms: MutRoute[] = [];\nms.push(routes[0]);\n"},
		{"a shorthand", "const r: Route = routes[0];\nconst box: { r: MutRoute } = { r };\n"},
		{"a callback in place", "const paths: string[] = routes.map((r: MutRoute): string => r.path);\n"},
		{"a named callback", "function pathOf(r: MutRoute): string {\n  return r.path;\n}\nconst paths: string[] = routes.map(pathOf);\n"},
		{"an array of them", "const ms: readonly MutRoute[] = routes;\n"},
		{"a function's result", "const f: () => MutRoute = (): Route => routes[0];\n"},
		{"a promise of one", "async function get(): Promise<Route> {\n  return routes[0];\n}\nconst p: Promise<MutRoute> = get();\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, errs := lowerSource(t, frozenTypes+routes+tt.src)
			if msgs := joinMsgs(errs); !strings.Contains(msgs, want) {
				t.Errorf("errors = %q, want one containing %q", msgs, want)
			}
		})
	}

	// The other way, a mutable value into a readonly slot, is fine, as it is
	// in TypeScript: nothing frozen is involved.
	lowerClean(t, frozenTypes+"const m: MutRoute = { path: \"/\", page: { title: \"t\", items: [] } };\nconst r: Route = m;\nfunction show(x: Route): string {\n  return x.path;\n}\nconsole.log(show(m));\nconsole.log(r.path);\n")
}
