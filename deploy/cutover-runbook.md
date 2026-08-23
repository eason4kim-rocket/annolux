# Free-product cutover and rollback

This runbook stages Annolux alongside every existing service. It never reuses
another deployment directory, mutates a source index, or changes public
traffic before a rollback path has been proved. Host addresses, credentials,
container digests and recovery contacts belong only in ignored
`OPS.local.md` and `.env`.

## 1. Immutable release inputs

Record the following in a private evidence directory before touching the host:

- source commit and clean-tree status;
- passing CI run and repository-policy output;
- private API/web and cloudflared image digests plus evidence that the
  read-only GHCR deploy identity pulled the two private images;
- release-site generation result with verified legal fields;
- SHA-256 of the frozen ranking lock and v2 manifest.

Only deploy `ghcr.io/eason4kim-rocket/annolux@sha256:…`. A tag is useful for
discovery, but it is never a deployment identity. Keep the package private;
use a dedicated expiring classic PAT with only `read:packages` through
`deploy/pull-private-ghcr.sh`. The token must enter on standard input and must
not be persisted in `.env`, Compose, the default Docker config or evidence.

## 2. Prepare the isolated host directory

1. Create `/opt/annolux`, `/opt/annolux/data`, `/opt/annolux/accounts`, and a
   private evidence folder. The copied index lives at
   `/opt/annolux/data/index.db` and is bind-mounted read-only. Set only the
   accounts directory owner to the explicit container UID/GID from `.env`
   (the image default is 65532:65532); do not make either directory
   world-readable.
2. Copy reviewed compose, nginx and operations files without a deletion flag.
3. Create a mode-0600 `.env`; generate a new account-ledger HMAC secret.
4. Keep Paddle disabled. OAuth may remain disabled for an infrastructure-only
   stage, but the free public route cannot launch until both providers work.
5. Bind API and web to unused loopback ports. Confirm they do not overlap any
   current listener before starting containers.
6. If a protected container already owns a final Annolux container name, set
   the three `ANNOLUX_*_CONTAINER_NAME` values to unique staging names. Never
   stop or rename the protected container merely to make staging start.

Stop if `/opt/annolux` resolves through a symlink, a requested port is in use,
the index has a non-empty WAL, or any volume target is outside
`/opt/annolux/data` and `/opt/annolux/accounts`.

## 3. Copy and prove the index

1. Stop writers to the source index and checkpoint its WAL. The API refuses to
   start when the copied index has a non-empty WAL.
2. Run `annolux-admin verify-index` against the source and retain the JSON.
3. Run `ops/migrate-index.sh SOURCE /opt/annolux/data/index.db`.
4. Retain the destination audit. Size, SHA-256, integrity result, page/FTS
   counts, language counts, latest fetch time and frontier counts must match.
5. Do not migrate queued frontier work into a scheduled expansion job. The
   copied rows are inert unless an operator explicitly runs the indexer.

The source database remains unchanged and available for rollback.

## 4. Start on spare loopback ports

1. Pull API and web by digest with the private-GHCR helper, then start them with
   `docker compose up -d --pull never annolux-api annolux-web`. OAuth and
   billing may remain entirely unset for this infrastructure-only stage. Start
   the tunnel profile only after both are healthy; first pull its public,
   digest-pinned cloudflared image with `docker compose --profile tunnel pull
   annolux-tunnel`, then start it with `--pull never`. Do not edit the active
   public ingress yet.
2. Require Docker health to pass for API, web and tunnel.
3. Run `ops/smoke.sh` without a key first.
4. Confirm `/api/v1/ready` fails when the index is hidden and recovers when it
   is restored. This proves readiness is not merely process liveness.
5. Run the v2 held-out gate with `ops/run-v2-gate.sh`; retain the JSONL, index
   audit and gate result. A failure freezes release and must not trigger tuning
   against the exposed held-out cases.
6. Run `tools/loadtest` with dedicated keys and explicit credit-spend
   confirmation. Require p95 at most one second, fewer than 1% 5xx, and the
   configured success gate.

## 5. Temporary public-route acceptance

Use a private temporary hostname or narrowly scoped tunnel route that points
only to the spare ports. Verify:

- web health, API health and API readiness;
- TLS, security headers, 404 behavior and every redirect matrix case;
- English and Chinese static pages with JavaScript disabled;
- OAuth new-account login for Google and GitHub;
- no automatic merge when both providers expose the same email;
- authenticated second-identity linking;
- exactly one 1,000-credit signup grant;
- API-key create/show-once/use/revoke and three-key limit;
- one successful Search commits one credit and all failure classes commit zero;
- Playground, logout and account closure preconditions;
- external monitor and status-page checks.

Do not expose Paddle Live during this acceptance. Sandbox checkout is a
separate paid gate.

## 6. Switch traffic

1. Export or screenshot the active tunnel ingress configuration privately.
2. Change apex and API routes to Annolux origins in one controlled change.
3. Re-run the public smoke suite, OAuth flows and one real Search.
4. Watch readiness, 5xx, p95, credit-reservation backlog and container restarts
   continuously during the observation window.
5. Stop traffic to superseded containers only after the public checks pass;
   retain their images, data and previous ingress configuration.
6. Only after the observation gate passes, retire any protected name holder
   and move the reviewed deployment to the canonical container names. Keep the
   previous loopback origin available for route-first rollback.

## 7. Rollback triggers and action

Rollback immediately for any of these conditions:

- API readiness fails or the copied index cannot be queried;
- signup grants duplicate, successful calls do not charge exactly once, or an
  error path charges a credit;
- OAuth state, nonce, PKCE, Session, Origin or CSRF checks fail;
- 5xx reaches 1%, p95 exceeds one second persistently, or containers restart;
- canonical/redirect behavior changes in a way that can split indexing;
- tunnel health is lost after the route switch.

Rollback is routing-first: restore the saved tunnel ingress, verify the former
public route, then stop the new containers. Never delete `/opt/annolux/data` or
`/opt/annolux/accounts`, and never overwrite the source deployment during
rollback. Preserve logs and private artifacts for diagnosis. A later retry
uses a new image digest and new evidence directory.

## 8. Paid gate remains independent

Free launch does not unlock billing. Paddle Live remains frozen until legal
identity is confirmed, Sandbox products and the full event matrix pass,
encrypted off-site restore succeeds, and Paddle accepts the buyer-support
arrangement or a business phone is supplied.
