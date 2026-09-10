#!/usr/bin/env bash
# Keep ir/ and backend/ extractable.
#
# The IR and the Go backend are written so they could be lifted into a
# standalone module -- github.com/zobstory/buildbinary was the working name --
# without untangling anything. That is not a plan so much as an option worth
# keeping cheap, and it buys something concrete either way: the Go emitter can
# be swapped for an LLVM one without the frontend noticing.
#
# The property is narrow and mechanical:
#
#   ir/       imports only the standard library
#   backend/  imports only ir/ and the standard library
#
# Neither may reach into the forked TypeScript compiler. lower/ exists to
# absorb that coupling, and is the only package that sees both worlds.
#
# We enforce this with a check rather than an actual module split. A separate
# module would prove the same property, at the cost of a tag in one repo and a
# version bump in the other every time the IR changes shape -- which, while the
# IR is still young, is often.
set -euo pipefail

cd "$(dirname "$0")/.."

MODULE=$(go list -m)
FAILED=0

# report prints a violation as both a GitHub annotation and a readable line.
report() {
  local file="$1" imported="$2" reason="$3"
  echo "::error file=$file::$reason ($imported)"
  echo "  $file imports $imported" >&2
  FAILED=1
}

# imports_of lists the import paths of a Go file, ignoring test files: tests may
# reach for anything, and it is the shipped code whose dependencies matter.
imports_of() {
  go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' "$1" 2>/dev/null || true
}

check_pkg() {
  local dir="$1" allow_ir="$2"
  [ -d "$dir" ] || return 0

  local pkg
  while IFS= read -r pkg; do
    [ -n "$pkg" ] || continue
    local imported
    while IFS= read -r imported; do
      [ -n "$imported" ] || continue
      case "$imported" in
        "$MODULE"/internal/*)
          report "$dir" "$imported" "$dir must not import the forked compiler"
          ;;
        "$MODULE"/ir | "$MODULE"/ir/*)
          if [ "$allow_ir" != "yes" ]; then
            report "$dir" "$imported" "ir/ must not import anything of ours"
          fi
          ;;
        "$MODULE"/*)
          report "$dir" "$imported" "$dir must not depend on the rest of cakebear"
          ;;
      esac
    done < <(imports_of "$pkg")
  done < <(go list ./"$dir"/... 2>/dev/null || true)
}

# ir/ is the stricter of the two: pure data, standard library only.
check_pkg ir no
# backend/ may use ir/, and nothing else of ours.
check_pkg backend yes

if [ "$FAILED" -ne 0 ]; then
  {
    echo
    echo "ir/ and backend/ are meant to be liftable into their own module."
    echo "Anything needing the TypeScript frontend belongs in lower/, which is"
    echo "the package that exists to see both worlds."
  } >&2
  exit 1
fi

echo "OK: ir/ imports only the standard library; backend/ imports only ir/ and the standard library."
