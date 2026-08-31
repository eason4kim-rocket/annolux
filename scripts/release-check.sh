#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
bash scripts/repo-policy.sh
bash scripts/mcp-distribution-check.sh
bash scripts/site-check.sh
go run ./tools/sitegen --check --release
if rg -n 'WAITING_USER|REPLACE_ME|sha256:REPLACE' website/dist; then
  echo 'release site still contains a frozen placeholder' >&2
  exit 1
fi
echo 'release artifacts: pass'
