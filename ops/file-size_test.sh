#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=ops/file-size.sh
source "$script_dir/file-size.sh"

fixture="$script_dir/run-v2-gate.sh"
expected="$(wc -c <"$fixture" | tr -d '[:space:]')"
actual="$(annolux_file_size_bytes "$fixture")"
[[ "$actual" =~ ^[0-9]+$ ]]
[[ "$actual" == "$expected" ]]

if annolux_file_size_bytes "$script_dir/not-a-real-file" >/dev/null 2>&1; then
  echo 'missing files must be rejected' >&2
  exit 1
fi
