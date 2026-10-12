#!/usr/bin/env bash
# Enforce the golden rule: a change may not touch an upstream file.
#
# cakebear is a fork of microsoft/TypeScript's tsc/. A sync replaces every
# upstream file with the new snapshot, so a change made inside their tree is
# silently lost on the next one. Every cakebear behaviour goes in a new file
# under a path listed in scripts/owned-paths. Hooking the pipeline never
# requires editing their tree -- cmd/cakec builds its own
# Program rather than routing through internal/execute, precisely so that
# stays true.
#
# Upstream syncs legitimately touch upstream files. scripts/sync-upstream.sh
# produces those; CI holds them to scripts/check-upstream-snapshot.sh instead.
#
# Usage: scripts/check-upstream-untouched.sh [base-ref]   (default: origin/main)
set -euo pipefail

BASE="${1:-origin/main}"

cd "$(dirname "$0")/.."
# shellcheck source=scripts/lib-owned.sh
. scripts/lib-owned.sh
owned_load

if ! git rev-parse --verify --quiet "$BASE" >/dev/null; then
  echo "base ref '$BASE' not found; fetch it first" >&2
  exit 1
fi

changed=$(git diff --name-only "$BASE"...HEAD)
if [ -z "$changed" ]; then
  echo "OK: no files changed against $BASE."
  exit 0
fi

violations=()
while IFS= read -r file; do
  is_owned "$file" || violations+=("$file")
done <<<"$changed"

if [ ${#violations[@]} -eq 0 ]; then
  echo "OK: $(printf '%s\n' "$changed" | wc -l | tr -d ' ') changed file(s), all cakebear-owned."
  exit 0
fi

for file in "${violations[@]}"; do
  echo "::error file=$file::upstream file modified; cakebear code belongs in a path listed in scripts/owned-paths"
done

{
  echo
  echo "${#violations[@]} upstream file(s) modified:"
  printf '  %s\n' "${violations[@]}"
  echo
  echo "cakebear must not edit upstream's tree. Put the change in a path listed"
  echo "in scripts/owned-paths (cmd/cakec, lower, ir, backend, types) instead."
  echo "If you genuinely need upstream behaviour to differ, that is a discussion"
  echo "before it is a diff."
} >&2

exit 1
