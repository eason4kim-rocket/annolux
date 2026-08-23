#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=ops/eval-identity.sh
source "$script_dir/eval-identity.sh"

commit=3be720b5d9249f0db2a82afc5cfec4d43cb5fd06
expected=annolux-v2-heldout-20260815t235900z-3be720b5d924
[[ "$(annolux_eval_run_id "$commit" 20260815t235900z)" == "$expected" ]]
[[ "$(annolux_eval_run_id "$commit")" =~ ^annolux-v2-heldout-[0-9]{8}t[0-9]{6}z-3be720b5d924$ ]]

if annolux_eval_run_id "$commit" 20260815T235900Z >/dev/null 2>&1; then
  echo 'uppercase evaluation timestamps must be rejected' >&2
  exit 1
fi
if annolux_eval_run_id not-a-commit 20260815t235900z >/dev/null 2>&1; then
  echo 'invalid evaluation commits must be rejected' >&2
  exit 1
fi

manifest=/opt/annolux/eval/corpus/manifest.json
[[ "$(annolux_eval_companion_path "$manifest" release-gate.json)" == /opt/annolux/eval/corpus/release-gate.json ]]
[[ "$(annolux_eval_companion_path "$manifest" ranking.lock.json /private/ranking.lock.json)" == /private/ranking.lock.json ]]
if annolux_eval_companion_path "$manifest" ../escape >/dev/null 2>&1; then
  echo 'companion filenames must not contain path traversal' >&2
  exit 1
fi
