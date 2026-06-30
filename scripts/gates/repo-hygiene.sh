#!/usr/bin/env bash
# Repo hygiene gate — Constitution Principle IX.
# Public repo: AI tooling, planning docs, and vendored clients must NEVER be tracked/committed.
# Usage:
#   repo-hygiene.sh           # check STAGED files (pre-commit)
#   repo-hygiene.sh --all     # check ALL TRACKED files (CI / local CI)
set -euo pipefail

# Private path classes that must stay out of the public repo.
PATTERNS='^\.claude/|^\.specify/|^CLAUDE\.md$|^GEMINI\.md$|^AGENTS\.md$|^ROADMAP\.md$|^SPEC\.md$|^PLAN.*\.md$|.*\.plan\.md$|^planning/|^docs-internal/|^\.internal/|^clients/'

if [ "${1:-}" = "--all" ]; then
  files=$(git ls-files)
  scope="tracked"
else
  files=$(git diff --cached --name-only)
  scope="staged"
fi

violations=$(printf '%s\n' "$files" | grep -E "$PATTERNS" || true)

if [ -n "$violations" ]; then
  echo "✗ repo-hygiene: these $scope paths must not enter the public repo (Principle IX):"
  printf '    %s\n' $violations
  echo "  Fix: git rm --cached <file>  (do not force-add; already in .gitignore)"
  exit 1
fi

echo "✓ repo-hygiene: $scope clean"
