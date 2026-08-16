#!/usr/bin/env bash
# Print the import paths of the Go packages cakebear owns, one per line.
#
# Upstream's tree dwarfs ours, so build, test, coverage and lint all need to be
# scoped to our own code -- otherwise the coverage denominator is Microsoft's
# and the number means nothing.
#
# Prints nothing (and exits 0) when no owned packages exist yet: the phases
# land cmd/cakec, ir, backend, runtime and types one at a time, and CI should
# stay green in between.
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=scripts/lib-owned.sh
. scripts/lib-owned.sh
owned_load

module=$(go list -m)

# `go list ./...` walks the whole fork, so filter its output rather than
# passing it directory patterns that may not exist yet.
go list ./... 2>/dev/null | while IFS= read -r pkg; do
  rel="${pkg#"$module"/}"
  [ "$rel" = "$pkg" ] && continue # the root package itself
  is_owned "$rel/" && echo "$pkg"
done || true
