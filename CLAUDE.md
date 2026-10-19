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
`cakec build` constructs its own `Program` from their packages and runs its own
emit — it never routes through `internal/execute`, precisely so that hooking
the pipeline requires no edit inside their tree. Everything else `cakec` does
is `tsc`, unchanged: it calls `execute.CommandLine` exactly as `cmd/tsc` does,
using copies of `cmd/tsc`'s own files (`cmd/cakec/tsc_*.go`), since a
`package main` can't be imported. `scripts/copy-tsc-cmd.sh` writes the copies,
every sync runs it, and `TestTscCopies` fails when one has drifted. The copies
are verbatim but for one line of `lsp.go`, which routes the language server's
filesystem through `lspFS`. The script stops if upstream no longer has that
line, rather than drop the change.

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
├── cmd/cakec/      OURS      the cakebear CLI: `tsc`, plus `cakec build`
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
bin/cakec -p .                        # anything but `cakec build` is tsc, unchanged
bin/cakec build --version             # cakebear's; `cakec --version` is tsc's
scripts/copy-tsc-cmd.sh               # re-copy cmd/tsc into cmd/cakec (syncs run it)
bin/cakec build                       # the project in the nearest tsconfig.json
bin/cakec build examples/basic/main.ts
bin/cakec build -o hello examples/http-hello/main.ts && ./hello   # a node:http server
bin/cakec build -p examples/http-project && examples/http-project/bin/server   # in three modules
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

**Go's build cache keys a package on the directory it was built from**, unless
the build is `-trimpath`. Measured 2026-10-16:
- `cakec build` uses `-trimpath` (`backend/backend.go`). Before, each build
  stored the runtime package again, 2.1 MB a build even for the same
  program, so one run of the owned tests added 253 MB, and 9 MB after.
- Building the fork itself from a new directory still adds about 2.1 GB:
  every worktree, and every `git archive` export an agent builds in, is a new
  copy. A cache of 194 GB came from a few days of that. Go deletes entries
  unused for five days; `go clean -cache` clears it at once.

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

## Current phase: **M5**, `null` and `undefined`

P0–P5 and M1–M4 are done. **M5** is `null` and `undefined`: optional
properties, `T | undefined`, `?.` and `??`, narrowing (by truthiness too),
the members that return `T | undefined` (`find`, `at`, `pop()` as a value),
and string- and number-literal unions. Its plan, decided 2026-10-15 at scope
D, is `~/Desktop/cakebear/m5-nullables-plan.md`, sized by measurements of
wx-next (`spikes/m5-measure/`) and a hand port of two of its paths. Then
**M6**, errors: `throw`, `try`/`catch`/`finally`, `reject`, not planned yet,
per the per-phase rule below.

Step 0, the fixes a port of real code found (D13), is done. `%`, products,
extension-type arithmetic and literals (PR 47; "Numbers" and the type
extensions, below), and linear appends, ASCII-pattern search and `req.url`'s
raw bytes (PR 48; "Strings", below). CPU per request against Node, done-check
11, is measured by `spikes/m5-cpu/`: the route server takes 0.66× Node's at
one connection and 1.25× at eight, with 3.7× its throughput.

Step 1, the IR, is done, with nothing lowered to it yet (`ir/maybe.go`):
- `KindMaybe` (`ir.MaybeOf(T)`) is `T | null | undefined`: a T, null or
  undefined. The IR doesn't record which of the two a type admits, since the
  value carries its state and the checker holds a program to its types, so
  `T | null` flows into a `T | null | undefined` slot unchanged (D2, amended).
- `ir.Field.Optional` is `p?: T`, a field of a maybe that a literal may leave
  out. `{ a?: string }` and `{ a: string | undefined }` are two shapes, since
  Node prints `{}` for one and `{ a: undefined }` for the other (D3).
- The nodes: `Wrap` (a T into a maybe's slot, D6), `Unwrap` (a maybe the
  checker narrowed to T, checked, D4), `Empty` (null or undefined in a
  maybe's slot), `IsEmpty` (`=== undefined`, `=== null`, `== null`, D12), and
  `Cond` (`?:`, which `??` and `?.` are built from, D5).
- `Program.Validate` holds each to its types. The backend refuses all of it
  by name until step 3 (`TestMaybesAreRefusedForNow`).

Step 2, the runtime, is done, again with nothing lowered to it yet:
- `Maybe[T]` (`backend/runtime/maybe.go`) is a value and a state, zero for
  undefined. An empty one holds T's zero value and nothing else, so Go's `==`
  on two is JavaScript's `===`, NaN and `-0` included. `Must` is an unwrap
  that panics, naming the value, where TypeScript's narrowing was unsound.
- Printing and JSON (D8) write undefined and null as Node does. A string a
  Maybe holds is written raw at `console.log`'s top level. An array of maybe
  numbers groups from the left only when the first 101 elements are all
  numbers, as `groupArrayElements` checks. `JSON.stringify` leaves an
  undefined property out, writes an array's as `null`, and makes `{}` of an
  object left with none. A spawn's result copies what a Maybe holds (D11).
- Optional properties (D3): `Layout.With` is `o.p = v` for a p the object
  lacks, appending the key, where Node puts it, and `Without` is
  `delete o.p`. Objects that take the same steps share the table they reach.
  Tables are package-level, so every request's goroutine steps from them:
  each keeps its steps in a list read with one atomic load and replaced whole
  under a mutex. That measured 2.0 ns a lookup, against 7.4 for `sync.Map`
  and, across 12 cores, 87 for an `RWMutex` (M5 plan D3).
- The inspect and JSON goldens hold 333 and 95 more outputs recorded from
  Node, with null and undefined (`{"e":"undefined"}` in the codec), in
  sections after the old ones, which are byte for byte as they were.

M4's re-measurement of wx-next (check 9, below) found four lowering bugs,
fixed after M4 closed: a function with no result type was lowered as void
whatever it returned; an extension type in a `number`'s slot reached Go
unconverted; `import type` from a package was refused; and a relative import
of a `.mjs` file was lowered as TypeScript, where it is refused by name now. Three of the four failed inside `go build`,
so `Program.Validate` now holds every `return` to its function's result
(`ir/returns.go`): the next such bug is reported as invalid IR at the return,
before `go build` runs.

M4, decided 2026-10-14 and done 2026-10-15, was objects and arrays, since a
real program is several files of them: exact object types with required
properties, arrays, `for…of`, the common array methods, printing both as Node
does, and `JSON.stringify` (M4 plan D1), then frozen consts and strings. They
extended the race rule before they landed, since a `const` array is still
mutable.

M4's step 1, the prerequisites, is done: `for (;;)`, `break` and `continue`,
`++`, `--` and compound assignment, boolean conditions, and type declarations
erased (see "Since M4" in the language subset). So is step 2, the fail-loud
emitter: a type, a printed value or an operator the backend has no Go for is
reported as `cannot compile yet` at its span. Before, `goType` fell back to
`any`, `console.log` to printing `undefined`, and an unknown unary operator
to `!`, so the IR could gain a kind (as it will with objects) and build into
a program that quietly did the wrong thing.

Step 3, the IR for objects and arrays, is done too, with nothing lowered to
it yet:
- `ir.KindObject` and `ir.KindArray`. An object type is of a **shape**
  (`ir/shape.go`): its fields sorted by name, interned program-wide by
  `ir.Shapes`, so one structure is one `*Shape` and `Equal` is pointer
  identity (D3). `Program.Shapes` lists them by ID. A type that refers to
  itself is reserved first, then defined, and is never merged with another.
- The nodes (`ir/aggregate.go`): `ObjectLit`, whose fields stay in source
  order (the order Node prints and evaluates them in), `ArrayLit`,
  `FieldGet`, `IndexGet`, `ArrayOp` (the D16 methods), and the statements
  `FieldSet`, `IndexSet` and `ForOf`.
- `Program.Validate` (`ir/check.go`) holds them to their shapes and element
  types: every field set exactly once, each field index in range, values of
  the field's type, each method's arguments and result, and every object
  type of one of the program's own shapes.

Step 4, the runtime for them (`backend/runtime/`), is done, again with
nothing lowered to it yet:
- `Array[T]` (`array.go`): a reference to its elements, so every alias sees
  a push. `Get`, `Set` and `SetLength` panic where Node would read
  `undefined` or leave a hole (D8, D9), each message saying what Node does.
- `Object` (`object.go`), the interface each generated struct will
  implement: `Layout`, the key table its literal chose, so `{ y, x }` prints
  `y` first, and `Field`. `Clone` copies a graph of them, sharing and cycles
  kept, for a spawn's result (D11).
- `inspect.go` and `inspect_string.go`: a port of Node 24.18's
  `util.inspect`, function by function, for what the subset builds, and
  `Log(args...)`. `TestInspectGoldens` holds it to 20,000 outputs recorded
  from Node (`testdata/`, made and checked by `gen-inspect-goldens.mjs`,
  deterministic). A function can't be printed: a Go func carries no name for
  `[Function: name]`, so lowering must refuse `console.log` of a value
  holding one.
- `width.go`: display width, which grouping measures. The table is Node's
  own, every code point through `getStringWidth`
  (`scripts/gen-width-table.mjs`). Node measures a string's NFC form, and
  only Hangul jamo and 94 other pairs compose into another width, so
  `displayWidth` composes those rather than carry NFC.

Step 5, objects end to end, is done: objects lower, emit and print as Node
prints them.
- Lowering (`lower/object.go`): shapes, literals in their slot's shape,
  fields, exact flows checked against every expression's contextual type,
  D6's refusals by name, and the race rule for aggregates (D10).
- Emitting (`backend/emit_object.go`): a struct per shape, `ts_shape<ID>`,
  that is a `cakebearrt.Object`, and a package-level key table per shape and
  key order. A literal is a keyed composite literal in source order, so its
  calls run in JavaScript's order. In an ordered expression a field read is
  `ts_read(p.x)` (check 7).
- `TestObjectsMatchNode` and a multi-module case hold it to Node, 20 runs
  each. `TestAggregateSharedStateRefused` holds the race rule through
  `cakec`.

Step 6, arrays end to end, is done (`lower/array.go`,
`backend/emit_array.go`). An array is a `*cakebearrt.Array[T]`, literals take
their slot's element type, and `for…of` re-reads the length every iteration.
A spawned function's array or object result is copied on its way to the
caller when the spawn left work behind (`runtime/spawn.go`, D11). What Node
answers with `undefined` or a hole panics (D8, D9). `TestArraysMatchNode`
holds it to Node.

Step 7, the array methods, is done (D16). Lowering
(`lower/array_methods.go`) gives each callback its slot's full signature,
and `TestArrayMembersAreAccountedFor` fails when TypeScript's libs give
`Array` a member that is neither lowered nor refused by name. The runtime
(`runtime/array_methods.go`, `array_join.go`) follows ECMA-262: length taken
at the start, SameValueZero for `includes`, ToString for `join`,
ToIntegerOrInfinity for `slice`, a stable sort. `TestArrayMethodsMatchNode`
holds it to Node. Two silent bugs from before turned up and are fixed:
- a named function with fewer parameters than the function type it fills
  failed `go build` (`lower/adapt.go` wraps it);
- arithmetic on two literals ran at Go's compile time, exactly, so
  `0.1 + 0.2` printed 0.3 (`backend/emit_const.go`).

Step 8, shorthand properties and object destructuring, is done (D14,
`lower/destructure.go`). `{ x }` is `{ x: x }`. A pattern holds the value it
destructures in a binding of lowering's own, whose name has a space in it so
it meets none of the program's, and reads each name's field from that, so
the value is evaluated once. Patterns work wherever a name is declared:
`const` and `let`, at the top level (exported too) or not, the parameters of
functions and closures, `for…of`, and a `for` loop's first clause. A
destructured `const` is a const to the race rule, and a shorthand reads its
variable there as the bare name does, though the checker resolves the
shorthand's name to the property (`valueSymbol`, `lower/closure.go`). `TestDestructuringMatchesNode`,
its edge cases and a multi-module case hold it to Node. One silent bug from
step 1 turned up and is fixed: a `for` loop declaring two variables emitted
one Go short declaration, which evaluates every value before it declares any
name, so in `for (let i = 0, j = i + 1; …)` the `i` that `j` read was an
outer one. `backend/emit_loops.go` now declares them in order, before the
loop.

Step 9, `JSON.stringify`, is done (D15). Lowering (`lower/json.go`) makes
an `ir.JSONStringify`, and the runtime (`runtime/json.go`, `json_string.go`)
writes it as ECMA-262 does, with V8's two departures matched: a number space
between 0 and 1 breaks lines but indents by nothing, and a string space ends
at its first NUL. That is why the gap is a `JSONGap` value rather than a
string. `TestJSONGoldens` holds the runtime to 10,600 outputs recorded from
Node (`testdata/gen-json-goldens.mjs`, deterministic), cycles included, and
`TestJSONStringifyMatchesNode` holds a program to it end to end. A cycle
panics with V8's `TypeError` message, verbatim.

Step 10, frozen consts, is done (D10b, `lower/frozen.go`). A request handler
or a spawned function may read a `const` object or array declared outside it
when the const is frozen: its type is readonly all the way down, and its
initialiser builds everything it holds from literals or other frozen consts.
That gives a server a route table. To keep it sound, lowering refuses a
readonly value where a mutable one is wanted, which TypeScript allows for
properties. `TestFrozenRouteTableServes` serves a route table as Node does.
Two fixes came with it:
- Since M1, a response the program gave no `Content-Type` went out with one
  net/http sniffed from the body, where Node sends none
  (`runtime/http.go`).
- `TestSpawnsRunInParallel` timed two sleeps against a limit a loaded machine
  could miss. It now waits at a barrier instead.

The strings step, between steps 10 and 11, is done (`m4-strings-plan.md`,
its scope ranked by a measurement of wx-next). Lowering (`lower/strings.go`,
`lower/numbers.go`) makes `ir.BuiltinCall` and `ir.Concat`, and the runtime
gives them JavaScript's semantics in UTF-16 units: the string methods, the
case table generated from Node and comparison (`runtime/str*.go`), and
`toFixed`, the parsers and the URI functions (`num*.go`, `uri.go`).
`TestStrGoldens`, `TestNumGoldens` and `TestURIGoldens` hold them to about
110,000 outputs recorded from Node, and `TestStringsMatchNode` holds a
program to it end to end. Silent bugs found and fixed:
- string `<` compared UTF-8 bytes, so `"\uFFFF" < "😀"` was true (since P4);
- a surrogate pair joined by `+` didn't equal the literal, and printed raw;
- printing a lone surrogate wrote raw bytes, where Node writes U+FFFD;
- `setHeader` silently dropped a header name Node throws for, and sent a
  value Node throws for; both panic with Node's message now.

Step 11, M4's end to end, is done. `examples/route-server` is a JSON API in
four modules, a frozen menu, query parsing with the string methods and
`JSON.stringify` replies, built from its `tsconfig.json`. It is check 8,
zob's live test. `TestRouteServerServes` holds its answers to 14 requests to
Node's (status, `Content-Type` and body), and `TestRouteServerUnderRace`
serves 200 requests, 8 at a time, from a race-detector build, which it checks
is one, with no race found. Check 9 re-measured wx-next with the real lowering:
`spikes/m4-remeasure/` (see M4 plan, step 11 results).

M3 made `cakec` a superset of `tsc` that builds real projects:
- the `tsc` passthrough;
- native builds from `tsconfig.json`;
- cakebear's declarations in `tsc` mode and the language server;
- the IR for programs (`ir/program.go`): an `ir.Program` holds the modules
  sorted by file, a reference to a top-level name says which module declares
  it, and `Program.Validate`, which the backend runs before emitting, holds
  lowering to that;
- lowering across files (`lower/program.go`, `lower/module.go`): the entry
  module and every module of the program's own it imports, through one
  checker, under the program-wide race rule;
- emitting across files (`backend/emit_program.go`): one Go package, each
  module's names prefixed, each module's top-level code run in ES order (see
  "Modules" below);
- end to end: `examples/http-project`, a server in three modules built from
  its `tsconfig.json`. Its imports name their `.ts` files, so Node runs the
  same sources, and `TestMultiFileMatchesNode` holds four such programs to
  Node's output, 20 runs each.

- **`cakec` as a superset of `tsc`.** The end goal is tight integration with
  TypeScript: `cakec` behaves exactly as `tsc` (every flag, `tsconfig.json`,
  `--watch`, `--build`) and adds native builds. It cannot be done by adding
  commands to `cmd/tsc`, which is upstream's: a sync replaces it wholesale.
  But upstream's `cmd/tsc/main.go` is a thin wrapper around
  `execute.CommandLine`, so `cakec` makes the same call for anything that is
  not `cakec build`. Only the bare word is cakebear's, one dash from `tsc`'s
  `--build` (M3 plan D2). `TestCakecIsTsc` compares the two, built from this tree,
  over sample projects and flags: stdout, stderr, the exit code and every file
  written, plus `--watch` through an edit and the language server's
  `initialize`. Native settings go in a top-level `"cakebear"`
  block of `tsconfig.json`, which `tsc` ignores. Inside `compilerOptions`, an
  unknown key is TS5023. Both were measured 2026-10-12.
  - **Project builds** (`cmd/cakec/project.go`) read `"cakebear": { "main",
    "output" }` from the config named by `-p`, or else the nearest one at or
    above the working directory, as `tsc` finds it.
  - They type-check every file the config lists, under its own options, and
    compile `main` alone. A file `main` doesn't import is checked but never
    runs, as under Node.
  - They refuse a config that turns off `strictNullChecks` or `noImplicitAny`,
    since cakebear's own options always have both on.
  - Only that config's block is read: `extends` doesn't carry unknown keys, so
    a block in a base config isn't seen, and the error says so.
  - cakebear's declarations join the project's root files. They stay files of
    their own because lowering recognises `spawn`, the conversions and
    `node:http`'s members by the virtual file declaring them.

  `execute` has no plugin
  or emit hook, and its `OnProgram` callback switches upstream into test mode,
  so native builds keep constructing their own Program. This replaces the
  earlier "`tsconfig.json` support" item. The `tsc` passthrough differs from
  `tsc` in one way: a project with a `"cakebear"` block, and every project in
  the language server, sees cakebear's declarations (see the type
  extensions).
- Widen the language subset. `null`/`undefined` need a nullable representation
  in the IR (M5); arrays and objects need an aggregate one (M4). Both are
  prerequisites for most real programs, and aggregates must extend the race
  rule first.

Completed: **P0** fork established, **P1** guardrails and sync, **P2** `cakec`
type-checks TypeScript, **P3** parallelism exposed and proven deterministic,
**P4** IR, Go emission and native binaries, **P5** cakebear's type extensions,
**M1** a `node:http` server on goroutines: closures, `node:http` lowering, the
runtime and its run loop, and the handler race rule; **M2** `async`/`await`:
promises, `await`, `new Promise`, `setTimeout`/`clearTimeout`, a domain per
request, matching Node's ordering byte for byte within a domain, then
top-level `await` and `spawn`. Upstream then moved, and
cakebear was rebased onto `microsoft/TypeScript`'s `tsc/` with snapshot syncs.
**M3** real projects: `cakec` is `tsc` for everything but `cakec build`,
native builds from `tsconfig.json`, cakebear's declarations in `tsc` mode and
the language server, and programs of several modules, lowered under a
program-wide race rule and run in ES order. **M4** objects and arrays: exact
shapes, arrays and their methods, destructuring, printing and
`JSON.stringify` as Node does, frozen consts a handler may share, and the
strings step's template literals, string methods and number parsers.

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

Declared in `types/cakebear.d.ts` and injected into every program, so they
resolve with no import or reference directive (`cmd/cakec/declarations.go`):

- **Native builds** add the declarations files as root files at virtual paths.
  Lowering recognises `spawn`, the conversions and `node:http`'s members by
  the virtual file declaring them, so they stay files of their own.
- **The language server** (`cakec --lsp`) always has them. **`tsc` mode** has
  them only for a project with a `"cakebear"` block, so `cakec` on any other
  project is byte for byte `tsc`. Neither can be given extra root files, so
  the declarations are appended to the bundled `lib.es5.d.ts`, which every lib
  chain with an `Array` reaches. Go-to-definition on `spawn` lands there.
- A *script*, a file with no import or export, that declares its own global
  `spawn` or `i32` merges with cakebear's and gets TS2394 inside the lib. A
  module's own declaration shadows cakebear's instead.

| Type | Lowers to | Notes |
|---|---|---|
| `i32` `i64` | `int32` `int64` | native integers, no float64 round-trip |
| `u32` `u64` | `uint32` `uint64` | |
| `f32` | `float32` | |
| `base64` | `string` | compile-time refinement, no runtime representation |

The same file declares one function that isn't a conversion: **`spawn`**
(see the language subset). It is recognised by that declaration, so a
program's own `spawn`, or one imported from elsewhere, is an ordinary call.

They are **branded types** — an intersection of the primitive with a phantom
property, e.g. `number & { readonly __cakebearBrand: "i32" }`. That is what
makes them possible without a single edit inside `internal/checker`:
TypeScript's own checker does the enforcement, so upstream merges stay
mechanical and retiring an extension is deleting a declaration rather than
unpicking a patch.

Each brand has a conversion function of the same name, and conversion is
deliberately explicit: TypeScript types `i32 + i32` as `number`, because adding
two 32-bit integers can overflow, so widening back is `i32(a + b)` and the
truncation is visible to whoever reads it. The other way, an extension type
filling a `number`'s slot (a variable, an argument, a result), TypeScript
allows, so lowering widens it to a float64; it used to reach `go build`
unconverted and fail there.

**Arithmetic follows the types TypeScript gives it** (`lower/arith.go`):
- Outside a conversion, an extension type's arithmetic is a number's, its
  operands widened: `const x: number = a / b` is 3.5 for 7 and 2, and an
  `i32` with a `number` is a number's sum.
- Inside its own conversion, it is native: `i32(a + b)` adds in Go's `int32`
  and wraps, as JavaScript's `(a + b) | 0` does, and `i32(a * b)` is
  `Math.imul`'s, exactly. That is the escape hatch's speed.
  - For an integer type, every `+`, `-` and `*` below the argument is native,
    and a `/` or `%` only as the argument itself, since truncating
    mid-expression isn't what a number truncated once gives.
  - For `f32`, only the argument's own operator is native: `float32` rounds
    every step, where a number rounds once.
  - A zero divisor there panics, saying what the number would have been.
- Until this was decided, the arithmetic was native everywhere, so a sum of
  two large `i32`s printed wrapped. And from PR 46, every conversion's
  argument went through a `float64`, so `i64(b + b)` lost what a number
  can't hold past 2^53.

**Literal arguments are validated at compile time.** `i32(3000000000)`,
`i32(1.5)`, `u32(-1)` and `base64("!!!")` are all compile errors. So is a
literal no number holds exactly: TypeScript reads the literal in
`i64(9007199254740993)` as the number 9007199254740992 before the conversion
sees it. A literal converts as the number it is, from its exact digits.
This is the part a plain `number` or `string` could never do, and the reason
the extensions earn their place. A non-literal number converted to an
integer type, `i32(n)`, is the machine's problem at runtime, under Go's
conversion rules. A value of another extension type converts as Go converts
integers, so `u32(i32(-1))` is 4294967295, as `>>> 0` gives.

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
annotations; function declarations with typed parameters and return; arithmetic
(`+`, `-`, `*`, `/` and `%`, which is `math.Mod`, Node's to the bit);
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

Since M2 (`lower/async.go`):
- `async` functions and arrows, including an async request handler;
- `await` of a promise;
- `Promise<T>` as a type, recognised only when every declaration of it is in a
  bundled lib;
- `new Promise((resolve) => …)` with the executor written inline. `resolve`
  is a plain function: `(value: T) => void`, or `() => void` for
  `Promise<void>`;
- `setTimeout(callback, delay)` and `clearTimeout(id)`, the DOM's, recognised
  by being declared only in `lib.dom.d.ts`. The callback must be `() => void`,
  and the id is a `number`;
- top-level `await`. TypeScript allows it only in an ES module, a file with an
  import or an export, so an empty `export {}` is accepted, as a no-op;
- `spawn(fn)`, cakebear's own global (`types/cakebear.d.ts`, so it needs no
  import). It runs `fn`, a function taking nothing, in a new domain, in
  parallel with its caller, and returns a promise of its result. An async `fn`
  runs until its own promise settles. `fn` is held to the race rule below, and
  the program waits for every spawn before it exits.

Since M3 (`lower/module.go`), across the program's own modules:
- named imports, renamed or not (`import { a, b as c } from "./x"`), of the
  functions and `const`s a module declares at its top level;
- `import "./x"` for its effects alone, and type-only imports and exports,
  from a package too (`import type { NextRequest } from "next/server"`), since
  they evaluate nothing;
- `export function`, `export const` and `export { x }`;
- import cycles.

The program is the entry module and everything it imports, directly or not,
by a relative path to a TypeScript file. A file nothing imports is checked but
is no part of it.

Refused by name: a JavaScript module (`./helper.mjs`), default imports and exports, `import * as` (except
`node:http`), `export … from` and re-exporting an import, exporting a `let`
(every importer would share a binding that can change), and a top-level
`await` outside the entry module (M3 plan D8).

Since M4 (`lower/loops.go`):
- `for (init; cond; post)`, with any clause empty. A `let` in the first clause
  is a binding per iteration, as in JavaScript, and so is a Go 1.22 loop
  variable, so a closure made in the body keeps its own iteration's value;
- unlabelled `break` and `continue`;
- `++`, `--`, `+=`, `-=`, `*=`, `/=` and `%=` on a `number` variable, and `+=` of a
  `string` onto a `string`, as statements. They lower to `x = x op v`. On an
  extension type they are refused, so the narrowing stays visible:
  `n = i32(n + 1)`;
- `interface` and `type` declarations, which are erased;
- a condition (of an `if`, a `while` or a `for`) must be a `boolean`.
  TypeScript tests any value's truthiness, which is refused for now.

A function whose end TypeScript knows can't be reached, such as one ending in
`while (true)`, gets a panic after its last statement: Go's rule for where a
function may end is narrower (`backend/emit_loops.go`).

Refused by name: labelled statements, a comma expression in a `for` clause,
`++` or `--` as a value (`f(i++)`), and an update of anything but a
variable's name or a field of one.

Since M4 step 5 (`lower/object.go`), objects:
- interfaces and object type aliases whose properties are all required and
  of types the subset holds, and object literals of them. A type is a
  **shape**, its fields sorted by name, so `interface Point` and
  `type Vec = { y: number; x: number }` are one type, and a value flows
  between them unchanged. A literal takes its slot's shape, from the
  checker's contextual type.
- a literal keeps the order it writes its keys in, which is the order Node
  prints them (`{ y, x }` prints `y` first), and is evaluated in that order;
- field reads, writes, and `++`, `--` and compound assignment on `name.f`;
- `===` and `!==` on objects, which compare identity, as in JavaScript;
- `console.log` of several values, objects included, printed through the
  runtime's port of `util.inspect`. A string first, before other values, must
  be a literal with no `%`: Node would treat it as a format.

Flows are exact (M4 plan D5): a value of one shape can't fill a slot of
another, even where TypeScript allows an object with more fields. Refused by
name: optional properties, index signatures and `Record`, unions and
intersections, classes, methods and accessors, computed, numeric and
integer-like keys, `__proto__`, spread,
optional chaining, and `console.log` of a value holding a function (a Go
func has no name to print).

Since M4 step 6 (`lower/array.go`), arrays:
- `T[]` and `readonly T[]` of any type the subset holds, and array literals.
  A literal's element type is its slot's, so `[]` is typed by where it goes.
- `xs[i]` read and written, `xs.length`, `xs.length = n` to shrink,
  `xs.push(a, …)`, and `xs.pop()` as a statement. `++`, `--` and compound
  assignment on `xs[i]`, with a name or a number for `i`, and on
  `xs.length`;
- `for (const x of xs)` and `let`, over an array: evaluated once, its length
  read every iteration, so it visits what the loop pushes (D13), with a
  binding per iteration;
- `===` and `!==`, identity, and arrays in objects, objects in arrays, and
  arrays of arrays.

Refused by name: tuples, holes, spread, `pop()` as a value (its
`T | undefined` waits for M5), a member a program adds to `Array`, an
element access on anything but an array, an index that isn't a number, and
`for…of` over a string, with `await`, with an array pattern, or over an
existing variable.

Since M4 step 7 (`lower/array_methods.go`), the array methods: `map`,
`filter`, `join`, `includes`, `indexOf`, `slice`, `sort(compare)`, `some`,
`every`, `forEach`, `reduce(fn, initial)` and `findIndex`. A callback may be
written in place or named. A named function taking fewer parameters than its
slot passes is adapted to it, here and wherever a function flows
(`lower/adapt.go`). Refused by name: every other `Array` member (`find`,
`at`, `shift` and `findLast` wait for M5's `T | undefined`), `sort()` with no
comparator, `reduce()` with no initial value, a `thisArg`, `fromIndex`, a
callback testing truthiness, `map` returning nothing, `includes` or
`indexOf` over functions, and `join` over functions, promises, host values
or objects with their own `toString` or `valueOf`.

Since M4 step 8 (`lower/destructure.go`):
- shorthand properties, `{ x }`;
- object destructuring: `const { a, b: renamed, c: { d } } = value`, and
  `let`, in a declaration, a parameter (`({ x, y }: Point) => …`), a
  `for…of` loop's variable, and a `for` loop's first clause. The value is
  evaluated once, and each name reads its field from it.

Refused by name: array destructuring, rest elements, defaults, computed
keys, destructuring assignment (`({ a } = o)`), and destructuring anything
but an object (a string, an array, a `node:http` value: read `req.url`
instead).

Since M4 step 9 (`lower/json.go`), `JSON.stringify(value)` and
`JSON.stringify(value, null, space)`, byte for byte as Node writes it:
objects in their literal's key order, `-0` as `0`, NaN and the infinities as
`null`, and a space as a number of spaces (at most 10) or a string's first
10 characters. V8 has a quirk of its own, matched too: a number strictly
between 0 and 1 gives lines and `": "` with no indent, where the
specification says compact. `JSON` is recognised as the global TypeScript's libs declare,
so a program's own `JSON` is an ordinary binding.

Refused by name: a replacer (`null` or `undefined` in its place is how a call
reaches the space), `JSON.parse` (it returns `any`, and decoding into a type
needs checks that wait for M5 and M6), JSON's other members, and
serialising a function, a promise or a host value, or an object or array
holding one: Node leaves the first out, or gives `undefined` for one alone,
and serialises the others as objects of its own.

Since M4 step 10 (`lower/frozen.go`), **frozen consts**: a `const` whose type
is readonly all the way down (`readonly` properties, `readonly T[]` or
`ReadonlyArray<T>`, `Readonly<T>`, of primitives or more of the same) and
whose initialiser builds every object and array it holds from literals, or
other frozen consts, may be read by a request handler or a spawned function.
A name destructured from one is frozen too. A refusal for a const that isn't
says why, such as ``property `path` isn't readonly``.

Refused by name, since step 10: **a readonly value where a mutable one is
wanted**, which TypeScript allows for properties (`const m: Route = frozen`,
where `Route` has `path` and the frozen value's type `readonly path`), in a
variable, an argument, a result, a field, an element, or a callback's
parameter. It is the one way a mutable alias of a frozen const could be
made. The other way round, mutable into readonly, is fine.

Since the M4 strings step (`lower/strings.go`, `lower/numbers.go`; plan
`m4-strings-plan.md`, its scope ranked by a measurement of wx-next):
- template literals, `` `a${x}b` ``, whose substitutions are strings,
  numbers, booleans or extension types, converted as JavaScript's ToString
  converts them;
- `String(x)`, `x.toString()` and `n.toFixed(digits)`, with JavaScript's
  rounding, a tie up (`(2.5).toFixed(0)` is `"3"`, where Go's `FormatFloat`
  gives `"2"`);
- a string's `length`, `charAt`, `charCodeAt`, `includes`, `startsWith`,
  `endsWith`, `indexOf`, `lastIndexOf`, `slice`, `substring`, `split` with
  a string separator, `replace` and `replaceAll` with a string pattern and
  replacement (`$&` and the rest expanded), `toLowerCase` and `toUpperCase`
  (full case mapping: `"ß"` → `"SS"`), `trim`, `trimStart`, `trimEnd`
  (JavaScript's whitespace, not Go's), `padStart`, `padEnd` and `repeat`.
  Every index and length is in UTF-16 code units, through the runtime;
- `Number(s)`, `parseInt`, `parseFloat`, `Number.isFinite`, `Number.isNaN`,
  `Number.isInteger`, `isNaN` and `isFinite` of a number, and
  `encodeURIComponent` and `decodeURIComponent`;
- `<`, `>`, `<=` and `>=` on strings compare in UTF-16 order, and `+` on two
  strings joins a surrogate pair split between them, as JavaScript's UTF-16
  strings do. Both are through the runtime (`StrCompare`, `StrConcat`).

The globals are recognised by where their value is declared, in TypeScript's
libs, so a program's own `String` or `parseInt` is an ordinary binding. Every
member the libs give a string is lowered or refused by name, and a test
fails when they gain one.

Refused by name: anything taking a RegExp, and `match`, `matchAll` and
`search` (RegExp is a step of its own: in 12% of wx-next's files, and
`match` returns `null`, M5's); a function replacement; `localeCompare`
(ICU collation) and the `toLocale*` forms; `normalize`; `at` and
`codePointAt` (`undefined` past the end, M5); `s[i]` (the same; use
`charAt`); `toString(radix)`; `toPrecision`; a template, `String()` or
`Number()` of anything but a primitive; and `+` between a string and
anything else (measured at 10 uses in wx-next: write a template literal).

Names TypeScript allows and Go doesn't are escaped (`backend/mangle.go`):
`$`, combining marks (an `é` written as `e` and U+0301), the joiners U+200C
and U+200D, and letter-numbers such as `Ⅻ`. A name holding any of them
becomes `ts_x_` and the name with `_` doubled, `$` as `_D`, and anything else
as `_u` and its hex code point, closed by `_`: `$count` is `ts_x__Dcount`.
The escape reads back to exactly one name, so two names never meet. Before,
each reached `go build` and failed there.

Not yet: `null`/`undefined` as values (no nullable representation), mixed-type
`+`, RegExp, `URL` and `URLSearchParams`, `var`, loose equality, classes,
generics, imports from anything but `node:http` and the program's own
modules, `for…in`, `do…while`, `switch`, bitwise operators, `**` (it waits
for `Math`, since Go's `math.Pow` differs from V8's in the last bit);
calling anything but a name or a `node:http` member (`f()()`); named function
expressions; optional, default or rest parameters; overloaded or
generic function types; `===` or `console.log` on a function, a host value or
a promise; the standard library's other globals (`NaN`, `Math`,
`setInterval`, …), which lowering refuses by name because only TypeScript's
libs declare them. A program's own function of the same name is unaffected.

Not yet, from M2, each refused by name:
- `reject`, `throw` and errors (M6);
- adopting one promise into another, through `resolve(p)` or an async `return p`
  (await it instead);
- `.then`, `.catch`, `.finally` and `Promise.all`/`resolve`/…;
- an async executor;
- a string timer handler, extra timer arguments, or a missing delay;
- a request handler that reaches a promise declared outside it (the race
  rule, below).

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

**Evaluation order** (`backend/order.go`). JavaScript evaluates operands left
to right, reads included, so in `a + f()` with `f` changing `a`, `a` is read
first. Go orders an expression's calls but not its plain reads against them,
and gc reads after the calls: `a + f()` gave `f`'s new `a`, silently, until M3
closed. Now, in an expression with a call below its root, a read of a binding
that some code writes to (`ir.Ident.Stable` false) goes through a generated
identity function, `ts_read(a)`, and Go's left-to-right order of calls puts it
where JavaScript reads it. A binding nothing writes after its declaration
needs nothing, which keeps the generated Go readable. `TestEvaluationOrderMatchesNode`
holds eight such expressions to Node's output.

**Top-level variables** are declared at Go package level and assigned in `main`
at their place in the source, so top-level functions can use them and side
effects keep their order (a package-level initialiser would run before any of
`main`). One difference from JavaScript: reading one before its declaration has
run, from a function called earlier, gives Go's zero value where JavaScript
throws a `ReferenceError`.

**Modules** (M3, `backend/emit_program.go`) all go into one Go package. A Go
package per module fails: Go forbids the import cycles ES allows.
- Each module's top-level names are prefixed `ts_m<i>_`, with its index in
  `ir.Program.Modules`, which lowering sorts by file. That keeps two modules'
  `helper`s apart, and `ts_` is the namespace `mangle` keeps for itself, so a
  prefixed name never meets a local's. A program of one module has no
  prefixes, and its Go is what it always was.
- Each module but the entry has its top-level code in `ts_m<i>_init`. `main`
  calls them in `ir.Program.EvaluationOrder`, then runs the entry's own:
  depth first from the entry, each module after what it imports, in import
  order, with a module already on the way not entered again, which settles a
  cycle. That is an ES module graph's order, checked against Node 24.18.
- An import evaluates its module whether or not what it names is used, as
  under Node. `import type` is erased and brings in nothing, and so is an
  import naming only types, as in TypeScript's emit (Node refuses one: a type
  has no export to bind). A module that only types reach never runs.
- Reading another module's binding across a cycle, before that module has
  run, gives Go's zero value where JavaScript throws a `ReferenceError`. It's
  the same difference as for a top-level variable read early.

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

*Live since the M4 strings step:* the helpers (`backend/runtime/str*.go`),
with an ASCII fast path: 2% of wx-next's string literals hold anything else,
and that is mostly dashes, arrows and emoji. A lone surrogate is WTF-8, as the
scanner stores `"\uD800"`, and printing one writes U+FFFD, as Node's stdout
does. Comparison is in UTF-16 order, which differs from Go's byte order only
where a character outside the BMP meets one from U+E000 up: `"\uFFFF" < "😀"`
is false, as in JavaScript.

*Since M5 step 0:* appends are linear (M5 plan D13, `lower/append.go`,
`backend/emit_append.go`, `runtime/str_acc.go`). A Go string is immutable,
so `s += x` copied the whole string every time, where V8's ropes don't: a
10k-append page builder took 331 ms against Node's 0.8 ms.
- A string `let` of its own statement, or a parameter, that the program
  appends to (`s += x`, `s = s + …`, or a template starting with `${s}`)
  gets a `cakebearrt.StrAcc` beside it, `ts_acc_s`, in the same Go scope. A
  closure capturing s captures its accumulator too.
- An append writes in place while s is still the accumulator's newest
  string, and copies otherwise: another write to s, `s = ""` say, just
  leaves the accumulator behind.
- `s.length` on the binding reads its length from the accumulator while
  every byte so far is ASCII. Otherwise it scans, as `StrLength` does.
- The bytes of a string the accumulator handed out are never written again.
  So any prefix of it held elsewhere keeps its value, even a `const` copied
  from s and read by a spawn as s grows (`TestAppendedPrefixSharedWithASpawn`,
  under `-race`).
- It needs no atomics only because the race rule keeps a `let` in one
  domain. Shared-state tools would have to revisit it.
- A field (`o.f += x`), an element, a let in a `for` loop's first clause (a
  binding per iteration), a destructured one, and a prepend still copy. On
  wx-next that leaves 1 of 148 append sites
  (`spikes/m5-measure/reports/append_sites.md`).
- A chain of string `+` is one `StrConcat` of all its pieces, not one per
  `+`.

**2. Numbers are `float64`, printed by JS rules.** TS `number` is IEEE-754
double, so it lowers to Go `float64` directly. Bitwise operators will need
int32/uint32 coercion (`a | b` becomes `float64(int32(a) | int32(b))`), and
`>>>` the unsigned form; neither is in the subset yet.

*Live in Phase 1:* printing. `console.log(5)` must print `5`, not `5e+00` or
`5.000000`. Go's `%v` on a float64 is not JS's Number-to-String algorithm, so
the runtime implements it. This is why `console.log` is a runtime call rather
than a direct `fmt.Println`.

*Since M4 step 7:* arithmetic. Go evaluates an expression of constants
exactly, at compile time, so `0.1 + 0.2` emitted as is gives 0.3 and
`1e308 * 10` won't compile. Arithmetic on two literals passes one through the
identity function `ts_read`, which makes it a run-time float64 operation,
rounded as JavaScript rounds it (`backend/emit_const.go`).

*Since M5 step 0:* products. Go may fuse `a * b + c` into one multiply-add
that rounds once, where JavaScript rounds the product and then the sum, and
on arm64 it does. Every product was emitted bare from P4 on, so
`0.6764556596678251 * 0.4613862177205994 - 0.5085473976760264` printed
`-0.1964400794061956` where Node prints `-0.19644007940619557`. A product is
now emitted as `float64(a * b)`: an explicit conversion is the one thing the
Go spec says a fusion can't cross (`unfused`, `backend/emit_arith.go`).
`TestArithmeticMatchesNode` holds all four fusable forms to Node.

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

*Live since M2:* the domains. The runtime (`backend/runtime/domain.go`,
`promise.go`) gives each domain a microtask queue, a timer heap and a token.
Only the goroutine holding a domain's token touches that domain, so it needs
no lock.
- An async call (`cakebearrt.Async`) runs on its own goroutine, and its caller
  waits for the token to come back.
- `await` (`cakebearrt.Await`) hands the token back and resumes in the
  microtask the promise's settling queues.
- Main's loop (`Run`) runs in the order an ES module sees under Node: the
  microtasks, then queued callbacks such as `listen`'s, then timers. This was
  measured on Node 24.18; CommonJS puts the `listen` callback first.
- A request's domain is driven by its `net/http` goroutine until `res.end()`.
  Anything still pending then carries on on its own goroutine, and keeps the
  process alive, because Go finishes a response when its handler returns.
- With a top-level `await`, main's top-level code is itself an async body
  (`ts_module`). `RunModule` exits 13 if it never settles, as Node does.
- `spawn` (`backend/runtime/spawn.go`) starts a domain on a goroutine of its
  own. Its promise is the one promise another domain settles: the spawned
  domain posts the result to the caller's **inbox**, a mutex-guarded queue
  with a wake channel. That queue is the only part of a domain other
  goroutines touch. The caller's driver runs posted work as tasks, and counts
  outstanding spawns as pending work. A panic in a spawned function is
  re-raised in the caller.

Go has no goroutine-local storage, so every compiled function and function
value takes the domain it runs in as its first parameter (`ts_domain`), and
every call passes its own. `main` declares one only when its own code uses it.
`ts_domain` and `ts_co` can't collide with user identifiers: `mangle` produces
a `ts_` name only from a Go keyword or a name already starting with `ts_`.

**The handler race rule** (`lower/race.go`) is what keeps that safe. Correct
TypeScript relies on handlers never overlapping, so `hits = hits + 1` in a
handler is fine under Node and a data race here. Rather than add locks,
lowering refuses what could race, as `cannot compile yet`:

- a request handler may not touch a mutable binding declared outside it: a
  `let`, or an enclosing function's parameter. Reading is refused as well as
  writing, because the rest of the program can change the binding while
  handlers run;
- nor may it call, or pass on, a function that touches one, directly or through
  the functions it calls (a summary over the whole program's call graph,
  through imports, recursion included);
- a handler the rule cannot see into, such as a parameter or a call's result,
  is refused rather than trusted;
- so is using a function value it can't see into, a `const` holding what a
  call returned, say, called or passed on. A counter a factory returned
  compiled clean until this was added, and `go build -race` caught the race it
  ran: what the function touches lives in the factory, where no summary looks.
  A function declaration, or a `const` bound to an arrow function, is seen
  into and fine.

Reading a `const` is allowed only when it holds a primitive, an extension
type or a function: the rule's test is an allowlist (`shareableConst`), so a
`const` of any other kind counts as shared, and a kind added later is refused
until the rule names it. ⚠ **Carry-forward invariant:** a `const` object or
array is still mutable (`o.f = …`, `xs.push(…)`), so it must stay off that
list unless the rule learns to prove it unchanged. Frozen consts (M4 step 10,
below) are the one proof it has so far. Explicit shared-state tools (an atomic
counter type in `types/`, say) are how a later milestone widens it.

M4 holds objects to it (plan D10): a request handler or a spawned function
may use only the objects it creates itself, or that a function it calls
returns. One declared outside it, a `const` or a `let`, is refused, read or
written, directly or through what it calls: "use `home`, an object created
outside it; an object can be shared between requests only if it is frozen
(…), and this one isn't: property `path` isn't readonly. Freeze it, or
create it inside the handler, or in a function that returns a new one".

A **frozen** const is the exception (M4 plan D10b, `lower/frozen.go`): its
type is readonly all the way down, and its initialiser builds what it holds
from literals or other frozen consts, so nothing anywhere holds a mutable
reference to any of it. TypeScript refuses writes through it. TypeScript
does let a readonly property flow into a mutable one, though, which would
make such a reference, so lowering refuses that flow (`readonlyFlow`),
wherever a value fills a slot. A function, a promise or a host value inside
makes a const not frozen: what it captures or stands for can change.

M2 hit this invariant first, with promises. A `Promise` settles, so it is
mutable state like any object. Lowering therefore refuses a handler that
reaches a promise declared outside it, such as a top-level
`const ready: Promise<void> = …` awaited in a handler, directly or through what
it calls. The runtime assumes a promise never leaves the domain that created
it, and panics if one does rather than race. A promise a handler creates
itself is its own.

`spawn` widened it again. A `const` holding a **host value** (a request, a
response, a server) is shared state too, because the runtime's host types
aren't goroutine-safe. That is refused for handlers as well: a handler
reaching an outer `const server` was a latent M1 race. A spawned function is
held to the same rule as a handler, with its caller in place of the other
requests, so the inbox is the only thing the two domains share. Use
`const url: string = req.url` to hand a spawn what it needs.

**Accepted differences from Node** (M1 plan D5, M2 plan D6). Each is deliberate. Anything
not listed here that behaves differently is a bug.

| Behaviour | Node | cakebear |
|---|---|---|
| A CPU-heavy request handler | blocks every other request | the others are still served (8 `fib(38)` requests: ~1.3× one, vs Node's ~7.7×) |
| `console.log` order across requests | one at a time, per event-loop turn | interleaved, nondeterministic (each line whole) |
| A handler returns without `res.end()` | the request hangs until it times out | an empty 200, plus a one-line warning on stderr |
| A handler does what Node throws for (headers after they were sent, a bad status code, writing after `end`, a header name that isn't a token, a header value with a character past U+00FF or a control) | the uncaught exception kills the process | a panic with Node's message; `net/http` drops only that connection |
| A header value with spaces at either end | sent as written | trimmed, as `net/http` writes every header |
| A request line holding raw bytes from 0x80 up | a 400 | served: each such byte is one U+FFFD in `req.url` |
| Response header names | sent as written | canonicalised (`content-type` → `Content-Type`) |
| `req.url`, `req.method` | `string \| undefined` (`@types/node`) | `string`: a server request always has both |
| Reading a top-level variable before its declaration runs | `ReferenceError` | Go's zero value; for an object, a nil-pointer panic on its first field read |
| Reading an imported binding across an import cycle, before its module has run | `ReferenceError` | Go's zero value |
| A top-level `await` in a module other than the entry | the modules importing it wait for it | refused at compile time (M3 plan D8) |
| Port already in use | `EADDRINUSE` crash, exit 1 | the same message, exit 1 |
| Ctrl-C / SIGTERM | exit 130 / 143 | exit 130 / 143, after flushing output |
| The listen callback and the first request | the callback runs first | the same: serving starts after the callback |
| Async ordering within one request, or within main | synchronous code, microtasks, then timers | identical, byte for byte (`ordering.ts`, 20 of 20 runs) |
| Async code in *different* requests after an `await` | interleaved on one thread | in parallel (8 requests that await then compute `fib(38)`: ~1.2× one, vs Node's ~7.2×) |
| `setTimeout`'s return value | a `Timeout` object | a `number`, as the DOM's declaration types it |
| The cost of one `await` | ~32 ns | ~224 ns: a goroutine handoff |
| A panic in work that runs after the response (a timer set after `res.end()`) | the uncaught exception kills the process | reported on stderr (`cakebear: after a response was sent: …`); the process stays up |
| A delay over 2^31−1 ms | 1 ms, plus a `TimeoutOverflowWarning` | 1 ms, without the warning |
| A top-level `await` that never settles | exit 13, with a warning naming the file, the line and the source line | exit 13, with a warning naming only the file |
| Top-level `await` in a file with no import or export | runs: Node detects the module syntax | TS1375, as from `tsc`: add `export {}` |
| `spawn(fn)` | no such global: `ReferenceError` (and TS2304 from `tsc`) | runs `fn` in parallel (4 spawned `fib(38)` calls: ~1.1× one); a shim `spawn = fn => Promise.resolve().then(fn)` runs the same program under Node, without the parallelism |
| Reading past an array's end, or a negative or fractional index (M4 D8) | `undefined` | a panic saying so: `cakebear: index 9 is out of range for an array of length 3 (Node reads undefined)`, exit 2 |
| A string or number builtin given what Node throws for (`"ab".repeat(-1)`, `(1).toFixed(101)`, a string past V8's length limit, `decodeURIComponent("%")`, `encodeURIComponent` of a lone surrogate) | a `RangeError` or `URIError`, exit 1 | a panic with Node's message, e.g. `cakebear: repeat can't repeat a string -1 times (Node throws RangeError: Invalid count value: -1)`, exit 2 |
| `JSON.stringify` of a value that refers to itself | a `TypeError` naming the path around the circle, exit 1 | a panic with the same message: `cakebear: JSON.stringify met a cycle (Node throws TypeError: Converting circular structure to JSON …)`, exit 2 |
| Writing past `length`, leaving a hole, or a negative or fractional index (D9) | a hole, or a property of that name | a panic saying so |
| Growing `length` (D9) | holes | a panic; shrinking is allowed |
| A callback that shrinks its array during `map`, `filter`, `forEach`, `some`, `every`, `reduce` or `findIndex` (D9) | skips the indices it removed (`findIndex` reads them as `undefined`) | a panic saying so: `cakebear: the array shrank to 2 elements while forEach ran over 4 (Node skips the rest)` |
| The calls `sort`'s comparator gets | V8's TimSort pairs | a Go stable sort's: the same result for any consistent comparator, but a comparator that logs or counts sees other calls (`[3, 1, 2]`: Node calls it 4 times, cakebear 3) |
| A `spawn`'s object or array result, while the spawn has work left (D11) | the same object | a copy, aliasing and cycles kept, so the spawn's leftover work can't change it in parallel |
| `i32(a / b)` or `i32(a % b)` with `b` zero, and the same for the other integer types | no such conversion; the shim `i32 = x => x \| 0` gives 0 | a panic saying so: `cakebear: i32(a / b) with b zero: as a number it is Infinity, or NaN for 0 / 0, which i32 can't hold`, exit 2 |

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
