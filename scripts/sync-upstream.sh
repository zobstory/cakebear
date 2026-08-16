#!/usr/bin/env bash
# Merge microsoft/typescript-go into cakebear.
#
# This works only because of the golden rule: we never modify an upstream .go
# file. If that holds, the only conflicts possible are the import-path lines
# this fork rewrites, and "take upstream's side, then re-run the rename" is
# always the correct resolution. A conflict in a file cakebear owns means the
# rule was broken somewhere, so the script stops and hands it to you.
#
# Usage: scripts/sync-upstream.sh [ref]        (default: upstream/main)
set -euo pipefail

REF="${1:-upstream/main}"

cd "$(dirname "$0")/.."

# Paths cakebear owns. Everything else belongs to upstream.
OURS_RE='^(cmd/cakec/|ir/|backend/|runtime/|types/|scripts/|CLAUDE\.md|README\.md|\.github/workflows/cakebear-)'

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "working tree is dirty; commit or stash first" >&2
  exit 1
fi

git fetch upstream --no-tags
git merge --no-commit --no-ff "$REF" || true

conflicts=$(git diff --name-only --diff-filter=U || true)
if [ -n "$conflicts" ]; then
  ours=$(printf '%s\n' "$conflicts" | grep -E "$OURS_RE" || true)
  if [ -n "$ours" ]; then
    echo "conflicts in files cakebear owns — resolve these by hand, then commit:" >&2
    printf '%s\n' "$ours" | sed 's/^/  /' >&2
    exit 1
  fi
  # Upstream-owned conflicts are import lines. Take their side wholesale;
  # rename-module.sh re-applies our module path immediately after.
  printf '%s\n' "$conflicts" | tr '\n' '\0' | xargs -0 git checkout --theirs --
  printf '%s\n' "$conflicts" | tr '\n' '\0' | xargs -0 git add --
fi

scripts/rename-module.sh

# Build before committing: a broken merge stays staged for inspection
# rather than landing on the branch.
go build ./...

git add -A
git commit -m "Merge upstream $REF"
echo "merged $REF — review with: git show --stat HEAD"
