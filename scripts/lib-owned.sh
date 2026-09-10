# Shared ownership matching for cakebear's guardrail scripts.
# Source this, don't execute it. Expects the caller to have cd'd to the repo root.
#
# The manifest is scripts/owned-paths; see CLAUDE.md, "The golden rule".

# shellcheck shell=bash

_OWNED_PATTERNS=""

owned_load() {
  local manifest="scripts/owned-paths"
  if [ ! -f "$manifest" ]; then
    echo "$manifest not found; run from the repo root" >&2
    return 1
  fi
  _OWNED_PATTERNS=$(grep -vE '^[[:space:]]*(#|$)' "$manifest")
  if [ -z "$_OWNED_PATTERNS" ]; then
    echo "$manifest lists no paths" >&2
    return 1
  fi
}

# is_owned <path> -- true if the path belongs to cakebear rather than upstream.
# A manifest entry ending in "/" matches that directory and everything under
# it; any other entry must match the path exactly.
is_owned() {
  local file="$1" pattern
  while IFS= read -r pattern; do
    case "$pattern" in
      */) [ "${file##"$pattern"}" != "$file" ] && return 0 ;;
      *)  [ "$file" = "$pattern" ] && return 0 ;;
    esac
  done <<<"$_OWNED_PATTERNS"
  return 1
}
