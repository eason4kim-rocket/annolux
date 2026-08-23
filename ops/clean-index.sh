#!/usr/bin/env bash
set -euo pipefail
umask 077

# clean-index.sh applies the crawler's current junk rule retroactively, and it
# does so on a copy: the served index is read once and never written, so a
# failed quality gate is answered by discarding the candidate rather than by
# restoring the thing customers are querying.
#
# It stops after producing a verified candidate. Loading that candidate on a
# spare port, re-running the held-out gate and only then switching are separate,
# deliberate steps.

if [[ $# -ne 2 ]]; then
  echo 'usage: clean-index.sh SERVED_INDEX CANDIDATE_INDEX' >&2
  exit 2
fi
served_index="$1"
candidate_index="$2"
container_uid="${ANNOLUX_CONTAINER_UID:-65532}"
container_gid="${ANNOLUX_CONTAINER_GID:-65532}"
[[ -f "$served_index" ]] || { echo 'served index is not a regular file' >&2; exit 1; }
[[ "$served_index" != "$candidate_index" ]] || { echo 'candidate must be a separate file' >&2; exit 1; }
[[ ! -e "$candidate_index" ]] || { echo 'candidate index already exists' >&2; exit 1; }
[[ ! -s "$served_index-wal" ]] || { echo 'served WAL is not empty; checkpoint before copying' >&2; exit 1; }
[[ "$container_uid" =~ ^[0-9]+$ && "$container_gid" =~ ^[0-9]+$ ]] || { echo 'container UID/GID must be numeric' >&2; exit 2; }

resolve_command() {
  local configured="$1"
  local fallback="$2"
  if [[ -n "$configured" ]]; then
    [[ "$configured" == /* && -x "$configured" ]] || { echo "$fallback binary must be an executable absolute path" >&2; exit 2; }
    printf '%s\n' "$configured"
    return
  fi
  command -v "$fallback" >/dev/null 2>&1 || { echo "$fallback is not installed and no binary path was provided" >&2; exit 2; }
  command -v "$fallback"
}

admin_bin="$(resolve_command "${ANNOLUX_ADMIN_BIN:-}" annolux-admin)"
clean_bin="$(resolve_command "${ANNOLUX_INDEXCLEAN_BIN:-}" annolux-indexclean)"
report_dir="${ANNOLUX_CLEANUP_REPORT_DIR:-$(dirname "$candidate_index")}"
[[ -d "$report_dir" ]] || { echo 'report directory does not exist' >&2; exit 2; }

served_sha="$(sha256sum "$served_index" | cut -d' ' -f1)"
cp --reflink=auto --preserve=mode,timestamps -- "$served_index" "$candidate_index"
candidate_sha="$(sha256sum "$candidate_index" | cut -d' ' -f1)"
[[ "$served_sha" == "$candidate_sha" ]] || { echo 'candidate copy does not match the served index' >&2; exit 1; }

# The dry run is kept even when the apply pass succeeds: it is the only record
# of what the index looked like before anything was deleted.
"$clean_bin" -db "$candidate_index" -top 25 -report "$report_dir/cleanup-dryrun.json" >/dev/null
"$clean_bin" -db "$candidate_index" -apply -top 25 -report "$report_dir/cleanup-apply.json" >/dev/null
"$admin_bin" verify-index "$candidate_index" >"$report_dir/cleanup-candidate-index.json"

chown "$container_uid:$container_gid" "$candidate_index"
chmod 600 "$candidate_index"
echo "candidate cleaned and verified: $candidate_index"
echo "served index untouched at $served_sha"
