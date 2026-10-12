# Shared upstream snapshot fetching for sync-upstream.sh and
# check-upstream-snapshot.sh. Source this, don't execute it.
#
# Upstream is the Go compiler inside microsoft/TypeScript, under tsc/. It was
# microsoft/typescript-go until that repository was archived on 2026-08-20 and
# its code, with its history rewritten under tsc/, moved into the TypeScript
# repo. cakebear keeps that module at its own root.

# shellcheck shell=bash

UPSTREAM_URL="${UPSTREAM_URL:-https://github.com/microsoft/TypeScript.git}"

# UPSTREAM_DIR is the Go module inside the monorepo.
UPSTREAM_DIR="tsc"

# UPSTREAM_ROOT_FILES are monorepo-root files cakebear carries as upstream
# files: the license notice has to travel with the code it covers.
UPSTREAM_ROOT_FILES="NOTICE.txt"

# upstream_fetch <ref> <dest> fetches <ref> into <dest>/repo, checking out only
# tsc/ and the monorepo's root files, and prints the commit's full SHA. A
# shallow, blob-filtered fetch: the full TypeScript history runs to gigabytes.
upstream_fetch() {
  local ref="$1" dest="$2"
  git init -q "$dest/repo"
  git -C "$dest/repo" remote add origin "$UPSTREAM_URL"
  git -C "$dest/repo" sparse-checkout set "$UPSTREAM_DIR" >/dev/null
  git -C "$dest/repo" fetch -q --depth 1 --filter=blob:none origin "$ref"
  git -C "$dest/repo" -c advice.detachedHead=false checkout -q FETCH_HEAD
  git -C "$dest/repo" rev-parse FETCH_HEAD
}

# upstream_tree <dest> lays the snapshot out in <dest>/tree as cakebear's root
# holds it: tsc/'s contents, plus UPSTREAM_ROOT_FILES.
upstream_tree() {
  local dest="$1" f
  mkdir -p "$dest/tree"
  (cd "$dest/repo/$UPSTREAM_DIR" && tar cf - .) | (cd "$dest/tree" && tar xf -)
  for f in $UPSTREAM_ROOT_FILES; do
    cp "$dest/repo/$f" "$dest/tree/$f"
  done
}
