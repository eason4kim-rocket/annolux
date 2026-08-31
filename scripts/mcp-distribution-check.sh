#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

if rg -n -- '(go install .*annolux-mcp|Install the annolux-mcp binary|rerun go install|重跑 go install|["[:space:]]-key["[:space:]])' \
  README.md README_zh.md website/content.json packages/annolux-mcp/README.md smithery.yaml glama.json; then
  echo 'legacy or unsafe MCP installation instructions remain in a public channel' >&2
  exit 1
fi

for public_source in README.md README_zh.md website/content.json packages/annolux-mcp/README.md; do
  rg -q -- 'npx -y annolux-mcp' "$public_source" || {
    echo "$public_source does not contain the canonical npm installation command" >&2
    exit 1
  }
done

node scripts/mcp-package-smoke.mjs
node scripts/mcp-package-smoke.mjs --expect-upgrade

pack_dir="$(mktemp -d)"
trap 'rm -rf -- "$pack_dir"' EXIT
package_file="$(npm pack --silent --pack-destination "$pack_dir" ./packages/annolux-mcp)"
mkdir "$pack_dir/install"
npm install --prefix "$pack_dir/install" --ignore-scripts --no-audit --no-fund "$pack_dir/$package_file" >/dev/null
node scripts/mcp-package-smoke.mjs "$pack_dir/install/node_modules/.bin/annolux-mcp"
node scripts/mcp-package-smoke.mjs "$pack_dir/install/node_modules/.bin/annolux-mcp" --expect-upgrade

echo 'MCP distribution: pass'
