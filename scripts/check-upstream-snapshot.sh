#!/usr/bin/env bash
# Prove cakebear's upstream tree is exactly the snapshot scripts/upstream-ref
# names: every file that isn't cakebear-owned must be byte-identical to
# microsoft/TypeScript's tsc/ at that commit, after the module rename, with
# none missing and none extra.
#
# This is the golden rule's check for sync PRs. They change upstream files by
# design, so check-upstream-untouched.sh cannot judge them; this can, and it is
# stricter: an edit to an upstream file anywhere fails it, sync or not.
#
# Usage: scripts/check-upstream-snapshot.sh
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=scripts/lib-owned.sh
. scripts/lib-owned.sh
# shellcheck source=scripts/lib-upstream.sh
. scripts/lib-upstream.sh
owned_load

ref=$(tr -d '[:space:]' <scripts/upstream-ref)
if ! printf '%s' "$ref" | grep -Eq '^[0-9a-f]{40}$'; then
  echo "scripts/upstream-ref does not hold a commit SHA: '$ref'" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
upstream_fetch "$ref" "$tmp" >/dev/null
upstream_tree "$tmp"
scripts/rename-module.sh "$tmp/tree" >/dev/null

(cd "$tmp/tree" && find . -type f | sed 's|^\./||' | LC_ALL=C sort) >"$tmp/want"
git ls-files | grep -vE "$(owned_regex)" | LC_ALL=C sort >"$tmp/have"

missing=$(LC_ALL=C comm -23 "$tmp/want" "$tmp/have")
extra=$(LC_ALL=C comm -13 "$tmp/want" "$tmp/have")

# Content, by checksum in batches: one cmp per file is ~55,000 processes.
LC_ALL=C comm -12 "$tmp/want" "$tmp/have" >"$tmp/both"
checksums() { tr '\n' '\0' <"$tmp/both" | xargs -0 shasum -a 256 | awk '{print $1}'; }
(cd "$tmp/tree" && checksums) >"$tmp/want.sum"
checksums >"$tmp/have.sum"
differ=$(paste "$tmp/both" "$tmp/want.sum" "$tmp/have.sum" | awk -F'\t' '$2 != $3 {print $1}')

if [ -z "$missing$extra$differ" ]; then
  echo "OK: $(wc -l <"$tmp/want" | tr -d ' ') upstream file(s) match microsoft/TypeScript@${ref:0:9}:tsc/."
  exit 0
fi
report() {
  [ -z "$2" ] && return 0
  echo "$1 ($(printf '%s\n' "$2" | wc -l | tr -d ' ')):" >&2
  printf '%s\n' "$2" | head -20 | sed 's/^/  /' >&2
}
report "in the snapshot but missing here" "$missing"
report "upstream-looking files the snapshot doesn't have (own them in scripts/owned-paths, or delete them)" "$extra"
report "changed from the snapshot (the golden rule: never edit an upstream file)" "$differ"
exit 1
