#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
sitemap="$root/website/dist/sitemap.xml"
[[ -f "$sitemap" ]] || { echo 'generate the site before submitting IndexNow' >&2; exit 1; }
proof_file="$(python3 -c 'import pathlib,re,sys; files=[p for p in pathlib.Path(sys.argv[1]).glob("*.txt") if re.fullmatch(r"[0-9a-f]{64}\.txt",p.name)]; assert len(files)==1; print(files[0])' "$root/website/dist")"
key="$(tr -d '\r\n' < "$proof_file")"
[[ "$key" = "$(basename "$proof_file" .txt)" && "$key" =~ ^[0-9a-f]{64}$ ]] || {
  echo 'IndexNow proof filename and content do not agree' >&2
  exit 1
}

urls="$(sed -n 's|.*<loc>\([^<]*\)</loc>.*|\1|p' "$sitemap" | jq -R . | jq -s .)"
payload="$(jq -n --arg host annolux.com --arg key "$key" --arg keyLocation "https://annolux.com/$key.txt" --argjson urlList "$urls" '{host:$host,key:$key,keyLocation:$keyLocation,urlList:$urlList}')"
curl --fail --silent --show-error --max-time 20 --retry 2 --retry-all-errors \
  -H 'Content-Type: application/json; charset=utf-8' \
  --data-binary "$payload" \
  https://api.indexnow.org/indexnow >/dev/null
echo 'IndexNow sitemap URLs submitted'
