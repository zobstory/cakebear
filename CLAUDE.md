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
a new file under a path listed in `scripts/owned-paths.txt`. In particular
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
  the Go emitter for LLVM if beating Go ever matters) and the precondition for
  extracting `backend/` to a standalone `github.com/zobstory/buildbinary`.

## Repo layout

Upstream's tree, plus five directories upstream will never create:

```
cakebear/
├── internal/       UPSTREAM  scanner, parser, binder, checker, program, LSP
├── cmd/tsgo/       UPSTREAM  their CLI — keep it working, it is a free
│                             regression check that a sync didn't break the frontend
├── cmd/cakec/      OURS      the cakebear CLI
├── ir/             OURS      lowered IR: the contract between their checked AST
│                             and our backend
├── backend/        OURS      IR → Go source → `go build` → executable
├── runtime/        OURS      support code linked into compiled output
├── types/          OURS      cakebear type extensions (base64, refined numerics)
└── scripts/        OURS      fork maintenance and CI gates
```

`ir/` and `backend/` sit at top level rather than under `internal/` on purpose:
that is what makes the eventual extraction to `buildbinary` a file move rather
than a visibility rewrite.

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

scripts/owned-go-packages.sh          # the packages that are ours
go test $(scripts/owned-go-packages.sh)

python3 scripts/check-line-limit.py           # ≤500 lines per owned .go file
scripts/check-upstream-untouched.sh main      # the golden rule
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

`.github/workflows/cakebear-sync-upstream.yml` runs this weekly and opens a
labelled PR. Keep the cadence: weekly syncs conflict for minutes, six-monthly
syncs conflict for a day and eventually get abandoned.

## Current phase: **Phase 3 — expose the parallelism**

Wire `--checkers` through `cakec` (the option already exists as
`core.CompilerOptions.Checkers`), benchmark on a real multi-thousand-file
codebase to establish the baseline every later phase is measured against, and
verify diagnostic output is byte-identical at 1 and 8 workers. Nondeterministic
error ordering is the classic bug here and it bites CI, not you.

Completed: **P0** fork established, **P1** guardrails and sync, **P2** `cakec`
type-checks TypeScript end to end.

## Open questions — answer before writing the emitter

Emitting Go means TypeScript's runtime semantics get expressed in Go's, and
three places they disagree need decisions recorded here before Phase 4, not
during it:

1. **Strings.** JS strings are UTF-16 code units (`"日本".length` is 2); Go
   strings are UTF-8 bytes (`len("日本")` is 6). A naive mapping silently breaks
   `.length`, indexing and `slice` on non-ASCII input. Leaning toward UTF-8
   underneath with runtime helpers carrying an ASCII fast path.
2. **Numbers.** `number` lowers to `float64`, but JS bitwise operators coerce to
   int32 first: `a | b` must lower to `float64(int32(a) | int32(b))`, and `>>>`
   to the uint32 form.
3. **`async`/`await`.** JS async is concurrent but never parallel, so correct
   TypeScript cannot race. Goroutines are genuinely parallel. Taking the
   parallel mapping makes cakebear a language that differs from TypeScript at
   runtime — which the README's compatibility promise currently rules out, and
   that promise is the thing to amend.

Related: because `number` is `float64`, integer-heavy TypeScript will run
slower than equivalent Go. The refined numeric types (`i32`, `u64`) lowering
straight to Go's are the escape hatch, which makes them a performance feature
rather than an ergonomic one.

## Per-phase planning

cakebear is multi-year and will involve many contributors. Plan only the
**current** phase in detail; defer later phases until they are the active
phase. Don't write speculative roadmaps for things that aren't being built yet.
