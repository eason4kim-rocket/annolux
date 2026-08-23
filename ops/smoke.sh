#!/usr/bin/env bash
set -euo pipefail
umask 077

web_url="${ANNOLUX_SMOKE_WEB_URL:-http://127.0.0.1:8084}"
api_url="${ANNOLUX_SMOKE_API_URL:-http://127.0.0.1:8083}"

valid_base_url() {
  [[ "$1" == https://* || "$1" == http://127.0.0.1:* || "$1" == http://127.0.0.1 ]]
}
valid_base_url "$web_url" || { echo 'ANNOLUX_SMOKE_WEB_URL must use HTTPS or loopback HTTP' >&2; exit 2; }
valid_base_url "$api_url" || { echo 'ANNOLUX_SMOKE_API_URL must use HTTPS or loopback HTTP' >&2; exit 2; }
web_url="${web_url%/}"
api_url="${api_url%/}"

scratch_dir="$(mktemp -d)"
cleanup() {
  case "$scratch_dir" in
    /tmp/*|/private/tmp/*) rm -rf -- "$scratch_dir" ;;
    *) echo 'refusing unsafe smoke cleanup' >&2 ;;
  esac
}
trap cleanup EXIT

curl_args=(--silent --show-error --max-time 15 --retry 2 --retry-all-errors)
curl "${curl_args[@]}" --fail "$web_url/healthz" >"$scratch_dir/web-health"
curl "${curl_args[@]}" --fail "$api_url/api/v1/health" | jq -e '.status=="healthy"' >/dev/null
curl "${curl_args[@]}" --fail "$api_url/api/v1/ready" | jq -e '.status=="ready"' >/dev/null

for route_name in / /docs /pricing /benchmarks /blog /privacy /terms /refund /acceptable-use /zh/ /zh/docs /zh/privacy /openapi.json /robots.txt /sitemap.xml; do
  curl "${curl_args[@]}" --fail "$web_url$route_name" >/dev/null
done
not_found_status="$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$web_url/smoke-not-a-real-page")"
[[ "$not_found_status" == 404 ]] || { echo "expected a true 404, got $not_found_status" >&2; exit 1; }

if [[ "${ANNOLUX_SMOKE_CONFIRM_CREDIT:-}" == YES ]]; then
  api_key="${ANNOLUX_SMOKE_API_KEY:-}"
  [[ "$api_key" =~ ^ann_live_[A-Za-z0-9_-]+$ ]] || { echo 'ANNOLUX_SMOKE_API_KEY is missing or invalid' >&2; exit 2; }
  printf 'header = "Authorization: Bearer %s"\n' "$api_key" >"$scratch_dir/curl-auth.conf"
  chmod 600 "$scratch_dir/curl-auth.conf"
  # X-Annolux-Ops keeps this probe out of the query log so operator smoke
  # traffic never pollutes the real query distribution.
  status="$(curl "${curl_args[@]}" --config "$scratch_dir/curl-auth.conf" \
    -D "$scratch_dir/search-headers" -o "$scratch_dir/search.json" -w '%{http_code}' \
    -H 'Content-Type: application/json' -H 'X-Annolux-Ops: smoke' \
    --data-binary '{"query":"rust ownership","limit":3,"timeout":10}' \
    "$api_url/api/v1/search")"
  [[ "$status" == 200 ]] || { echo "Search smoke returned HTTP $status" >&2; exit 1; }
  jq -e '.success==true and (.results|type=="array") and all(.results[]; (.fetched_at|type)=="string")' "$scratch_dir/search.json" >/dev/null
  tr -d '\r' <"$scratch_dir/search-headers" | rg -qi '^X-Request-ID: req_[a-f0-9]{32}$'
  tr -d '\r' <"$scratch_dir/search-headers" | rg -qi '^X-Annolux-Credits-Used: 1$'
  tr -d '\r' <"$scratch_dir/search-headers" | rg -qi '^X-Annolux-Credits-Remaining: [0-9]+$'
  echo 'Annolux web, API readiness, routing, 404 and one-credit Search smoke: pass'
else
  echo 'Annolux web, API readiness, routing and 404 smoke: pass'
  echo 'Search was not called; set ANNOLUX_SMOKE_CONFIRM_CREDIT=YES with ANNOLUX_SMOKE_API_KEY to spend one credit.'
fi
