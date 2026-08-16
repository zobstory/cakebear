#!/usr/bin/env bash
# Rewrite upstream's Go module path to cakebear's, everywhere.
#
# Idempotent and safe to re-run. That property is load-bearing:
# scripts/sync-upstream.sh resolves import-line merge conflicts by taking
# upstream's side of a file wholesale and then re-running this script.
set -euo pipefail

FROM="github.com/microsoft/typescript-go"
TO="github.com/zobstory/cakebear"

cd "$(dirname "$0")/.."

list=$(mktemp)
trap 'rm -f "$list"' EXIT

# testdata/ and _submodules/ are excluded on purpose: those are upstream's
# expected test output and a vendored TypeScript checkout, not part of our
# import graph. Rewriting them would only churn baselines.
grep -rl --include='*.go' -F "$FROM" . \
  --exclude-dir=testdata \
  --exclude-dir=_submodules \
  --exclude-dir=.git >"$list" || true

n=$(wc -l <"$list" | tr -d ' ')
if [ "$n" -gt 0 ]; then
  # perl, not `sed -i`: BSD sed (macOS) and GNU sed (CI) disagree about the
  # backup-suffix argument, and the difference corrupts files silently.
  tr '\n' '\0' <"$list" | xargs -0 perl -pi -e "s|\Q$FROM\E|$TO|g"
  # The new path sorts to a different position inside import blocks;
  # gofmt re-sorts them so goimports linting stays clean.
  tr '\n' '\0' <"$list" | xargs -0 gofmt -w
fi

go mod edit -module "$TO"

if [ -f .golangci.yml ]; then
  perl -pi -e "s|\Q$FROM\E|$TO|g" .golangci.yml
fi

echo "module path $FROM -> $TO ($n Go file(s) rewritten)"
