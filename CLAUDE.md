# cakebear

A compiler that takes **TypeScript** and produces a **single native binary**.

cakebear is a permanent fork of [microsoft/typescript-go][tsgo]. Their frontend —
scanner, parser, binder, checker — is ours by merge. Where upstream emits
JavaScript, we lower to Go and shell out to `go build`.

> **Note:** `README.md` is from late 2023 and describes a Rust implementation
> that no longer exists. Trust this file over the README until the README is
> rewritten. Full migration plan and rationale:
> <https://claude.ai/code/artifact/485fa63c-f984-4648-87a8-bb0b9ddbb4aa>

[tsgo]: https://github.com/microsoft/typescript-go

## The golden rule

**Never modify an upstream `.go` file.**

A fork's maintenance cost is directly proportional to its diff against
upstream, and upstream is actively developed. Every cakebear behaviour goes in
a new file under a path listed in `scripts/owned-paths`. In particular
`cmd/cakec` constructs its own `Program` from their packages and runs its own
emit — it never routes through `internal/execute`, precisely so that hooking
the pipeline requires no edit inside their tree.

Hold this line and the only merge conflicts possible are import-path lines,
which `scripts/sync-upstream.sh` resolves mechanically. Break it once and every
future sync gets more expensive. CI enforces it (`golden-rule` job); the check
is `scripts/check-upstream-untouched.sh`.

If you genuinely need upstream behaviour to differ, that is a discussion before
it is a diff.

## Locked architectural decisions

- **Relationship to upstream:** hard fork, not a dependency. Forced by Go's
  `internal/` rule — everything we need lives under `internal/`, so it cannot
  be imported across modules, and upstream marks its public API as not ready.
  Inside the module it is all available.
- **Module path:** `github.com/zobstory/cakebear`. Rewritten from upstream's on
  every sync by `scripts/rename-module.sh`.
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
├── cmd/tsgo/       UPSTREAM  their CLI — keep it working, it is a free
│                             regression check that a sync didn't break the frontend
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
go run ./cmd/tsgo --version           # upstream CLI still works
bin/cakec build examples/basic/main.ts
bin/cakec build --checkers 8 --files-from list.txt   # large corpora
scripts/bench-checkers.sh <dir> <limit> 1 2 4 8      # scaling baseline

scripts/owned-go-packages.sh          # the packages that are ours
go test $(scripts/owned-go-packages.sh)

python3 scripts/check-line-limit.py           # ≤500 lines per owned .go file
scripts/check-upstream-untouched.sh main      # the golden rule
scripts/check-boundary.sh                     # ir/ and backend/ stay liftable
scripts/check-workflows.sh                    # our workflow YAML actually parses
# Relative patterns, not owned-go-packages.sh: that prints import paths, which
# golangci-lint cannot resolve -- it reports "0 issues" having linted nothing.
golangci-lint run --config .golangci.cakebear.yml \
  ./cmd/cakec/... ./lower/... ./types/... ./ir/... ./backend/...
```

**Always build binaries into `bin/`.** A bare `go build ./cmd/cakec` drops a
`cakec` executable at the repo root, which is not ignored — and we cannot add it
to `.gitignore`, because that is an upstream file. `/bin` is already in
upstream's ignore list, so `-o bin/` is the convention that needs no edit to
their tree. (The `golden-rule` gate catches this if you forget; it already has
once.)

`_submodules/TypeScript` is upstream's conformance corpus. Clone without
`--recurse-submodules` unless you need it.

## Upstream sync

```sh
scripts/sync-upstream.sh              # merges upstream/main, rewrites module path
```

It takes upstream's side of every conflicted file and re-runs the module rename
— correct only because of the golden rule. If it finds a conflict in a file we
own, it stops and hands it to you rather than guessing.

Lint uses `.golangci.cakebear.yml`, not upstream's `.golangci.yml`. Theirs
enables `customlint`, a plugin that only exists in a binary built from their
`.custom-gcl.yml`, so pointing at it fails before linting a single file. Ours is
their linter set minus that plugin, minus `depguard` (their import graph) and
minus `modernize` (absent from the pinned version). The golangci-lint version is
pinned in CI so it matches what gets run locally.

One trap worth knowing: upstream's `.gitignore` has a blanket `*.txt` rule, and
`scripts/owned-paths` went untracked for five phases when it was named
`owned-paths.txt`. Every guardrail script reads that manifest, so a fresh
checkout would have failed all of them. We cannot add a negation — `.gitignore`
is an upstream file — so the manifest is deliberately extensionless. Run
`git status --ignored` before assuming a new file is committed.

`.github/workflows/cakebear-sync-upstream.yml` runs this weekly and opens a
labelled PR. Keep the cadence: weekly syncs conflict for minutes, six-monthly
syncs conflict for a day and eventually get abandoned.

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
`_submodules/TypeScript` is the obvious candidate and is not cloned. Copying
`.d.ts` files to `.ts` does not work as a substitute: most of their content
(ambient declarations, bodyless overloads) is not legal in a `.ts` file, which
is why workload B is 491 files rather than 3000.

## Current phase: **open**

P0–P5 are done and nothing is blocked. Reasonable next steps, none urgent:

- Widen the language subset. `null`/`undefined` need a nullable representation
  in the IR; arrays and objects need an aggregate one. Both are prerequisites
  for most real programs.
- Multi-file programs, which need an import graph in the IR.
- `tsconfig.json` support, so `cakec` can build a real project.
- Push. Nothing has been pushed to GitHub yet, and upstream's `ci.yml` will fire
  its full matrix on the first push unless Actions is disabled first.

Completed: **P0** fork established, **P1** guardrails and sync, **P2** `cakec`
type-checks TypeScript, **P3** parallelism exposed and proven deterministic,
**P4** IR, Go emission and native binaries, **P5** cakebear's type extensions.

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
`console.log` on one primitive argument; calls to top-level functions.

Not yet: `null`/`undefined` as values (no nullable representation), mixed-type
`+`, `var`, loose equality, arrays, objects, classes, generics, imports, `for`,
multi-file programs, bitwise operators, `async`.

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

**3. `async`/`await` maps to goroutines, genuinely in parallel.** JS async is
concurrent but never parallel, so correct TypeScript cannot race; goroutines
can. We take the parallel mapping: it is what the M:N scheduler note always
implied, and pretending to be an event loop would throw away most of the
performance that justifies compiling TypeScript at all.

This makes cakebear a language that differs from TypeScript at runtime, which
the README's compatibility promise currently rules out. **That promise is the
thing to amend**, and it should be amended before anyone writes async code
against cakebear rather than after.

*Live in Phase 1:* nothing, but the IR must not assume single-threaded
execution anywhere.

Related: because `number` is `float64`, integer-heavy TypeScript will run
slower than equivalent Go. The refined numeric types (`i32`, `u64`) lowering
straight to Go's are the escape hatch, which makes them a performance feature
rather than an ergonomic one.

## Per-phase planning

cakebear is multi-year and will involve many contributors. Plan only the
**current** phase in detail; defer later phases until they are the active
phase. Don't write speculative roadmaps for things that aren't being built yet.
