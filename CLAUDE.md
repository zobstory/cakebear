# cakebear

A compiler that takes **TypeScript** and produces a **single native binary**.

cakebear is a permanent fork of the Go compiler in [microsoft/TypeScript][ts],
the `tsc/` directory of that repository. Their frontend — scanner, parser,
binder, checker — is ours by snapshot. Where upstream emits JavaScript, we lower
to Go and shell out to `go build`.

Upstream was [microsoft/typescript-go][tsgo] until Microsoft archived it in
August 2026 and moved the code into the main TypeScript repository. cakebear
was rebased onto `tsc/` in October 2026. That history was rewritten during the
move, so the two share no commits, and syncs are snapshots rather than merges
(see "Upstream sync").

> The plan and rationale for moving from the original Rust implementation to
> this fork: <https://claude.ai/code/artifact/485fa63c-f984-4648-87a8-bb0b9ddbb4aa>

[ts]: https://github.com/microsoft/TypeScript/tree/main/tsc
[tsgo]: https://github.com/microsoft/typescript-go

## The golden rule

**Never modify an upstream `.go` file.**

A fork's maintenance cost is directly proportional to its diff against
upstream, and upstream is actively developed. Every cakebear behaviour goes in
a new file under a path listed in `scripts/owned-paths`. In particular
`cmd/cakec` constructs its own `Program` from their packages and runs its own
emit — it never routes through `internal/execute`, precisely so that hooking
the pipeline requires no edit inside their tree.

Hold this line and a sync is mechanical: `scripts/sync-upstream.sh` replaces
every upstream file with the new snapshot and rewrites the module path, and
nothing of ours is lost because nothing of ours lives in their files. Break it
once and the next sync silently deletes the change. CI enforces it
(`golden-rule` job); the check is `scripts/check-upstream-untouched.sh`. It
compares committed history only, so it passes vacuously on uncommitted work,
and against a stale local `main` it counts files that are already merged. Run
it after committing, against `origin/main`.

If you genuinely need upstream behaviour to differ, that is a discussion before
it is a diff.

## Locked architectural decisions

- **Relationship to upstream:** hard fork, not a dependency. Forced by Go's
  `internal/` rule — everything we need lives under `internal/`, so it cannot
  be imported across modules, and upstream marks its public API as not ready.
  Inside the module it is all available.
- **Module path:** `github.com/zobstory/cakebear`. Rewritten from upstream's
  (`github.com/microsoft/TypeScript/tsc`) on every sync by
  `scripts/rename-module.sh`.
- **Go version:** whatever upstream's `go.mod` says, currently **1.27**. Go
  1.27 added generic methods and upstream uses them, so older toolchains, and
  older `gofmt` and `golangci-lint` binaries, fail to parse the tree.
- **License:** Apache-2.0, inherited. Keep `LICENSE` and `NOTICE.txt` intact.
- **Backend:** lower IR to **Go source**, then invoke `go build`. This buys GC,
  an M:N scheduler for `async`/`await`, static single-binary output and
  cross-compilation on day one — three roadmap items that would otherwise each
  be a project. The cost is a performance ceiling at roughly Go's, and a
  build-time dependency on the Go toolchain.
- **`ir/` is backend-agnostic.** It must not import `backend/`, and `backend/`
  must import nothing from the fork. That constraint is the escape hatch (swap
  the Go emitter for LLVM if beating Go ever matters) and what keeps a future
  extraction into a separate module a file move. `scripts/check-boundary.sh`
  enforces it and CI runs it.

## Repo layout

Upstream's tree, plus five directories upstream will never create:

```
cakebear/
├── internal/       UPSTREAM  scanner, parser, binder, checker, program, LSP
├── cmd/tsc/        UPSTREAM  their CLI — keep it working, it is a free
│                             regression check that a sync didn't break the frontend
├── testdata/       UPSTREAM  baselines and the conformance corpus (tests/cases/)
├── cmd/cakec/      OURS      the cakebear CLI
├── ir/             OURS      lowered IR: pure data, stdlib imports only
├── lower/          OURS      checked AST → ir; the only package seeing both worlds
├── backend/        OURS      IR → Go source → `go build` → executable
│   └── runtime/    OURS      linked into compiled output; lives here because
│                             go:embed cannot traverse ".."
├── types/          OURS      cakebear type extensions (base64, refined numerics)
└── scripts/        OURS      fork maintenance and CI gates
```

`ir/` and `backend/` sit at top level rather than under `internal/` on purpose:
that is what would make a future extraction into a separate module a file move
rather than a visibility rewrite:

- `ir/` imports only the standard library.
- `backend/` imports only `ir/` and the standard library.
- `lower/` absorbs all the coupling to the fork, and is the only package that
  sees both worlds.

`scripts/check-boundary.sh` enforces this, and CI runs it.

## Pipeline

```
source (.ts)
  → internal/scanner   UPSTREAM   tokens
  → internal/parser    UPSTREAM   ast
  → internal/binder    UPSTREAM   symbols
  → internal/checker   UPSTREAM   types + diagnostics
  → ir                 OURS       lowered, backend-agnostic
  → backend            OURS       Go source
  → go build           OURS       native binary
```

Parallelism comes with the frontend: parse runs a goroutine per file, and the
checker pool runs N checkers (default 4, `--checkers`) over a shared immutable
AST with per-checker type state. We inherit it rather than build it — the work
is exposing the knobs and keeping diagnostic output deterministic across worker
counts.

## Build / test / run

```sh
go build ./...                        # whole fork; ~1.5 min cold
go build -o bin/cakec ./cmd/cakec     # ours — always -o bin/, see below
go run ./cmd/tsc --version            # upstream CLI still works
bin/cakec build examples/basic/main.ts
bin/cakec build -o hello examples/http-hello/main.ts && ./hello   # a node:http server
bin/cakec build --checkers 8 --files-from list.txt   # large corpora
scripts/bench-checkers.sh <dir> <limit> 1 2 4 8      # scaling baseline

scripts/owned-go-packages.sh          # the packages that are ours
go test $(scripts/owned-go-packages.sh)

python3 scripts/check-line-limit.py           # ≤500 lines per owned .go file
scripts/check-upstream-untouched.sh origin/main  # the golden rule; commit first
scripts/check-upstream-snapshot.sh            # upstream tree == scripts/upstream-ref
scripts/check-boundary.sh                     # ir/ and backend/ stay liftable
scripts/check-workflows.sh                    # our workflow YAML actually parses
# Relative patterns, not owned-go-packages.sh: that prints import paths, which
# golangci-lint cannot resolve -- it reports "0 issues" having linted nothing.
# Needs v2.14.0 or later: a binary built with Go older than go.mod's refuses
# the module outright.
golangci-lint run --config .golangci.cakebear.yml \
  ./cmd/cakec/... ./lower/... ./types/... ./ir/... ./backend/...
```

**Always build binaries into `bin/`.** `/bin` is ignored. A bare
`go build ./cmd/cakec` or `./cmd/tsc` drops an executable at the repo root
instead. `.gitignore` covers `/cakec` and `/tsc` as a backstop, but `-o bin/`
keeps the root clean.

`testdata/tests/cases/` is upstream's conformance corpus: about 12,400 `.ts`
cases across `compiler/` and `conformance/`. It arrives with every sync now.
Under typescript-go it was a submodule that nobody cloned.

## Pull requests

- Assign zob on every PR (`gh pr create --assignee @me`).
- Merges into `main` are squash merges, and the head branch is deleted on
  merge. Both are repository settings, so GitHub enforces them. Upstream syncs
  squash like everything else: a snapshot carries no upstream parent to
  preserve.
- A sync PR carries the `upstream-sync` label. That label switches CI's
  golden-rule job from "no upstream file changed" to "every upstream file
  matches `scripts/upstream-ref`".
- Actions is disabled on this repository, so no checks run on a PR. Run the
  gates in "Build / test / run" locally and say in the PR what passed.

## Upstream sync

```sh
scripts/sync-upstream.sh [ref]        # snapshot microsoft/TypeScript@ref:tsc/ (default main)
scripts/check-upstream-snapshot.sh    # prove the tree still matches scripts/upstream-ref
```

A sync is a snapshot, not a merge. The script sparse-fetches `tsc/` at the
given ref and deletes every file not listed in `scripts/owned-paths`. It then
copies the snapshot in, plus the monorepo's root `NOTICE.txt`, rewrites the
module path and records the commit in `scripts/upstream-ref`. Finally it builds
and commits. The wholesale replace is correct only because of the golden rule.
If upstream ever ships a file at a path we own, the script stops and lists it
rather than choosing a side.

There is no merge because there is no shared history: typescript-go's commits
were rewritten when they moved under `tsc/`. A side effect is that `git log` on
an upstream file shows cakebear's sync commits, not upstream's authors. To find
when a change landed upstream, look in microsoft/TypeScript.

`check-upstream-snapshot.sh` re-fetches the recorded commit, applies the same
rename, and compares every upstream file by content. It reports missing, extra
and changed files separately, which makes it the review for a sync PR: a diff of
67,000 files cannot be read, but this check can be trusted.

A sync's real cost is upstream API churn in owned code, not conflicts. The
rebase hit three kinds. `ast.Symbol` fields became methods. File paths became
`tspath.RootedFilePath`, which reached the `vfs.FS` interface and
`GetSourceFile`. And the signatures of `NewCompilerHost`,
`NewParsedCommandLine` and `diagnosticwriter.FormattingOptions` changed.
`go build ./...` inside the sync is what surfaces this.

Lint uses `.golangci.cakebear.yml`. Upstream's `.golangci.yml` is at the
monorepo root, outside `tsc/`, and it needs `customlint`, a plugin that only
exists in a binary built from their `.custom-gcl.yml`. Ours is their linter set
minus the rules about their own codebase. The header lists them, and why
`modernize` is still off. CI pins golangci-lint to v2.14.0 so it matches what
gets run locally.

`.gitignore` and `.gitattributes` are cakebear's own and listed in
`scripts/owned-paths`. Upstream's are at the monorepo root, which no sync
copies. The manifest is still extensionless: upstream's old `.gitignore` had a
blanket `*.txt` rule, and when the manifest was `owned-paths.txt` it went
untracked for five phases. Run `git status --ignored` before assuming a new
file is committed.

`.github/workflows/cakebear-sync-upstream.yml` runs weekly. It compares
upstream's `main` to `scripts/upstream-ref`, syncs to that exact commit, and
opens a PR labelled `upstream-sync`. Keep the cadence. A week of API churn is a
small fix, and six months of it is a rebase.

## Parallelism baseline

Measured 16 Aug 2026 on Darwin arm64, 8 cores, via
`scripts/bench-checkers.sh`. Re-run on the same machine and corpus when
comparing; the ratio is the number that matters, not the seconds.

| Workload | 1 | 2 | 4 | 8 |
|---|---|---|---|---|
| A — 3000 real `.d.ts` from a `node_modules` tree | 1.00x | 1.45x | 2.19x | 2.24x |
| B — 491 `.ts` that actually type-check, 1588 errors | 1.00x | 1.09x | 1.44x | 1.55x |

Read these carefully rather than quoting the headline. Workload A is dominated
by parse, bind and module resolution: `SkipLibCheck` is on by default, so
declaration files are parsed but never checked. Workload B is checker-dominated
but small, so a fixed cost that does not parallelise — loading `lib.d.ts`,
building the program — eats a large share of the run.

Neither reaches Microsoft's ~3x, and neither should: their figure comes from
codebases orders of magnitude larger, where the fixed cost is noise. The
honest read is that parallelism is working and scaling stops paying past 4
checkers at these sizes.

The gap in this baseline is a genuinely large corpus of checkable `.ts`.
When this was measured, upstream's corpus was an uncloned submodule. Since the
rebase it is in the tree at `testdata/tests/cases/` (about 12,400 `.ts`), but
it has not been benchmarked yet, and many cases are deliberately erroneous or
multi-file, so it needs filtering first. Copying `.d.ts` files to `.ts` does
not work as a substitute: most of their content (ambient declarations,
bodyless overloads) is not legal in a `.ts` file, which is why workload B is
491 files rather than 3000. The table also predates the rebase onto
`microsoft/TypeScript`, so re-measure before comparing against it.

## Current phase: **M2, async/await**

P0–P5 and M1 are done, and **M2 is in progress**: `async`/`await`, `Promise`
and `setTimeout`, built on M1's run loop. Its execution model is decided
(semantics decision 3, as amended), and so far it has refused library globals
at lowering and added the IR for promises: `ir.KindPromise`, `Async` on `Func`
and `FuncLit`, `ir.Await` and `ir.NewPromise`. The backend refuses all of them
by name until its emitter lands. The runtime has its domains
(`backend/runtime/domain.go`, `promise.go`): a token-handoff scheduler, a
microtask queue and a timer heap per domain, `Promise[T]`, `Async`, `Await`,
`SetTimeout`, and a domain per request that carries on after `res.end()`.
Generated code will pass the current domain as every function's first
argument, because Go has no goroutine-local storage.

Lowering accepts M2's code (`lower/async.go`):
- async functions and arrows, `await`, and `new Promise` with an inline
  executor;
- `setTimeout(callback, delay)` and `clearTimeout(id)`, recognised by being
  declared only in `lib.dom.d.ts`.

The executor's `resolve` lowers as a plain function, because the checker's
union type for it exists only for adopting another promise. Lowering refuses,
each by name:
- `reject`;
- adopting one promise into another, through `resolve` or an async `return`;
- `.then` and `Promise`'s statics;
- top-level `await`;
- a string or extra-argument timer;
- a timer callback that isn't `() => void`.

The race rule now treats a `const` promise as shared state.

The emitter (`backend/emit_async.go`) writes async code as Go:
- an async body is `cakebearrt.Async(ts_domain, func(ts_co *cakebearrt.Co) T {…})`;
- `await` is `cakebearrt.Await(ts_co, p)`;
- promises are `*cakebearrt.Promise[T]`, with `struct{}` for `void`.

Every compiled function and function value takes `ts_domain` first, and every
call passes it on. `main` declares one only when its own code uses it, so a
program that touches nothing in the runtime still has no import. `ts_domain`
and `ts_co` can't collide with user names, because `mangle` produces a `ts_`
name only from a Go keyword or a name already starting with `ts_`. `sleep.ts`
and `ordering.ts` match Node byte for byte. An async handler serves 8
concurrent requests in about 1.3× the time of one, where Node takes 7.2×.
The end-to-end tests and the docs for the subset come next.

After M2, none urgent:

- **`cakec` as a superset of `tsc`.** The end goal is tight integration with
  TypeScript: `cakec` behaves exactly as `tsc` (every flag, `tsconfig.json`,
  `--watch`, `--build`) and adds native builds. It cannot be done by adding
  commands to `cmd/tsc`, which is upstream's: a sync replaces it wholesale.
  But upstream's `cmd/tsc/main.go` is a thin wrapper around
  `execute.CommandLine`, so `cakec` can make the same call for anything that
  is not a cakebear command. Native settings go in a top-level `"cakebear"`
  block of `tsconfig.json`, which `tsc` ignores. Inside `compilerOptions`, an
  unknown key is TS5023. Both were measured 2026-10-12. `execute` has no plugin
  or emit hook, and its `OnProgram` callback switches upstream into test mode,
  so native builds keep constructing their own Program. This replaces the
  earlier "`tsconfig.json` support" item.
- Widen the language subset. `null`/`undefined` need a nullable representation
  in the IR; arrays and objects need an aggregate one. Both are prerequisites
  for most real programs, and aggregates must extend the race rule first.
- Multi-file programs, which need an import graph in the IR.

Completed: **P0** fork established, **P1** guardrails and sync, **P2** `cakec`
type-checks TypeScript, **P3** parallelism exposed and proven deterministic,
**P4** IR, Go emission and native binaries, **P5** cakebear's type extensions,
**M1** a `node:http` server on goroutines: closures, `node:http` lowering, the
runtime and its run loop, and the handler race rule. Upstream then moved, and
cakebear was rebased onto `microsoft/TypeScript`'s `tsc/` with snapshot syncs.

The repository is on GitHub. Actions is disabled there, so pushes and PRs run
nothing. The only workflows in the tree are cakebear's own:
`cakebear-ci.yml` checks our code and `cakebear-sync-upstream.yml` opens the
weekly sync. Upstream's workflows live at the monorepo root and are not synced,
so enabling Actions no longer fires their matrix.

### On extracting the backend into its own module

An earlier plan made this a phase. It was done, then reverted. Keeping the
reasoning so it is not re-litigated from scratch:

A separate module enforces the `ir`/`backend` boundary structurally. But
`scripts/check-boundary.sh` enforces the same property for a fraction of the
cost, and a split charges a tag in one repo plus a version bump in the other
every time the IR changes shape — which, while the IR is young, is most weeks.
A `replace` directive papers over that precisely while it matters most.

Worth revisiting when there is a second consumer of the IR, or when the IR has
gone a few months without changing shape. Note also that a Go *module* boundary
does not require a separate *repository*: a nested `go.mod` gives the same
guarantee in one repo.

## cakebear's type extensions

Declared in `types/cakebear.d.ts` and injected into every program from a
virtual path, so they resolve with no import or reference directive.

| Type | Lowers to | Notes |
|---|---|---|
| `i32` `i64` | `int32` `int64` | native integers, no float64 round-trip |
| `u32` `u64` | `uint32` `uint64` | |
| `f32` | `float32` | |
| `base64` | `string` | compile-time refinement, no runtime representation |

They are **branded types** — an intersection of the primitive with a phantom
property, e.g. `number & { readonly __cakebearBrand: "i32" }`. That is what
makes them possible without a single edit inside `internal/checker`:
TypeScript's own checker does the enforcement, so upstream merges stay
mechanical and retiring an extension is deleting a declaration rather than
unpicking a patch.

Each brand has a conversion function of the same name, and conversion is
deliberately explicit: TypeScript types `i32 + i32` as `number`, because adding
two 32-bit integers can overflow, so widening back is `i32(a + b)` and the
truncation is visible to whoever reads it.

**Literal arguments are validated at compile time.** `i32(3000000000)`,
`i32(1.5)`, `u32(-1)` and `base64("!!!")` are all compile errors. This is the
part a plain `number` or `string` could never do, and the reason the extensions
earn their place. Non-literal arguments are the machine's problem at runtime,
under Go's conversion rules.

Two failure kinds, reported separately because they mean opposite things:
`error:` is a genuine mistake no later phase will make legal, and
`cannot compile yet:` is a limit of the current phase.

## The Phase-1 language subset

`cakec` type-checks all of TypeScript but can only *lower* this much. Anything
else is refused by `lower/` with a span and the construct's name — the refusal
is a limit of the current phase, and the diagnostics say so rather than implying
the user made a mistake.

Supported: `number`, `string`, `boolean`, and cakebear's extensions
(`i32`, `i64`, `u32`, `u64`, `f32`, `base64`); `const`/`let` with explicit type
annotations; function declarations with typed parameters and return; arithmetic;
string concatenation with `+` when both operands are strings; comparisons;
`===`/`!==`; `&&`/`||`/`!`; unary minus; `if`/`else`; `while`; `return`;
`console.log` on one primitive argument; calls to a function by name;
arrow functions and anonymous function expressions as values, with
function-typed parameters, variables and results; `import` from `node:http`
or `http` (named, renamed or namespace), and the members its bundled
declarations list (`createServer`, `listen`, `setHeader`, `writeHead`, `end`,
`req.url`, `req.method`). Those lower to host operations and run on the
runtime's `node:http` (`backend/runtime/http.go`), where each request is handled
on its own goroutine.

Not yet: `null`/`undefined` as values (no nullable representation), mixed-type
`+`, `var`, loose equality, arrays, objects, classes, generics, imports from
anything but `node:http`, `for`, multi-file programs, bitwise operators,
`async`; calling anything but a name or a `node:http` member (`f()()`); named
function expressions; optional, default, rest or destructured parameters;
overloaded or generic function types; `===` or `console.log` on a function or
a host value; the standard library's globals (`parseInt`, `NaN`, `isNaN`,
`setTimeout`, …), which lowering refuses by name because only TypeScript's libs
declare them. A program's own function of the same name is unaffected.

**Host members** are recognised by where their symbol is declared, never by
name and never by the receiver's type. A user's own `createServer` or `end` is
an ordinary function, and a member a program adds to `ServerResponse` through
module augmentation is refused: the receiver's type merges across files, but
the added member has nothing behind it at runtime. The tables in `lower/host.go`
must list exactly what `types/node-http.d.ts` declares, and a test fails when
they drift apart.

**Closures** lower to Go function literals, and Go closures capture by
reference exactly as JavaScript's do, so a captured `let` is read when the
closure runs. A literal takes the signature its slot expects rather than its
own: TypeScript accepts `() => 5` where `() => void` is wanted, and a callback
that declares fewer parameters than its type passes, but Go function types must
match exactly, so lowering pads the parameters and drops the returned value.
Each literal records what it captures and whether that binding is mutable,
which the handler race rule (M1 step 8) builds on.

**Top-level variables** are declared at Go package level and assigned in `main`
at their place in the source, so top-level functions can use them and side
effects keep their order (a package-level initialiser would run before any of
`main`). One difference from JavaScript: reading one before its declaration has
run, from a function called earlier, gives Go's zero value where JavaScript
throws a `ReferenceError`.

## Semantics decisions

Recorded before the emitter was written, per the rule above. Emitting Go means
TypeScript's runtime semantics get expressed in Go's, and these are the three
places they disagree.

**1. Strings are UTF-8, with access mediated by the runtime.** JS strings are
UTF-16 code units (`"日本".length` is 2); Go strings are UTF-8 bytes
(`len("日本")` is 6). We keep Go's representation and route `.length`,
indexing and `slice` through runtime helpers that carry an ASCII fast path,
rather than paying UTF-16 conversion on every string. The overwhelming majority
of strings are ASCII, where the two agree exactly.

*Live in Phase 1:* only that string literals emit as Go string literals. The
helpers land with `.length`.

**2. Numbers are `float64`, printed by JS rules.** TS `number` is IEEE-754
double, so it lowers to Go `float64` directly. Bitwise operators will need
int32/uint32 coercion (`a | b` becomes `float64(int32(a) | int32(b))`), and
`>>>` the unsigned form; neither is in the subset yet.

*Live in Phase 1:* printing. `console.log(5)` must print `5`, not `5e+00` or
`5.000000`. Go's `%v` on a float64 is not JS's Number-to-String algorithm, so
the runtime implements it. This is why `console.log` is a runtime call rather
than a direct `fmt.Println`.

**3. `async`/`await` maps to goroutines: parallel across requests, in Node's
order within one.** JS async is concurrent but never parallel, so correct
TypeScript cannot race; goroutines can. We keep the goroutines, which is what
the M:N scheduler note always implied, and pretending to be one event loop for
the whole process would throw away most of the performance that justifies
compiling TypeScript at all.

*Amended for M2 (decided 2026-10-12).* The original wording, "genuinely in
parallel", is narrowed by **domains**: the main program is one, and each
request handler invocation is another. Every `async` call gets its own
goroutine, but within a domain only one runs at a time, handed a token in the
order Node's microtask and timer queues would run them. So async code inside
one request, or inside main, keeps Node's ordering exactly and cannot race with
itself, while separate requests still run in parallel. An `async` call runs
synchronously up to its first `await`, as in JavaScript. Fully free goroutines
were rejected because `sleep.ts`-style ordering would race. State machines on a
loop were rejected for now because they need a whole-IR transform: `await`
stays an IR expression (`ir.Await`), so they remain a possible backend-only
change later. The cost is roughly 224 ns per `await`, against Node's 32 ns.

This makes cakebear a language that differs from TypeScript at runtime. The
README's compatibility promise ("compared against Node for behavioural
equivalence") was amended in M1 to say so, and to point at the differences
listed below.

*Live since M1:* `node:http` request handlers, the first place cakebear
observably differs from Node: each request runs on its own goroutine, so a
CPU-heavy handler no longer holds up the others. The IR must not assume
single-threaded execution anywhere.

**The handler race rule** (`lower/race.go`) is what keeps that safe. Correct
TypeScript relies on handlers never overlapping, so `hits = hits + 1` in a
handler is fine under Node and a data race here. Rather than add locks,
lowering refuses what could race, as `cannot compile yet`:

- a request handler may not touch a mutable binding declared outside it: a
  `let`, or an enclosing function's parameter. Reading is refused as well as
  writing, because the rest of the program can change the binding while
  handlers run;
- nor may it call, or pass on, a function that touches one, directly or through
  the functions it calls (a summary over the file's call graph, recursion
  included);
- a handler the rule cannot see into, such as a parameter or a call's result,
  is refused rather than trusted.

Reading a `const` is allowed. ⚠ **Carry-forward invariant:** that is only safe
because every M1 `const` holds a primitive, a function or a host value. A
`const` array or object is still mutable (`xs.push(…)`), so this rule must
grow when aggregates arrive, before they do, not after. Explicit shared-state
tools (an atomic counter type in `types/`, say) are how a later milestone
widens it.

M2 hits this invariant first, with promises. A `Promise` settles, so it is
mutable state like any object. M2's lowering must refuse a handler that
reaches a promise declared outside it, such as a top-level
`const ready: Promise<void> = …` awaited in a handler, directly or through what
it calls. The runtime assumes a promise never leaves the domain that created
it, and panics if one does rather than race.

**Accepted differences from Node** (M1 plan, D5). Each is deliberate. Anything
not listed here that behaves differently is a bug.

| Behaviour | Node | cakebear |
|---|---|---|
| A CPU-heavy request handler | blocks every other request | the others are still served (8 `fib(38)` requests: ~1.3× one, vs Node's ~7.7×) |
| `console.log` order across requests | one at a time, per event-loop turn | interleaved, nondeterministic (each line whole) |
| A handler returns without `res.end()` | the request hangs until it times out | an empty 200, plus a one-line warning on stderr |
| A handler does what Node throws for (headers after they were sent, a bad status code, writing after `end`) | the uncaught exception kills the process | a panic with Node's message; `net/http` drops only that connection |
| Response header names | sent as written | canonicalised (`content-type` → `Content-Type`) |
| `req.url`, `req.method` | `string \| undefined` (`@types/node`) | `string`: a server request always has both |
| Reading a top-level variable before its declaration runs | `ReferenceError` | Go's zero value |
| Port already in use | `EADDRINUSE` crash, exit 1 | the same message, exit 1 |
| Ctrl-C / SIGTERM | exit 130 / 143 | exit 130 / 143, after flushing output |
| The listen callback and the first request | the callback runs first | the same: serving starts after the callback |

Not observable to the M1 programs and not yet compared: keep-alive timeouts,
the `Date` header, and when a response is chunked rather than given a
`Content-Length`. Add a row when one matters.

Related: because `number` is `float64`, integer-heavy TypeScript will run
slower than equivalent Go. The refined numeric types (`i32`, `u64`) lowering
straight to Go's are the escape hatch, which makes them a performance feature
rather than an ergonomic one.

## Per-phase planning

cakebear is multi-year and will involve many contributors. Plan only the
**current** phase in detail; defer later phases until they are the active
phase. Don't write speculative roadmaps for things that aren't being built yet.
