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

cakebear is a **fork of [microsoft/typescript-go][tsgo]**, Microsoft's Go port
of the TypeScript compiler. The scanner, parser, binder and type checker are
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

[tsgo]: https://github.com/microsoft/typescript-go

## Usage

```console
cakec build main.ts                  # type-check, compile, link
cakec build --check main.ts          # type-check only
cakec build --emit-go main.ts        # also write the generated Go
cakec build --target linux/amd64 main.ts
cakec build --checkers 8 --files-from list.txt
```

Building requires the Go toolchain on `PATH`; cakebear shells out to it.

## Types cakebear adds

TypeScript's `number` is a float64, so integer-heavy code pays for it. cakebear
adds machine-width numeric types, plus a validated `base64`:

```ts
const small: i32 = i32(42);              // lowers to Go's int32
const big: i64 = i64(9007199254740991);  // int64, exact past 2^53
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

## The language subset

Supported today: `number`, `string`, `boolean` and the extension types above;
`const`/`let` with explicit annotations; function declarations with typed
parameters and return; arithmetic; string concatenation; comparisons; `===` and
`!==`; `&&`, `||`, `!`; unary minus; `if`/`else`; `while`; `return`;
`console.log` of one primitive; calls to top-level functions.

Not yet: `null` and `undefined` as values, mixed-type `+`, `var`, loose
equality, arrays, objects, classes, generics, imports, `for`, multi-file
programs, bitwise operators, `async`.

Programs are compared against Node for behavioural equivalence, down to
`console.log(-0)` and `1e-7`.

## Contributing

The repository is 99% Microsoft's code. **Never modify an upstream `.go` file** —
every cakebear behaviour goes in a new file under a path listed in
[`scripts/owned-paths`](scripts/owned-paths), and CI enforces it. A fork's
maintenance cost is proportional to its diff against upstream, and upstream is
actively developed.

[`CLAUDE.md`](CLAUDE.md) is the working source of truth: architecture,
decisions, guardrails, and what phase the project is in.

## License

Apache-2.0, inherited from `microsoft/typescript-go`. See [`LICENSE`](LICENSE)
and [`NOTICE.txt`](NOTICE.txt).
