#!/usr/bin/env bash
set -euo pipefail
umask 077

deploy_root="${ANNOLUX_DEPLOY_ROOT:-/opt/annolux}"
data_root="${ANNOLUX_DATA_ROOT:-$deploy_root/data}"
drill="$data_root/restore-drill-$(date -u +%Y%m%dT%H%M%SZ)"
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
install -d -m 700 "$drill"
cleanup() {
  case "$drill" in
    "$data_root"/restore-drill-*) rm -rf -- "$drill" ;;
    *) echo 'refusing unsafe restore cleanup' >&2 ;;
  esac
}
trap cleanup EXIT

snapshot_id="$(restic snapshots --tag annolux-accounts --latest 1 --json | jq -er '.[0].short_id')"
restic restore "$snapshot_id" --target "$drill"
restored="$(find "$drill" -type f -name 'accounts-*.db' -print -quit)"
[[ -n "$restored" ]] || { echo 'restored accounts snapshot not found' >&2; exit 1; }
[[ ! -e "$data_root/restore-drill.db" ]] || { echo 'stale restore-drill.db exists' >&2; exit 1; }
cp -- "$restored" "$data_root/restore-drill.db"
chown "$container_uid:$container_gid" "$data_root/restore-drill.db"
chmod 600 "$data_root/restore-drill.db"
trap 'rm -f -- "$data_root/restore-drill.db"; cleanup' EXIT
docker exec "$api_container" annolux-admin -db /data/restore-drill.db integrity
echo "empty-directory restore drill passed: $snapshot_id"
