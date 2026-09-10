#!/usr/bin/env bash
# Measure how cakec's type checking scales with --checkers.
#
# The number that matters is the ratio, not the absolute time: this establishes
# the baseline every later phase is measured against, so the same corpus and
# the same machine have to be used when comparing.
#
# Usage:
#   scripts/bench-checkers.sh <dir-of-ts-files> [file-limit] [counts...]
#
# Example:
#   scripts/bench-checkers.sh ~/some-project/node_modules 2000 1 2 4 8
set -euo pipefail

cd "$(dirname "$0")/.."

DIR="${1:?usage: bench-checkers.sh <dir> [file-limit] [counts...]}"
LIMIT="${2:-2000}"
[ $# -ge 1 ] && shift
[ $# -ge 1 ] && shift
COUNTS=("$@")
if [ ${#COUNTS[@]} -eq 0 ]; then
  COUNTS=(1 2 4 8)
fi

if [ ! -x bin/cakec ]; then
  echo "building bin/cakec..." >&2
  go build -o bin/cakec ./cmd/cakec
fi

# A file list rather than a glob: several thousand paths on one command line
# runs into ARG_MAX, and xargs would split the run into separate programs,
# measuring process startup instead of type checking. cakec reads the list
# directly via --files-from, so the whole corpus is one program.
list=$(mktemp)
trap 'rm -f "$list"' EXIT
# `head` closing the pipe early makes `find` die of SIGPIPE, which under
# `set -o pipefail` would abort the whole script. The subshell contains that.
( find "$DIR" \( -name '*.ts' -o -name '*.tsx' \) -type f 2>/dev/null || true ) | head -n "$LIMIT" >"$list" || true

files=$(wc -l <"$list" | tr -d ' ')
if [ "$files" -eq 0 ]; then
  echo "no .ts files found under $DIR" >&2
  exit 1
fi

echo "corpus:  $files files from $DIR"
echo "machine: $(uname -sm), $(getconf _NPROCESSORS_ONLN) cores"
echo

# Warm the filesystem cache and the Go build cache so the first measured run
# is not paying for both.
./bin/cakec build --no-color --files-from "$list" --checkers 1 >/dev/null 2>&1 || true

printf '%-10s %10s %10s\n' "checkers" "seconds" "speedup"
baseline=""
for n in "${COUNTS[@]}"; do
  start=$(date +%s.%N)
  ./bin/cakec build --no-color --files-from "$list" --checkers "$n" >/dev/null 2>&1 || true
  end=$(date +%s.%N)
  elapsed=$(echo "$end - $start" | bc)
  [ -z "$baseline" ] && baseline="$elapsed"
  speedup=$(echo "scale=2; $baseline / $elapsed" | bc)
  printf '%-10s %10.2f %9sx\n' "$n" "$elapsed" "$speedup"
done
