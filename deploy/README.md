# Deployment

The production root is `/opt/annolux`. Never synchronize it with a deletion
flag and never point these files at another product directory or volume.

1. Generate the release site with verified legal environment values:

   ```sh
   make site-release
   bash scripts/release-check.sh
   ```

2. Build and publish the private `api` and `web` Docker targets. Record their
   immutable `sha256` digests in the host `.env`. Create a dedicated classic
   PAT with only `read:packages`; do not grant repository, write, or delete
   scopes. Feed it on standard input to the pull helper (the image references
   are examples of the required shape):

   ```sh
   read -r -s annolux_ghcr_token
   printf '%s\n' "$annolux_ghcr_token" | \
     ANNOLUX_GHCR_USERNAME=DEPLOY_READER \
       ./deploy/pull-private-ghcr.sh \
         ghcr.io/eason4kim-rocket/annolux@sha256:API_DIGEST \
         ghcr.io/eason4kim-rocket/annolux@sha256:WEB_DIGEST
   unset annolux_ghcr_token
   ```

   Supply standard input from the approved secret source; never paste the
   token into the command, `.env`, Compose, logs, or Git. The helper authenticates
   with an ephemeral Docker config, pulls exact digests, verifies them, and
   removes the config. Start the already-pulled images with `--pull never`.
3. Copy only reviewed deployment files to a new `/opt/annolux` directory.
4. Copy the search database into the new Annolux data directory; never move it.
   Run `annolux-admin verify-index` before and after the copy. Bind-mount this
   file read-only at `/data/index.db`; mount the separate accounts directory
   read/write at `/data` so only `accounts.db` can change.
5. Start on the spare loopback ports from `.env` with OAuth and billing values
   empty for infrastructure-only staging:

   ```sh
   docker compose up -d --pull never annolux-api annolux-web
   ```

   Run local health and Search smoke checks. The tunnel is behind the explicit
   `tunnel` profile; add it only after both services are healthy and its token
   exists:

   ```sh
   docker compose --profile tunnel pull annolux-tunnel
   docker compose --profile tunnel up -d --pull never annolux-tunnel
   ```
6. Exercise both OAuth providers, the 1,000-credit signup grant, API-key creation,
   one Search charge, key revocation and logout.
7. Change the tunnel routes only after the public temporary-route checks pass.

Use [cutover-runbook.md](cutover-runbook.md) for the evidence, acceptance and
rollback sequence. The API image runs as the explicit UID/GID in `.env`;
`/opt/annolux/accounts` is private and writable by that identity, while the
checkpointed `/opt/annolux/data/index.db` is private and read-only in the API
container.

Keep the previous containers and routes intact until the rollback drill passes.
The host-specific commands, addresses, secrets and recovery contacts belong in
ignored `OPS.local.md`, never in this repository.

Third-party account values and exact callbacks are enumerated in
`external-gates.md`. They remain frozen until the user completes the relevant
GitHub, OAuth, mailbox, Paddle, webmaster, R2 or monitoring action.
