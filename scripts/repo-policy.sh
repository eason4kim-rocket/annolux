#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

legacy_brand="$(printf '\160\165\162\151\146\171')"
legacy_account="$(printf '\105\141\163\157\156\154\151\165\154\151\141\156\147')"
legacy_domain="$(printf '\166\145\162\151\146\154\171\056\160\162\157')"

scan() {
  local pattern="$1"
	if command -v rg >/dev/null 2>&1; then
		rg -n -i --hidden --glob '!.git/**' --glob '!node_modules/**' --glob '!OPS.local.md' --glob '!scripts/repo-policy.sh' -- "$pattern" . || true
	else
		grep -RInEi --exclude-dir=.git --exclude-dir=node_modules --exclude=OPS.local.md --exclude=repo-policy.sh -- "$pattern" . || true
	fi
}

for pattern in "$legacy_brand" "$legacy_account" "$legacy_domain"; do
  matches="$(scan "$pattern")"
  if [[ -n "$matches" ]]; then
    printf 'forbidden legacy reference:\n%s\n' "$matches" >&2
    exit 1
  fi
done

# These customer-facing DTO names belong to product surfaces that are outside
# Annolux. Internal indexing and invitation-only Labs use neutral types instead.
legacy_contracts='\b(ScrapeRequest|ScrapeResponse|BatchRequest|BatchResponse|CrawlRequest|CrawlResponse|MapRequest|MapResponse|ExtractRequest|ExtractResponse|AnswerRequest|AnswerResponse|WatchRequest|WatchResponse)\b'
matches="$(scan "$legacy_contracts")"
if [[ -n "$matches" ]]; then
  printf 'forbidden commercial interface type:\n%s\n' "$matches" >&2
  exit 1
fi

go run ./tools/repopolicy

media="$(find . -type f -not -path '*/node_modules/*' \( -iname '*.mp4' -o -iname '*.mov' -o -iname '*.wav' -o -iname '*.aiff' \) -print)"
if [[ -n "$media" ]]; then
  printf 'forbidden media files:\n%s\n' "$media" >&2
  exit 1
fi

large="$(find . -type f -size +5M -not -path './.git/*' -not -path '*/node_modules/*' -print)"
if [[ -n "$large" ]]; then
  printf 'unapproved files larger than 5 MiB:\n%s\n' "$large" >&2
  exit 1
fi

production_data="$(find . -type f \( -name '.env' -o -name '*.db' -o -name '*.sqlite' -o -name '*-wal' -o -name '*-shm' \) -not -path './.git/*' -not -path '*/node_modules/*' -print)"
if [[ -n "$production_data" ]]; then
  printf 'production data or host environment file found:\n%s\n' "$production_data" >&2
  exit 1
fi

credential_matches="$(scan '(sshpass|password-file|BEGIN (RSA|OPENSSH|EC) PRIVATE KEY)')"
if [[ -n "$credential_matches" ]]; then
	printf '%s\n' "$credential_matches" >&2
  echo 'credential material or credential-path pattern found' >&2
  exit 1
fi

public_ghcr_guidance="$(scan '(ghcr.{0,80}anonymous|anonymous.{0,80}ghcr|package visibility.{0,40}public|public.{0,40}ghcr)')"
if [[ -n "$public_ghcr_guidance" ]]; then
	printf 'GHCR must remain private; forbidden publication guidance:\n%s\n' "$public_ghcr_guidance" >&2
	exit 1
fi

if [[ -e OPS.local.md ]]; then
  git check-ignore -q OPS.local.md || { echo 'OPS.local.md must remain ignored' >&2; exit 1; }
  [[ -z "$(git ls-files -- OPS.local.md)" ]] || { echo 'OPS.local.md must never be tracked' >&2; exit 1; }
fi

if command -v rg >/dev/null 2>&1; then
  unpinned_actions="$(rg -n --glob '.github/workflows/*.yml' --glob '.github/workflows/*.yaml' \
    'uses:[[:space:]]+[^[:space:]#]+@(v[0-9]+|main|master|latest)([[:space:]#]|$)' . || true)"
else
  unpinned_actions="$(grep -RInE --include='*.yml' --include='*.yaml' \
    'uses:[[:space:]]+[^[:space:]#]+@(v[0-9]+|main|master|latest)([[:space:]#]|$)' .github/workflows || true)"
fi
if [[ -n "$unpinned_actions" ]]; then
  printf 'GitHub Actions must use immutable commit SHAs:\n%s\n' "$unpinned_actions" >&2
  exit 1
fi

unpinned_bases="$(awk '/^FROM / && $2 != "scratch" && $2 !~ /@sha256:[0-9a-f]{64}$/ {print FNR ":" $0}' Dockerfile)"
if [[ -n "$unpinned_bases" ]]; then
  printf 'Docker base images must use immutable multi-platform digests:\n%s\n' "$unpinned_bases" >&2
  exit 1
fi

echo 'repository policy: pass'
