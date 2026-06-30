#!/usr/bin/env bash
# Docs-freshness gate — Constitution Principle X.
# Flags (warn-only): docs whose `Last reviewed` exceeds cadence, and code changes
# that touch no public doc (docs-with-code). Warn now; can be made blocking later.
set -euo pipefail

MAX_AGE_DAYS="${DOCS_MAX_AGE_DAYS:-90}"
today=$(date +%s)
warned=0

# Portable YYYY-MM-DD -> epoch (macOS BSD date, then GNU date fallback).
to_epoch() {
  date -j -f '%Y-%m-%d' "$1" +%s 2>/dev/null || date -d "$1" +%s 2>/dev/null || echo 0
}

# 1) Stale "Last reviewed:" headers.
while IFS= read -r f; do
  [ -z "$f" ] && continue
  d=$(grep -m1 -E 'Last reviewed:' "$f" | grep -oE '[0-9]{4}-[0-9]{2}-[0-9]{2}' || true)
  [ -z "$d" ] && continue
  ds=$(to_epoch "$d")
  [ "$ds" -eq 0 ] && continue
  age=$(( (today - ds) / 86400 ))
  if [ "$age" -gt "$MAX_AGE_DAYS" ]; then
    echo "⚠ docs-freshness: $f — reviewed $age days ago (>$MAX_AGE_DAYS). Update 'Last reviewed'."
    warned=1
  fi
done < <(grep -rlE 'Last reviewed:' --include='*.md' . 2>/dev/null || true)

# 2) docs-with-code: staged code changed, no public doc changed.
staged=$(git diff --cached --name-only 2>/dev/null || true)
if printf '%s\n' "$staged" | grep -qE '\.(go|ts|tsx|js|jsx|vue|svelte)$'; then
  if ! printf '%s\n' "$staged" | grep -qE '^(README|CHANGELOG)|^docs/'; then
    echo "⚠ docs-freshness: code changed but README/CHANGELOG/docs/ not updated (Principle X docs-with-code)."
    warned=1
  fi
fi

[ "$warned" -eq 0 ] && echo "✓ docs-freshness: ok"
exit 0
