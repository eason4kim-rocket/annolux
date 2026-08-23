# External account checklist

Everything in this file changes third-party account state and remains
`WAITING_USER` until the named owner action succeeds. Secrets and IDs go only
in GitHub Secrets or the ignored host `.env`; never paste them into issues,
logs, commits, or screenshots.

## GitHub and GHCR

- The public repository `eason4kim-rocket/annolux` now contains the clean
  independent history. Do not import or merge a historical repository.
- Keep Actions and package publishing enabled. Protect `main` after the final
  launch-status commit: require Verify and CodeQL checks, enforce the rule for
  administrators, and block force pushes and deletion. Do not require an
  unavailable second reviewer on this single-owner launch repository.
- Keep the `annolux` container package private. Create a dedicated classic PAT
  with only `read:packages`, no repository/write/delete scopes, and an expiry;
  the credential owner must retain read access to the package. Prove an
  authenticated immutable-digest pull with `deploy/pull-private-ghcr.sh`.
- Feed the token to the helper on standard input. Never put it in `.env`, a
  command argument, shell history, Compose, logs, screenshots, or Git. The
  helper uses a mode-0700 ephemeral Docker config and removes it after pulling.
- Release images from Actions only. Deploy immutable
  `ghcr.io/eason4kim-rocket/annolux@sha256:...` digests, never mutable tags.
- Web publication stays skipped until the three verified legal identity secrets
  exist and the repository variable `ANNOLUX_LEGAL_READY=true` is set. Set that
  variable only after the user confirms the release substitutions; a mistaken
  variable still fails closed in `sitegen --release`.
- MCP Registry publication stays frozen while the matching OCI image is
  private; do not run the manual metadata workflow for an image registry users
  cannot pull.

## OAuth

Google OAuth web application:

- Homepage/origin: `https://annolux.com`
- Authorized redirect URI:
  `https://api.annolux.com/api/v1/auth/google/callback`
- Scopes: `openid email profile`

GitHub OAuth application:

- Homepage: `https://annolux.com`
- Callback URL:
  `https://api.annolux.com/api/v1/auth/github/callback`
- Runtime scopes: `read:user user:email`

Do not enable email/password, magic-link, or additional OAuth scopes. Test both
new-account and in-account identity-link flows. A matching email must remain
two accounts unless the authenticated user actively links the second identity.

## Cloudflare Email Routing

Create four exact routes, no catch-all:

- `hello@annolux.com`
- `support@annolux.com`
- `privacy@annolux.com`
- `security@annolux.com`

All route to the designated Gmail inbox. Gmail destination confirmation is
`WAITING_USER`. Verify a real inbound message to each alias; forwarding does
not provide outbound sending and is not an authentication-email system.

## Paddle Sandbox and Live freeze

Create two monthly Sandbox prices:

| Product | Price | Entitlement recorded by Annolux |
|---|---:|---:|
| Annolux Search Pro | USD 29/month | 20,000 credits through `transaction.completed` |
| Annolux Search Scale | USD 99/month | 100,000 credits through `transaction.completed` |

Create a client-side token, API key, and webhook secret. Subscribe the webhook
to transaction completion, subscription lifecycle, and adjustment lifecycle
events at `https://api.annolux.com/api/v1/webhooks/paddle`. Put the two Price
IDs in the ignored environment. Checkout must remain Sandbox until duplicate,
out-of-order, refund, cancellation, renewal, chargeback, and reconciliation
tests pass.

Configure the customer portal for cancellation at period end only. Disable
plan switching, quantity changes, and prorated upgrades/downgrades; the server
also rejects a plan change on an existing subscription.

Live remains frozen while buyer support is email-only. Do not set both
`ANNOLUX_PADDLE_LIVE_ENABLED=true` and
`ANNOLUX_PADDLE_LIVE_APPROVED=true` unless Paddle explicitly accepts the
support arrangement or a business phone is supplied, seller identity/legal
fields have been confirmed, and the paid launch gate is complete.

## Search ownership

- Verify the apex domain in Google Search Console, submit
  `https://annolux.com/sitemap.xml`, and inspect both `/` and `/zh/`.
- Verify Bing Webmaster (or import the verified Search Console property),
  submit the same sitemap, and confirm the committed IndexNow key file.
- Run `ops/indexnow-submit.sh` only after the public canonical routes return
  200. It submits sitemap URLs, not account or preview routes.

## R2 and Better Stack

- Create a private R2 bucket dedicated to encrypted restic backups and a token
  limited to that bucket. Configure repository URL, credentials, and a long
  random restic password only in the host environment.
- Create the monitors, status page, and job heartbeats described in
  `ops/monitoring.md`. Verify the custom status domain before linking it from
  the website.
