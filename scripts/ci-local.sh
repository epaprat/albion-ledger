#!/usr/bin/env bash
# Local CI — runs ALL constitution gates on your machine (replaces GitHub Actions).
# Invoked by the pre-push hook; also runnable by hand: scripts/ci-local.sh
set -uo pipefail
root="$(git rev-parse --show-toplevel)"; cd "$root"
fail=0

echo "▶ repo-hygiene (--all)        [Principle IX]"
bash scripts/gates/repo-hygiene.sh --all || fail=1

echo "▶ no-ai-coauthor (unpushed)   [Principle IX]"
git log --pretty=%B '@{u}..HEAD' 2>/dev/null > /tmp/_albion_msgs.txt \
  || git log --pretty=%B -n 50 2>/dev/null > /tmp/_albion_msgs.txt \
  || : > /tmp/_albion_msgs.txt
bash scripts/gates/no-ai-coauthor.sh /tmp/_albion_msgs.txt || fail=1

echo "▶ docs-freshness (warn)       [Principle X]"
bash scripts/gates/docs-freshness.sh || true

# --- Code gates: only when the Go app exists ---
if [ -f go.mod ]; then
  echo "▶ go vet                      [Principle III]"
  go vet ./... || fail=1
  echo "▶ go test                     [Principle III]"
  go test ./... || fail=1
  if command -v golangci-lint >/dev/null 2>&1; then
    echo "▶ golangci-lint"
    golangci-lint run || fail=1
  else
    echo "  (golangci-lint not installed — skipped)"
  fi
  # TODO(M0+): fuzz (IV), soak/leak (XI), ux-a11y (XII) gates here.
fi

if [ "$fail" -eq 0 ]; then
  echo "✓ local CI passed"
else
  echo "✗ local CI failed — push aborted"
  exit 1
fi
