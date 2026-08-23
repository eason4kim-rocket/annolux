# Annolux launch status

Status meanings:

- `DONE_LOCAL`: implemented and verified in the clean staging source tree.
- `DONE_PRIVATE`: executed and verified in isolated private host staging without
  changing public traffic.
- `DONE_PUBLIC`: completed on a public project resource such as GitHub; this
  does not mean that customer traffic has been cut over.
- `READY_TO_RUN`: automation exists; it needs a built image, copied index, or
  isolated deployment to produce real evidence.
- `WAITING_USER`: an external identity, account, legal or authorization action
  cannot be completed from source code.
- `FROZEN`: intentionally prohibited until its prerequisite gate passes.

This file never claims that local code is already public production.

## Independent repository and product boundary

| Item | Status | Evidence / next action |
|---|---|---|
| Independent module, binaries, containers and environment namespace | `DONE_LOCAL` | Module and five binaries use Annolux naming. |
| Public Search-only HTTP and one-tool MCP contract | `DONE_LOCAL` | No customer scraping/crawling/extraction routes are registered; legacy commercial DTOs were removed and CI blocks their return. |
| Legacy-name, secret, IP, media and >5 MiB policy gates | `DONE_LOCAL` | `scripts/repo-policy.sh`; CI blocks regressions. |
| CI supply-chain pins | `DONE_LOCAL` | Every third-party Action is pinned to a full commit SHA; container releases emit SBOM and provenance. |
| Clean independent Git history | `DONE_PUBLIC` | The public history starts from one independently scanned source commit followed only by reviewable Annolux operations fixes; no prior history or large media was imported. |
| Public GitHub repository and first push | `DONE_PUBLIC` | `eason4kim-rocket/annolux` is public; Actions, CodeQL, vulnerability alerts, secret scanning and push protection are enabled. |
| GHCR API and MCP image builds | `DONE_PRIVATE` | Multi-architecture images with SBOM and provenance were built from the clean root commit; immutable digests are recorded in Actions. Web publication correctly remains frozen by the legal gate. |
| Private GHCR deployment access | `WAITING_USER` | Keep the package private. Create a dedicated expiring classic PAT with only `read:packages`, grant its owner package read access, then prove an authenticated immutable-digest pull with the non-persisting helper. |
| MCP Registry metadata and OCI ownership label | `FROZEN` | `server.json` passes the official 2025-12-11 schema, but registry publication remains frozen while the referenced OCI image is private. |

## Search, index and quality

| Item | Status | Evidence / next action |
|---|---|---|
| Curated bilingual FTS, frozen ranking and bounded candidate window | `DONE_LOCAL` | `hostedsearch`, `searchindex`, ranking lock. |
| `freshness` rejected; `fetched_at` returned | `DONE_LOCAL` | Public contract and tests. |
| Internal indexer, robots/fairness/filtering and 82%/25 GiB guard | `DONE_LOCAL` | Operator-only binary and tests. |
| Known-page-only refresh mode | `DONE_LOCAL` | No URL discovery/frontier leasing in refresh mode. |
| Copy/index integrity workflow | `DONE_PRIVATE` | The source remained in place; the private copy passed byte-for-byte hash, SQLite integrity, FTS/language and WAL checks. Evidence is mode-0600 on the host, not in Git. |
| v2 blind rerun against the copied index | `DONE_PRIVATE` | Frozen heldout: 40 cases, zero errors, Hit@1 72.5%, Hit@3 82.5%, Hit@10 85%, MRR@10 0.78125, p95 356 ms; the frozen ranking-lock gate passed. No ranking changes were made. |
| Single-VPS launch load profile | `DONE_PRIVATE` | Twelve independent accounts, 100/100 HTTP 200, zero transport errors and 5xx, 37.7 requests/s, p95 485 ms and p99 607 ms. |

## Accounts, credits and security

| Item | Status | Evidence / next action |
|---|---|---|
| SQLite WAL ledger and forward-only migrations | `DONE_LOCAL` | Separate `accounts.db`, foreign keys, serialized writes, newer-schema refusal. |
| Signup and paid credit batches, expiry order, reserve/commit/release | `DONE_LOCAL` | Concurrent, timeout, failure, expiry, refund and crash-recovery tests. |
| Key hash/show-once/revoke/max-three | `DONE_LOCAL` | HMAC-only storage and lifecycle tests. |
| Google + GitHub OAuth, PKCE/state/nonce, no email merge | `DONE_LOCAL` | Account-bound linking, verified email and replay tests. |
| Session, exact Origin, CSRF and CORS | `DONE_LOCAL` | Secure `__Host-` cookie and hostile-origin tests. |
| Search debit and failure contract on spare API | `DONE_PRIVATE` | 401 and unsupported freshness returned without a debit; one successful Search committed exactly one credit and returned used/remaining/request-id headers plus `fetched_at`. |
| Private evaluation ledger integrity | `DONE_PRIVATE` | 13 accounts/keys, 181 committed Search reservations, zero pending reservations, and `annolux-admin integrity` passed after blind, load and contract tests. |
| OAuth application credentials and real-provider callback exercise | `WAITING_USER` | Create both provider applications using `deploy/external-gates.md`. |

## Billing

| Item | Status | Evidence / next action |
|---|---|---|
| Paddle Sandbox adapter, raw signature and event idempotency | `DONE_LOCAL` | Duplicate/out-of-order lifecycle tests and reconciliation command. |
| No mid-period switching, quantity change or proration | `DONE_LOCAL` | Server rejects plan changes; portal must be cancellation-only. |
| Pro/Scale Sandbox products, prices, token and webhook secret | `WAITING_USER` | Create them in the authenticated Paddle dashboard. |
| Full Sandbox checkout, renewal, cancel, refund and chargeback E2E | `WAITING_USER` | Runs after Sandbox IDs/secrets exist. |
| Verified seller identity in legal pages | `WAITING_USER` | Supply confirmed legal name, country and governing law privately. |
| Paddle Live | `FROZEN` | Requires explicit email-only support acceptance or a business phone, plus every paid gate. |

## Website, legal and SEO

| Item | Status | Evidence / next action |
|---|---|---|
| Default English and static `/zh/` pages | `DONE_LOCAL` | Full content works without JavaScript. |
| Home/Pricing independent $0, $29/20k and $99/100k cards | `DONE_LOCAL` | No combined price or unsupported Scale entitlement. |
| Docs, Benchmarks, Blog, Changelog, About, Contact, Crawler, Security, Status | `DONE_LOCAL` | All have bilingual static routes. |
| Privacy, Terms, Refund and Acceptable Use | `DONE_LOCAL` | Billing/OAuth/retention/third-party disclosures included. |
| Account, Login, Playground and Checkout success `noindex/no-store` | `DONE_LOCAL` | Generated headers/meta checks. |
| Canonical, reciprocal hreflang, sitemap, robots, RSS, JSON-LD and OpenAPI | `DONE_LOCAL` | Static-site and API contract tests. |
| Browser accessibility and quality baseline | `DONE_LOCAL` | Six representative routes pass axe; Lighthouse en 99/100/100/100 and zh 97/100/100/100. |
| One-hop edge redirects and true 404 | `READY_TO_RUN` | Apply account-side rules and verify public matrix. |
| Release legal substitution | `WAITING_USER` | Release generator refuses unresolved seller placeholders. |
| Four support email aliases | `WAITING_USER` | Configure exact routes and confirm the Gmail destination. |
| Google Search Console, Bing and IndexNow ownership | `DONE_PUBLIC` | GSC verified via DNS TXT and sitemap submitted by the owner on 2026-08-20; Bing imported from GSC; an IndexNow submission of every sitemap URL succeeded from `ops/indexnow-submit.sh`. |

## Content and launch growth

| Item | Status | Evidence / next action |
|---|---|---|
| Annolux Lab Notes and four evidence-based launch articles | `DONE_LOCAL` | Bilingual static posts, BlogPosting schema and RSS. |
| Developer beta plan, support playbook, metrics and fact pack | `DONE_LOCAL` | Aggregate metrics avoid storing query text or PII. |
| GitHub/HN/Product Hunt/V2EX launch copy and asset checklist | `DONE_LOCAL` | User publishes final posts after beta thresholds. |
| 10–20 developer beta and usage thresholds | `WAITING_USER` | Invite real users; require 10 first calls and 3 repeat-day users before broad launch. |

## Infrastructure and recovery

| Item | Status | Evidence / next action |
|---|---|---|
| Isolated deployment root and spare-port API | `DONE_PRIVATE` | A non-root, read-only-root, capability-free API is healthy on loopback with the copied read-only index and isolated accounts database. It uses a commit-built temporary image until the private GHCR digest is pulled with the read-only deploy identity; no public route changed. |
| API/web/tunnel health checks and Search-aware readiness | `DONE_LOCAL` | Runtime definitions and smoke suite. |
| Runtime readiness fails closed | `DONE_PRIVATE` | A one-shot API with a non-regular index exited nonzero; the healthy spare API has zero restarts and the copied index SHA remains unchanged with a zero-byte WAL. |
| Non-root web runtime under a read-only, capability-free container | `DONE_PRIVATE` | Official unprivileged Nginx and all build bases are digest-pinned. A private loopback-only runtime test passed health, EN/ZH content, Privacy, 404, redirect and security-header checks with UID 101, no capabilities and a read-only root; CI repeats the same constraints. |
| Consistent local SQLite snapshot and empty-directory restore | `DONE_PRIVATE` | A live snapshot restored into a fresh directory, retained mode 0600, matched aggregate counts, passed integrity, and was securely removed after a private audit artifact was written. |
| Encrypted R2/restic backup and off-site restore | `WAITING_USER` | Scripts, timers and retention policy are ready; a private R2 bucket, scoped token and restic password are required. |
| Better Stack monitors, independent status and job heartbeats | `WAITING_USER` | Create/authorize monitoring account resources. |
| R2 bucket and scoped backup token | `WAITING_USER` | Create private bucket/token; never commit values. |
| Exact-GHCR API/web staging and route rollback drill | `WAITING_USER` | API behavior is proven on spare loopback. Exact-digest staging now requires only authenticated private-package read access; release web, temporary ingress and rollback still require verified legal substitution, OAuth and monitoring. Existing services remain untouched. |
| Dedicated Ed25519 deploy identity | `DONE_PRIVATE` | Key-only login was verified while password login remains available for rollback. The private key and host details are not tracked. |
| Disable password root login | `WAITING_USER` | Final confirmation only after key and provider-console recovery work. |

## Current release verdict

- Source and private core gates: **pass**. Race tests, vet/build, called-code vulnerability
  scan, npm audit, gitleaks, repository policy, frozen ranking, MCP metadata,
  static-site and browser accessibility checks are green. The copied index,
  v2 blind gate, debit contract, ledger integrity, load profile and local
  restore drill also pass. The release builder correctly fails closed while
  verified legal identity is absent.
- Public source: **live**. Customer product traffic: **not cut over**. External
  OAuth, legal identity, support email, monitoring/SEO ownership, private GHCR
  deploy access, exact-digest deployment and rollback evidence remain gated.
- Paid product: **Sandbox code ready, account-side setup pending; Live frozen**.
- No blocked external item permits weakening a security, legal, billing,
  quality, recovery or support gate.
