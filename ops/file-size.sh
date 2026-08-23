#!/usr/bin/env bash

# annolux_file_size_bytes prints one base-10 byte count on GNU/Linux and BSD
# systems. Validate the output because GNU stat accepts `-f` with different
# semantics and can otherwise emit an entire filesystem report.
annolux_file_size_bytes() {
  local path="${1:-}"
  local size
  [[ -f "$path" ]] || return 1
  if size="$(stat -c %s -- "$path" 2>/dev/null)"; then
    :
  elif size="$(stat -f %z "$path" 2>/dev/null)"; then
    :
  else
    return 1
  fi
  case "$size" in
    ''|*[!0-9]*) return 1 ;;
  esac
  printf '%s\n' "$size"
}
