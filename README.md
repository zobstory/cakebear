# cakebear

A compiler that takes **TypeScript** and produces a **single native binary**.

```console
$ cat main.ts
function add(a: number, b: number): number {
  return a + b;
}
const x: number = add(2, 3);
console.log(x);

$ cakec build main.ts
cakec: main.ts type-checks clean
cakec: built main

$ ./main
5
```

No Node, no V8, no bundler. The output is a statically linked executable.

> **Status: early.** The compiler type-checks all of TypeScript but can only
> *lower* a small subset of it — see [The language subset](#the-language-subset).
> Anything outside that subset is refused with a diagnostic naming the
> construct, not miscompiled.

## Why

TypeScript is one of the easiest mainstream languages to pick up and one of the
most widely known. Go and Rust offer dramatically better runtime performance and
a far simpler deployment story: one binary, no runtime install. cakebear aims to
give TypeScript developers that deployment and performance story without asking
them to learn a second language first.

If that lowers the barrier for people entering the field without a systems
background, the project has done its job.

## How it works

cakebear is a **fork of [Microsoft's Go TypeScript compiler][tsc]**, the
`tsc/` directory of microsoft/TypeScript (formerly microsoft/typescript-go). The
scanner, parser, binder and type checker are
theirs — a complete, conformance-tested implementation of a type system that
would otherwise take years to write. Where upstream emits JavaScript, cakebear
lowers to Go source and invokes the Go toolchain:

```
source (.ts)
  → scanner → parser → binder → checker     upstream
  → ir                                      lowered, backend-agnostic
  → backend  → Go source → `go build`       native binary
```

Emitting Go rather than machine code is a deliberate trade. It hands the project
a production-hardened garbage collector, an M:N scheduler ready for
`async`/`await`, static linking and cross-compilation on day one — and caps
performance at roughly Go's rather than beating it. The IR is kept
backend-agnostic so a native backend can replace the emitter later without the
frontend noticing.

Type checking inherits upstream's parallelism: a goroutine per file for parsing,
and a pool of checkers (`--checkers`, default 4) over a shared immutable AST.

[tsc]: https://github.com/microsoft/TypeScript/tree/main/tsc

## Usage

```console
cakec build main.ts                  # type-check, compile, link
cakec build --check main.ts          # type-check only
cakec build --emit-go main.ts        # also write the generated Go
cakec build --target linux/amd64 main.ts
cakec build --checkers 8 --files-from list.txt
cakec build                          # the project in the nearest tsconfig.json
cakec build -p app/                  # the project in app/tsconfig.json
cakec build --help                   # cakebear's flags
cakec -p .                           # anything but `cakec build` is tsc, unchanged
```

Building requires the Go toolchain on `PATH`; cakebear shells out to it.

### `cakec` is `tsc`

Anything but `cakec build` is upstream's `tsc`, unchanged: every flag,
`tsconfig.json`, `--watch`, `--build` and the rest. A test holds the two
byte for byte over sample projects: output, exit codes and every file
written. So `cakec --help` and `cakec --version` are `tsc`'s, and cakebear's
own are `cakec build --help` and `cakec build --version`. Mind the dash:
`cakec build` makes a native binary, while `cakec --build` (or `-b`) is
TypeScript's build of project references.

`cakec --lsp --stdio` is TypeScript's language server, with cakebear's own
names (`spawn`, `i32`, `node:http`) known in every project, so an editor that
runs it stops reporting them as unknown.

### Projects

A project build uses the `tsconfig.json`'s files and compiler options. The
module the binary runs goes in a top-level `"cakebear"` block, which `tsc`
ignores. Paths in it are relative to the config, and `output` defaults to
`main`'s basename:

```jsonc
{
  "compilerOptions": { "strict": true },
  "include": ["src"],
  "cakebear": { "main": "src/server.ts", "output": "bin/server" }
}
```

The program is `main` and every module it imports, by a relative path,
compiled into one binary. Each module's top-level code runs in the order Node
runs an ES module graph, cycles included.
[`examples/http-project`](examples/http-project) is a server in three
modules. Its imports name their `.ts` files, so Node runs the same sources:

```console
$ cakec build -p examples/http-project && examples/http-project/bin/server
$ node examples/http-project/src/main.ts      # the same server, under Node
```

With a `"cakebear"` block, `cakec` in `tsc` mode (`cakec -p .`, `--watch`,
`--build`) knows cakebear's own names too. On a project without one, it is
exactly `tsc`.

## Types cakebear adds

TypeScript's `number` is a float64, so integer-heavy code pays for it. cakebear
adds machine-width numeric types, plus a validated `base64`:

```ts
const small: i32 = i32(42);              // lowers to Go's int32
const big: i64 = i64(9007199254740991);  // int64, exact past 2^53
const sum: i32 = i32(small + small);     // int32 arithmetic, wrapping as `| 0` does
const half: number = small / i32(4);     // outside a conversion, a number's: 10.5
const enc: base64 = base64("Y2FrZWJlYXI=");

const bad: i32 = i32(3000000000);        // compile error: outside the range of i32
const nope: base64 = base64("!!!");      // compile error: not valid base64
```

These are branded types — an intersection of the primitive with a phantom
property — so TypeScript's own checker enforces them and cakebear needed no
change to the type checker to add them. Literal arguments are validated when the
program is compiled, which is the part a plain `number` or `string` could not do.

Per the project's own rule, any of these is retired the moment TypeScript ships
an equivalent.

## A server

```ts
import { createServer } from "node:http";

const server = createServer((req, res) => {
  res.setHeader("Content-Type", "text/plain");
  if (req.url === "/health") {
    res.end("ok\n");
  } else {
    res.end("hello from cakebear\n");
  }
});

server.listen(3000, () => {
  console.log("listening on http://localhost:3000");
});
```

`cakec build` turns this into one binary that serves HTTP, and the same file
runs unmodified under Node (it is
[`examples/http-hello/main.ts`](examples/http-hello/main.ts)). `node:http`'s
declarations ship with `cakec`, so there is no npm install and no
`@types/node`. They declare only what the runtime implements, so a missing API
such as `res.flushHeaders()` is a type error at the call site.

The difference from Node: every request runs on its own goroutine, genuinely in
parallel, so a CPU-heavy handler doesn't hold up the others. Eight requests to
a handler computing `fib(38)` take about 1.3× as long as one; under Node they
take about 7.7×, because its handlers share one thread.

Handlers can be `async`. Each request is its own *domain*: the awaits inside
one request run in exactly Node's order, while separate requests still run in
parallel. Eight requests to a handler that awaits a timer and then computes
`fib(38)` take about 1.2× as long as one; under Node they take about 7.2×.

```ts
createServer(async (req, res) => {
  await sleep(10); // a promise resolved by setTimeout
  res.end(`fib: ${fib(38)}\n`);
});
```

Because handlers overlap, one that changes shared state would be a data race.
`cakec` refuses it at compile time, including after an `await`:

```console
server.ts:10:3: cannot compile yet: this handler runs on many requests at once, so it can't change `hits`, which they all share
server.ts:11:3: cannot compile yet: this handler runs on many requests at once, so it can't call `bump()`, which changes `hits`, shared by them all
```

## A JSON API

[`examples/route-server`](examples/route-server) is a small JSON API in four
modules: a menu, query parsing, routes and the server. Every handler reads
one route table, declared once at the top level. An item may have a note, and
a sale price:

```ts
export interface Item {
  readonly id: number;
  readonly name: string;
  readonly price: number;
  readonly tags: readonly string[];
  readonly note?: string;
  readonly sale?: number | null;
}

export const items: readonly Item[] = [
  { id: 1, name: "Green tea", price: 4.5, tags: ["hot", "tea"], note: "loose leaf" },
  { id: 2, name: "Straße cake", price: 2.25, tags: ["sweet"], sale: 1.75 },
  // …
];
```

The API shows an item at its sale price, with the full one as `was`, and its
note only where it has one:

```ts
function summary(it: Item): Summary {
  const sale: number | null | undefined = it.sale;
  return {
    id: it.id,
    name: it.name,
    price: sale ?? it.price,
    ...(it.note ? { note: it.note } : {}),
    ...(sale != null && { was: it.price }),
  };
}
```

```console
$ cakec build -p examples/route-server && examples/route-server/bin/server
listening on http://localhost:3000
$ curl 'localhost:3000/items?tag=hot'
[{"id":1,"name":"Green tea","price":4.5,"note":"loose leaf"},{"id":4,"name":"Chai 😀","price":4}]
$ curl localhost:3000/items/2
{
  "id": 2,
  "name": "Straße cake",
  "price": 1.75,
  "was": 2.25
}
$ curl localhost:3000/menu
Green tea      $4.50 (loose leaf)
Straße cake    $1.75
…
```

The same sources run unmodified under Node, and answer every request the same
way, byte for byte: status, `Content-Type` and body
(`TestRouteServerServes`).

Requests run in parallel, so a handler may only read state nothing can
change. A `const` whose type is `readonly` all the way down, and which is built
from literals, qualifies: it is *frozen*. Make `items` a plain `Item[]` and
the build is refused, through the function the handler calls, with the fix:

```console
src/main.ts:23:24: cannot compile yet: this handler runs on many requests at once, so it can't call `route()`, which uses `items` in data.ts, an array created outside the handler, which isn't frozen: its type isn't readonly all the way down: an Item[] is mutable; write `readonly Item[]`
```

## Parallel work: `spawn`

`spawn` is Go's `go` for TypeScript. It runs a function on its own goroutine,
in parallel with the caller, and returns a promise of its result. It is a
global, like `setTimeout`, so there is nothing to import:

```ts
const a: Promise<number> = spawn(() => fib(38));
const b: Promise<number> = spawn(() => fib(38));
console.log((await a) + (await b));
```

Four spawned `fib(38)` calls take about 1.1× as long as one. Node has no
equivalent short of worker threads. The spawned function is held to the same
rule as a request handler: it can't touch a `let`, a promise or a request from
outside it, so the two can't race. `spawn` is cakebear's, not TypeScript's, so
plain `tsc` reports it as an unknown name.

## The language subset

Supported today:
- `number`, `string`, `boolean` and the extension types above; `const` and
  `let`; function declarations with typed parameters and return; arithmetic
  (`%` included),
  comparisons, `===` and `!==`, `&&`, `||`, `!`, unary minus; `if`/`else`,
  `while`, `for (;;)`, `for…of`, `break` and `continue`; `++`, `--` and the
  compound assignments as statements; `interface` and `type` declarations;
- `console.log` of any number of values, printed as Node prints them; calls
  to a function by name; arrow functions and function expressions as values;
- **objects** of interfaces and object types with required properties, and
  **arrays**: literals, fields, `xs[i]`, `length`, `push`, `pop`, and `map`,
  `filter`, `join`, `includes`, `indexOf`, `slice`, `sort`, `some`, `every`,
  `forEach`, `reduce` and `findIndex`; shorthand properties and object
  destructuring; `JSON.stringify`, byte for byte as Node writes it; frozen
  consts a request handler may read;
- **strings**: template literals, `String()`, `toString()`, `toFixed()`,
  `length`, and the common methods (`includes`, `startsWith`, `split`,
  `slice`, `replace` with a string pattern, `toUpperCase`, `trim`, `padStart`,
  …), all counting in UTF-16 units as JavaScript does; `Number()`,
  `parseInt`, `parseFloat`, `isNaN` and the URI functions;
- **`null` and `undefined`**: `T | undefined` and `T | null` of any of the
  above, optional properties, `?:`, `??` and `??=`, optional chains, `!`,
  narrowing wherever the checker narrows, any value tested for truthiness,
  `||` and `&&` on values, conditional spread, `delete` and `in`, `find`,
  `at` and `pop()` as values, optional and default parameters, and unions of
  literals such as `"get" | "post"`;
- `import` from `node:http`, and the members its bundled declarations list;
  `async` functions and arrows, `await`, including at the top level of a
  module, `Promise<T>`, `new Promise((resolve) => …)`, `setTimeout` /
  `clearTimeout`, and `spawn`;
- programs of several modules, with named imports and exports of top-level
  functions and `const`s, renamed or not, and import cycles.

Not yet: `throw`, `try` and `reject` (next, M6); other unions, classes,
generics, RegExp, `URL`, mixed-type `+` (a template literal does it), `var`,
loose equality but `== null`, imports from anything but `node:http` and the
program's own modules, default exports, `import * as` a module of your own,
re-exports, exporting a `let`, rest parameters, bitwise operators, `.then`
and `Promise.all`, and the standard library's other globals (`Math`, `Date`,
`setInterval`, …).

Programs are compared against Node for behavioural equivalence, down to
`console.log(-0)` and `1e-7`, with one deliberate exception: request handlers
run in parallel, where Node runs them one at a time. Async code within one
request, or within the main program, keeps Node's order exactly. The compiler refuses
shared mutable state in a handler, so that difference cannot become a data
race. [`CLAUDE.md`](CLAUDE.md) lists the other accepted differences, such as log
lines interleaving across requests and response header names being
canonicalised.

## Contributing

The repository is 99% Microsoft's code. **Never modify an upstream `.go` file** —
every cakebear behaviour goes in a new file under a path listed in
[`scripts/owned-paths`](scripts/owned-paths), and CI enforces it. A fork's
maintenance cost is proportional to its diff against upstream, and upstream is
actively developed.

[`CLAUDE.md`](CLAUDE.md) is the working source of truth: architecture,
decisions, guardrails, and what phase the project is in.

## License

Apache-2.0, inherited from `microsoft/TypeScript`. See [`LICENSE`](LICENSE)
and [`NOTICE.txt`](NOTICE.txt).
