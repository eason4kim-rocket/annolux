#!/usr/bin/env bash
set -euo pipefail
umask 077

deploy_root="${ANNOLUX_DEPLOY_ROOT:-/opt/annolux}"
data_root="${ANNOLUX_DATA_ROOT:-$deploy_root/data}"
staging="$data_root/backup-staging"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
snapshot="$staging/accounts-$timestamp.db"
container_uid="${ANNOLUX_CONTAINER_UID:-65532}"
container_gid="${ANNOLUX_CONTAINER_GID:-65532}"
api_container="${ANNOLUX_API_CONTAINER_NAME:-annolux-api}"

[[ "$deploy_root" == /opt/annolux ]] || { echo 'unexpected deploy root' >&2; exit 1; }
: "${ANNOLUX_RESTIC_REPOSITORY:?set ANNOLUX_RESTIC_REPOSITORY}"
: "${ANNOLUX_RESTIC_PASSWORD:?set ANNOLUX_RESTIC_PASSWORD}"
: "${ANNOLUX_R2_ACCESS_KEY_ID:?set ANNOLUX_R2_ACCESS_KEY_ID}"
: "${ANNOLUX_R2_SECRET_ACCESS_KEY:?set ANNOLUX_R2_SECRET_ACCESS_KEY}"
export AWS_ACCESS_KEY_ID="$ANNOLUX_R2_ACCESS_KEY_ID"
export AWS_SECRET_ACCESS_KEY="$ANNOLUX_R2_SECRET_ACCESS_KEY"
[[ "$container_uid" =~ ^[0-9]+$ && "$container_gid" =~ ^[0-9]+$ ]] || { echo 'container UID/GID must be numeric' >&2; exit 2; }
[[ "$api_container" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$ ]] || { echo 'invalid API container name' >&2; exit 2; }
install -d -o "$container_uid" -g "$container_gid" -m 700 "$staging"

cleanup() { rm -f -- "$snapshot"; }
trap cleanup EXIT

docker exec "$api_container" annolux-admin snapshot "/data/backup-staging/$(basename "$snapshot")"
docker exec "$api_container" annolux-admin -db "/data/backup-staging/$(basename "$snapshot")" integrity
[[ -f "$snapshot" ]] || { echo 'container did not create the accounts snapshot' >&2; exit 1; }
restic backup --tag annolux-accounts -- "$snapshot"
restic forget --tag annolux-accounts --keep-daily 14 --keep-weekly 8 --keep-monthly 12 --prune
echo "accounts backup completed: $timestamp"
