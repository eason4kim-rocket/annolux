#!/usr/bin/env bash

# annolux_eval_run_id produces the lowercase, bounded identifier required by
# the frozen evaluator. A timestamp may be injected by tests; production uses
# the current UTC time.
annolux_eval_run_id() {
  local commit="${1:-}"
  local stamp="${2:-$(date -u +%Y%m%dt%H%M%Sz)}"
  local run_id
  [[ "$commit" =~ ^([0-9a-f]{40}|[0-9a-f]{64})$ ]] || return 1
  [[ "$stamp" =~ ^[0-9]{8}t[0-9]{6}z$ ]] || return 1
  run_id="annolux-v2-heldout-$stamp-${commit:0:12}"
  [[ "$run_id" =~ ^[a-z0-9][a-z0-9-]{2,79}$ ]] || return 1
  printf '%s\n' "$run_id"
}

# annolux_eval_companion_path keeps a deployed corpus self-contained while
# allowing an explicit absolute override when operations layout requires it.
annolux_eval_companion_path() {
  local manifest="${1:-}"
  local filename="${2:-}"
  local override="${3:-}"
  [[ -n "$manifest" && "$filename" =~ ^[a-z0-9][a-z0-9.-]*$ ]] || return 1
  if [[ -n "$override" ]]; then
    printf '%s\n' "$override"
    return
  fi
  printf '%s/%s\n' "$(dirname -- "$manifest")" "$filename"
}
