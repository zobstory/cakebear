#!/usr/bin/env bash
# Sync cakebear's upstream tree to a snapshot of microsoft/TypeScript's tsc/.
#
# A sync is a snapshot, not a merge. Every upstream file is replaced by the
# snapshot's, and every upstream file the snapshot no longer has is deleted.
# That is correct only because of the golden rule: cakebear never edits an
# upstream file, so upstream's tree holds nothing of ours to lose. Paths in
# scripts/owned-paths are never touched, and the script stops if upstream ships
# a file at one of them.
#
# Why not a merge: microsoft/typescript-go was archived in August 2026 and its
# history moved into microsoft/TypeScript rewritten under tsc/, so the two
# share no commit a merge could start from. A snapshot needs no shared history.
#
# The commit synced to is recorded in scripts/upstream-ref;
# scripts/check-upstream-snapshot.sh proves the tree still matches it.
#
# Usage: scripts/sync-upstream.sh [ref]        (default: main)
set -euo pipefail

REF="${1:-main}"

cd "$(dirname "$0")/.."
# shellcheck source=scripts/lib-owned.sh
. scripts/lib-owned.sh
# shellcheck source=scripts/lib-upstream.sh
. scripts/lib-upstream.sh
owned_load

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "working tree is dirty; commit or stash first" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
sha=$(upstream_fetch "$REF" "$tmp")
upstream_tree "$tmp"

owned=$(owned_regex)
collisions=$(cd "$tmp/tree" && find . -type f | sed 's|^\./||' | grep -E "$owned" || true)
if [ -n "$collisions" ]; then
  echo "upstream ships files at paths cakebear owns; resolve these by hand:" >&2
  printf '%s\n' "$collisions" | sed 's/^/  /' >&2
  exit 1
fi

# Remove every upstream file, then lay the snapshot down in its place.
git ls-files | grep -vE "$owned" | tr '\n' '\0' | xargs -0 git rm -q --
# A removed git submodule leaves its directory behind, empty.
find . -mindepth 1 -type d -empty -not -path './.git' -not -path './.git/*' -delete
(cd "$tmp/tree" && tar cf - .) | tar xf -
scripts/rename-module.sh
# cmd/cakec runs copies of cmd/tsc's files; refresh them from the new snapshot.
scripts/copy-tsc-cmd.sh

git add -A
if git diff --cached --quiet; then
  echo "upstream tsc/ at ${sha:0:9} matches cakebear's tree; nothing to sync"
  exit 0
fi
printf '%s\n' "$sha" >scripts/upstream-ref
git add scripts/upstream-ref

# Build before committing: a sync that breaks cakebear's own code stays
# staged for inspection rather than landing on the branch.
go build ./...

git commit -q -m "Sync upstream to microsoft/TypeScript@${sha:0:9} (tsc/)"
echo "synced to microsoft/TypeScript@$sha; review with: git show --stat HEAD"
