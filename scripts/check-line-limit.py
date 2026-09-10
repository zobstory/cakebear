#!/usr/bin/env python3
"""Enforce a 500-line maximum on cakebear's own Go source files.

A guardrail to keep modules focused. If a file legitimately wants more
lines, split it into more files rather than raising the limit.

Only files under paths listed in scripts/owned-paths are checked --
upstream's tree runs well past 500 lines in many places and is not ours
to split. See CLAUDE.md, "The golden rule".

Run from the repo root:
    python3 scripts/check-line-limit.py

Exits 0 if all files are within the limit, 1 otherwise. In GitHub
Actions, emits `::error` annotations so violations appear inline in
the PR diff.
"""
from __future__ import annotations

import sys
from pathlib import Path

LIMIT = 500
SUFFIXES = {".go"}
OWNED = Path("scripts/owned-paths")


def owned_dirs() -> list[Path]:
    """Directory prefixes from the ownership manifest.

    Exact-file entries (CLAUDE.md and friends) are skipped: they hold no
    Go source, so the suffix filter would drop them anyway.
    """
    if not OWNED.exists():
        print(f"error: {OWNED} not found; run from the repo root", file=sys.stderr)
        sys.exit(2)

    dirs = []
    for raw in OWNED.read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or not line.endswith("/"):
            continue
        dirs.append(Path(line))
    return dirs


def count_lines(path: Path) -> int:
    with path.open("rb") as f:
        return sum(1 for _ in f)


def main() -> int:
    roots = owned_dirs()
    violations: list[tuple[Path, int]] = []
    checked = 0

    # Test files count too. A 600-line table-driven test is still a file
    # that wants splitting, and exempting them is how the limit erodes.
    for root in roots:
        if not root.exists():
            continue  # Phases land these directories one at a time.
        for path in sorted(root.rglob("*")):
            if not path.is_file() or path.suffix not in SUFFIXES:
                continue
            checked += 1
            n = count_lines(path)
            if n > LIMIT:
                violations.append((path, n))

    if not violations:
        roots_repr = ", ".join(str(r) for r in roots)
        print(f"OK: {checked} cakebear-owned .go file(s) within {LIMIT} lines.")
        print(f"    roots: {roots_repr}")
        return 0

    # GitHub Actions annotations (no-op outside Actions).
    for path, n in violations:
        print(f"::error file={path},line={LIMIT}::file has {n} lines, limit is {LIMIT}")

    print(f"\n{len(violations)} file(s) exceed the {LIMIT}-line limit:", file=sys.stderr)
    for path, n in violations:
        print(f"  {path}: {n} lines", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
