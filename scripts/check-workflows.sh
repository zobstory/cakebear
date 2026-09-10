#!/usr/bin/env bash
# Parse every workflow file we own.
#
# GitHub rejects a malformed workflow with a run that has zero jobs and no
# useful message, which is a slow and confusing way to find a typo. This catches
# it before the push.
#
# The specific bug that motivated this: a heredoc nested inside a `run: |` block
# scalar. The heredoc body sits at column 1, which ends the block scalar, so
# YAML reads the next line as a new top-level key. It looks completely fine to
# a human and to a grep-based smoke test.
set -euo pipefail

cd "$(dirname "$0")/.."

if ! command -v ruby >/dev/null 2>&1; then
  echo "ruby not found; skipping workflow validation" >&2
  exit 0
fi

failed=0
for f in .github/workflows/cakebear-*.yml; do
  [ -e "$f" ] || continue
  if err=$(ruby -ryaml -e '
    begin
      doc = YAML.load_file(ARGV[0])
      raise "not a mapping" unless doc.is_a?(Hash)
      # GitHub parses `on:` as the boolean true unless quoted, so accept either.
      raise "no jobs" unless doc["jobs"].is_a?(Hash)
      raise "no triggers" unless doc.key?("on") || doc.key?(true)
    rescue => e
      warn e.message
      exit 1
    end' "$f" 2>&1); then
    echo "OK: $f"
  else
    echo "::error file=$f::$err"
    echo "  $f: $err" >&2
    failed=1
  fi
done

exit "$failed"
