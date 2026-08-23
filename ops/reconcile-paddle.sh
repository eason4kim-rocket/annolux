#!/usr/bin/env bash
set -euo pipefail

api_container="${ANNOLUX_API_CONTAINER_NAME:-annolux-api}"
[[ "$api_container" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$ ]] || { echo 'invalid API container name' >&2; exit 2; }

docker exec "$api_container" annolux-admin reconcile-paddle "$(date -u -v-48H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '48 hours ago' +%Y-%m-%dT%H:%M:%SZ)"
