#!/usr/bin/env bash
set -euo pipefail

api_container="${ANNOLUX_API_CONTAINER_NAME:-annolux-api}"
[[ "$api_container" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$ ]] || { echo 'invalid API container name' >&2; exit 2; }

docker exec "$api_container" annolux-index \
  -out /data/index.db \
  -refresh-existing \
  -refresh-older-than "${ANNOLUX_REFRESH_OLDER_THAN:-720h}" \
  -max-pages "${ANNOLUX_REFRESH_MAX_PAGES:-2000}" \
  -max-disk-used-percent 82 \
  -min-disk-available-gib 25
