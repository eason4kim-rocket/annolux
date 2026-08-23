#!/usr/bin/env bash
set -euo pipefail

name="${1:?heartbeat name is required}"
url_variable="ANNOLUX_HEARTBEAT_${name^^}_URL"
url="${!url_variable:-}"
[[ "$url" == https://* ]] || { echo "missing HTTPS heartbeat URL in $url_variable" >&2; exit 1; }
curl --fail --silent --show-error --max-time 10 --retry 2 --retry-all-errors "$url" >/dev/null
