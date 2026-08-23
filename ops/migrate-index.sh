#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $# -ne 2 ]]; then
  echo 'usage: migrate-index.sh SOURCE_INDEX /opt/annolux/data/index.db' >&2
  exit 2
fi
source_index="$1"
target_index="$2"
container_uid="${ANNOLUX_CONTAINER_UID:-65532}"
container_gid="${ANNOLUX_CONTAINER_GID:-65532}"
[[ -f "$source_index" ]] || { echo 'source index is not a regular file' >&2; exit 1; }
[[ "$target_index" == /opt/annolux/data/index.db ]] || { echo 'unexpected target index path' >&2; exit 1; }
[[ ! -e "$target_index" ]] || { echo 'target index already exists' >&2; exit 1; }
[[ ! -s "$source_index-wal" ]] || { echo 'source WAL is not empty; stop writers and checkpoint first' >&2; exit 1; }
[[ "$container_uid" =~ ^[0-9]+$ && "$container_gid" =~ ^[0-9]+$ ]] || { echo 'container UID/GID must be numeric' >&2; exit 2; }

source_report="$(mktemp)"
target_report="$(mktemp)"
cleanup() { rm -f -- "$source_report" "$target_report"; }
trap cleanup EXIT

annolux-admin verify-index "$source_index" >"$source_report"
install -d -o "$container_uid" -g "$container_gid" -m 700 "$(dirname "$target_index")"
cp --reflink=auto --preserve=mode,timestamps -- "$source_index" "$target_index"
annolux-admin verify-index "$target_index" >"$target_report"

jq -n -e --slurpfile before "$source_report" --slurpfile after "$target_report" \
  '($before[0] | del(.path)) == ($after[0] | del(.path))' >/dev/null
chown "$container_uid:$container_gid" "$target_index"
chmod 600 "$target_index"
echo 'index copy verified byte-for-byte and structurally'
