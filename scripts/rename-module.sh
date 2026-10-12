#!/usr/bin/env bash
# Rewrite upstream's Go module path to cakebear's, everywhere.
#
# Idempotent and safe to re-run. scripts/sync-upstream.sh runs it on the
# snapshot it lays down, and scripts/check-upstream-snapshot.sh runs it on a
# fresh copy of the same snapshot, so the two cannot disagree about what a
# renamed upstream file looks like.
#
# Usage: scripts/rename-module.sh [dir]        (default: the repo root)
set -euo pipefail

FROM="github.com/microsoft/TypeScript/tsc"
TO="github.com/zobstory/cakebear"

cd "${1:-$(dirname "$0")/..}"

list=$(mktemp)
trap 'rm -f "$list"' EXIT

# testdata/ is excluded on purpose: it is upstream's expected test output, not
# part of our import graph, and rewriting it would only churn baselines.
grep -rl --include='*.go' -F "$FROM" . \
  --exclude-dir=testdata \
  --exclude-dir=.git >"$list" || true

n=$(wc -l <"$list" | tr -d ' ')
if [ "$n" -gt 0 ]; then
  # perl, not `sed -i`: BSD sed (macOS) and GNU sed (CI) disagree about the
  # backup-suffix argument, and the difference corrupts files silently.
  tr '\n' '\0' <"$list" | xargs -0 perl -pi -e "s|\Q$FROM\E|$TO|g"
  # The new path sorts to a different position inside import blocks;
  # gofmt re-sorts them so goimports linting stays clean. It has to be the
  # gofmt of the toolchain go.mod selects, not whichever is on PATH: an older
  # gofmt cannot parse newer syntax (Go 1.27's generic methods failed under
  # a 1.26 gofmt), and `go env` here resolves go.mod's toolchain.
  gofmt="$(go env GOROOT)/bin/gofmt"
  tr '\n' '\0' <"$list" | xargs -0 "$gofmt" -w
fi

go mod edit -module "$TO"

echo "module path $FROM -> $TO ($n Go file(s) rewritten)"
