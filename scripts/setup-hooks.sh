#!/usr/bin/env bash
# Activate the constitution git gates locally. Run once after clone.
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
cd "$root"
git config core.hooksPath .githooks
chmod +x .githooks/* scripts/gates/*.sh scripts/*.sh 2>/dev/null || true
echo "✓ git gates active (core.hooksPath=.githooks)"
echo "  - pre-commit : repo-hygiene (block) + docs-freshness (warn)"
echo "  - commit-msg : no-ai-coauthor (block)"
echo "  - pre-push   : local CI = scripts/ci-local.sh (all gates — replaces GitHub Actions)"
