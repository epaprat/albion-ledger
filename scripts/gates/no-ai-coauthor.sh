#!/usr/bin/env bash
# No-AI-co-author gate — Constitution Principle IX.
# Sole human maintainer: commit messages MUST NOT carry AI co-author trailers.
# Usage: no-ai-coauthor.sh <commit-msg-file>
set -euo pipefail

msg_file="${1:?commit message file required}"

if grep -iqE 'co-authored-by:.*(claude|anthropic|\bai\b|copilot|gpt|chatgpt|gemini)' "$msg_file"; then
  echo "✗ commit-msg: AI co-author trailer not allowed (Principle IX). Sole maintainer only."
  grep -inE 'co-authored-by:.*' "$msg_file" | sed 's/^/    /'
  exit 1
fi

echo "✓ no-ai-coauthor: clean"
