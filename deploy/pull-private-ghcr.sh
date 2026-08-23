#!/usr/bin/env bash
set -euo pipefail

registry="ghcr.io"
repository="ghcr.io/eason4kim-rocket/annolux"
username="${ANNOLUX_GHCR_USERNAME:-}"

if [[ -z "$username" ]]; then
	printf 'ANNOLUX_GHCR_USERNAME is required\n' >&2
	exit 2
fi
if [[ "$#" -eq 0 ]]; then
	printf 'usage: ANNOLUX_GHCR_USERNAME=OWNER %s IMAGE@sha256:DIGEST [...] < TOKEN\n' "$0" >&2
	exit 2
fi
if [[ -t 0 ]]; then
	printf 'refusing to read a GHCR token from an interactive terminal; provide it on standard input\n' >&2
	exit 2
fi

for image in "$@"; do
	digest="${image#"$repository"@sha256:}"
	if [[ "$image" != "$repository@sha256:$digest" || ! "$digest" =~ ^[0-9a-f]{64}$ ]]; then
		printf 'refusing mutable or foreign image reference: %s\n' "$image" >&2
		exit 2
	fi
done

umask 077
annolux_docker_config="$(mktemp -d "${TMPDIR:-/tmp}/annolux-docker-config.XXXXXX")"
cleanup() {
	rm -rf -- "$annolux_docker_config"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
chmod 0700 "$annolux_docker_config"

# Docker receives the token directly from stdin. Authentication is isolated in
# an ephemeral config so the read-only credential is not persisted on the host.
if ! DOCKER_CONFIG="$annolux_docker_config" docker login "$registry" \
	--username "$username" --password-stdin >/dev/null 2>&1; then
	printf 'private GHCR login failed\n' >&2
	exit 1
fi

for image in "$@"; do
	DOCKER_CONFIG="$annolux_docker_config" docker pull --quiet "$image" >/dev/null
	repo_digests="$(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$image")"
	if ! grep -Fqx -- "$image" <<<"$repo_digests"; then
		printf 'pulled image did not retain the requested digest: %s\n' "$image" >&2
		exit 1
	fi
done

printf 'private GHCR digest pull: pass (%d image(s))\n' "$#"
