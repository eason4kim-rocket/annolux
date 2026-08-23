#!/usr/bin/env bash
set -euo pipefail
umask 077

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=ops/file-size.sh
source "$script_dir/file-size.sh"
# shellcheck source=ops/eval-identity.sh
source "$script_dir/eval-identity.sh"

: "${ANNOLUX_EVAL_API_URL:?set the loopback or HTTPS API base URL}"
: "${ANNOLUX_API_KEY:?set a dedicated evaluation API key}"
: "${ANNOLUX_EVAL_IMAGE_DIGEST:?set the deployed immutable image digest}"
: "${ANNOLUX_EVAL_OUTPUT_DIR:?set an existing private artifact directory}"
: "${ANNOLUX_INDEX_PATH:?set the copied Annolux index path}"
: "${ANNOLUX_EVAL_CODE_COMMIT:?set the deployed source commit}"

[[ -d "$ANNOLUX_EVAL_OUTPUT_DIR" ]] || { echo 'evaluation output directory does not exist' >&2; exit 2; }
[[ -f "$ANNOLUX_INDEX_PATH" ]] || { echo 'index path is not a regular file' >&2; exit 2; }
manifest="${ANNOLUX_EVAL_MANIFEST:-scripts/searcheval/corpus/v2/manifest.json}"
[[ -f "$manifest" ]] || { echo 'evaluation manifest does not exist' >&2; exit 2; }
gate_config="$(annolux_eval_companion_path "$manifest" release-gate.json "${ANNOLUX_EVAL_GATE:-}")"
ranking_lock="$(annolux_eval_companion_path "$manifest" ranking.lock.json "${ANNOLUX_EVAL_RANKING_LOCK:-}")"
[[ -f "$gate_config" ]] || { echo 'evaluation release gate does not exist' >&2; exit 2; }
[[ -f "$ranking_lock" ]] || { echo 'evaluation ranking lock does not exist' >&2; exit 2; }
case "$ANNOLUX_EVAL_API_URL" in
  https://*|http://127.0.0.1|http://127.0.0.1:*) ;;
  *) echo 'evaluation API must use HTTPS or loopback HTTP' >&2; exit 2 ;;
esac

code_commit="$ANNOLUX_EVAL_CODE_COMMIT"
evaluator_commit="${ANNOLUX_EVAL_EVALUATOR_COMMIT:-$code_commit}"
[[ "$code_commit" =~ ^([0-9a-f]{40}|[0-9a-f]{64})$ ]] || { echo 'evaluation code commit must be a full hexadecimal object ID' >&2; exit 2; }
[[ "$evaluator_commit" =~ ^([0-9a-f]{40}|[0-9a-f]{64})$ ]] || { echo 'evaluator commit must be a full hexadecimal object ID' >&2; exit 2; }
[[ "$ANNOLUX_EVAL_IMAGE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'evaluation image digest must be an immutable sha256 digest' >&2; exit 2; }

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
if [[ -n "${ANNOLUX_EVALUATOR_BIN:-}" ]]; then
  evaluator=("$(resolve_command "$ANNOLUX_EVALUATOR_BIN" searcheval)")
else
  go_bin="$(resolve_command "${ANNOLUX_GO_BIN:-}" go)"
  evaluator=("$go_bin" run ./scripts/searcheval)
fi
if [[ -n "${ANNOLUX_EVALGATE_BIN:-}" ]]; then
  evalgate=("$(resolve_command "$ANNOLUX_EVALGATE_BIN" evalgate)")
else
  go_bin="${go_bin:-$(resolve_command "${ANNOLUX_GO_BIN:-}" go)}"
  evalgate=("$go_bin" run ./tools/evalgate)
fi

run_id="$(annolux_eval_run_id "$code_commit")"
artifact="$ANNOLUX_EVAL_OUTPUT_DIR/$run_id.jsonl"
gate_result="$ANNOLUX_EVAL_OUTPUT_DIR/$run_id-gate.json"
audit="$ANNOLUX_EVAL_OUTPUT_DIR/$run_id-index.json"

"$admin_bin" verify-index "$ANNOLUX_INDEX_PATH" >"$audit"
chmod 600 "$audit"
pages="$(jq -er '.pages' "$audit")"
english="$(jq -er '.english_pages' "$audit")"
chinese="$(jq -er '.chinese_pages' "$audit")"
latest="$(jq -er '.latest_fetched_at' "$audit")"
database_bytes="$(jq -er '.size_bytes' "$audit")"
wal_bytes=0
if [[ -f "$ANNOLUX_INDEX_PATH-wal" ]]; then
  wal_bytes="$(annolux_file_size_bytes "$ANNOLUX_INDEX_PATH-wal")"
fi

"${evaluator[@]}" \
  -manifest "$manifest" \
  -out "$artifact" \
  -api-url "$ANNOLUX_EVAL_API_URL" \
  -run-id "$run_id" \
  -code-commit "$code_commit" \
  -evaluator-commit "$evaluator_commit" \
  -image-digest "$ANNOLUX_EVAL_IMAGE_DIGEST" \
  -split heldout \
  -timeout 5s \
  -index-pages "$pages" \
  -index-en "$english" \
  -index-zh "$chinese" \
  -index-latest-fetched-at "$latest" \
  -index-db-bytes "$database_bytes" \
  -index-wal-bytes "$wal_bytes"

"${evalgate[@]}" -gate "$gate_config" -ranking-lock "$ranking_lock" -artifact "$artifact" -out "$gate_result"
echo "v2 blind release gate passed: $gate_result"
