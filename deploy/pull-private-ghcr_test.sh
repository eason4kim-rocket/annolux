#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
fixture="$(mktemp -d)"
cleanup() {
	rm -rf -- "$fixture"
}
trap cleanup EXIT

mkdir -p "$fixture/bin"
docker_log="$fixture/docker.log"
docker_config_log="$fixture/docker-config.log"
token="annolux-test-token-not-a-secret"
image="ghcr.io/eason4kim-rocket/annolux@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

cat >"$fixture/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$ANNOLUX_TEST_DOCKER_LOG"
case "${1:-}:${2:-}" in
	login:ghcr.io)
		IFS= read -r supplied || true
		[[ "$supplied" == "$ANNOLUX_TEST_EXPECTED_TOKEN" ]]
		[[ -n "${DOCKER_CONFIG:-}" && "$DOCKER_CONFIG" != "${ANNOLUX_TEST_DEFAULT_DOCKER_CONFIG:-}" ]]
		if ! docker_config_mode="$(stat -c '%a' "$DOCKER_CONFIG" 2>/dev/null)"; then
			docker_config_mode="$(stat -f '%Lp' "$DOCKER_CONFIG")"
		fi
		[[ "$docker_config_mode" == "700" ]]
		printf '%s\n' "$DOCKER_CONFIG" >"$ANNOLUX_TEST_DOCKER_CONFIG_LOG"
		;;
	pull:--quiet)
		[[ -d "${DOCKER_CONFIG:-}" ]]
		;;
	image:inspect)
		printf '%s\n' "${@: -1}"
		;;
	*)
		exit 91
		;;
esac
DOCKER
chmod 0755 "$fixture/bin/docker"

output="$({
	printf '%s\n' "$token" | env \
		PATH="$fixture/bin:$PATH" \
		ANNOLUX_GHCR_USERNAME=deploy-reader \
		ANNOLUX_TEST_DOCKER_LOG="$docker_log" \
		ANNOLUX_TEST_DOCKER_CONFIG_LOG="$docker_config_log" \
		ANNOLUX_TEST_EXPECTED_TOKEN="$token" \
		ANNOLUX_TEST_DEFAULT_DOCKER_CONFIG="${DOCKER_CONFIG:-}" \
		"$root/deploy/pull-private-ghcr.sh" "$image"
} 2>&1)"

grep -Fq 'private GHCR digest pull: pass (1 image(s))' <<<"$output"
if grep -Fq -- "$token" <<<"$output" || grep -Fq -- "$token" "$docker_log"; then
	printf 'token leaked into pull output or Docker arguments\n' >&2
	exit 1
fi
grep -Fq -- 'login ghcr.io --username deploy-reader --password-stdin' "$docker_log"
grep -Fq -- "pull --quiet $image" "$docker_log"
ephemeral_config="$(<"$docker_config_log")"
[[ ! -e "$ephemeral_config" ]]

if printf '%s\n' "$token" | env PATH="$fixture/bin:$PATH" ANNOLUX_GHCR_USERNAME=deploy-reader \
	"$root/deploy/pull-private-ghcr.sh" ghcr.io/eason4kim-rocket/annolux:latest >/dev/null 2>&1; then
	printf 'mutable tag was accepted\n' >&2
	exit 1
fi
if printf '%s\n' "$token" | env PATH="$fixture/bin:$PATH" ANNOLUX_GHCR_USERNAME=deploy-reader \
	"$root/deploy/pull-private-ghcr.sh" "ghcrXio/eason4kim-rocket/annolux@sha256:${image##*:}" >/dev/null 2>&1; then
	printf 'foreign registry was accepted\n' >&2
	exit 1
fi

printf 'private GHCR pull helper: pass\n'
