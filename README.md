# Annolux

Annolux is a curated English/Chinese Search API for AI agents and RAG systems.
It returns ranked titles, URLs, snippets, and the time each page was fetched.
The hosted index is intentionally bounded and transparent; it is not a claim
of real-time or whole-web coverage.

- Website: [annolux.com](https://annolux.com)
- API: [api.annolux.com](https://api.annolux.com)
- OpenAPI: [annolux.com/openapi.json](https://annolux.com/openapi.json)
- License: AGPL-3.0 (engine and evaluator) · MIT (MCP client: `cmd/annolux-mcp`, `models`, `evidence`, `receipts`)

## Product contract

The self-serve product has one commercial endpoint:

```http
POST /api/v1/search
Authorization: Bearer ann_live_…
Content-Type: application/json
```

```json
{
  "query": "rust ownership",
  "domains": ["doc.rust-lang.org"],
  "deduplicate": true,
  "limit": 5,
  "timeout": 10,
  "ranking": "default"
}
```

Supported fields are deliberately bounded:

| Field | Contract |
|---|---|
| `query` | 1–50 words |
| `domains` | optional hostname filters |
| `deduplicate` | boolean, defaults true |
| `limit` | 1–10 |
| `timeout` | 1–30 seconds |
| `ranking` | `default` or `provider` |

`freshness`, content extraction, verification and trust reranking are not
part of standard Search. Unsupported options fail with
`UNSUPPORTED_OPTION`; they are never silently ignored.

A successful response includes:

- `X-Annolux-Credits-Used: 1`
- `X-Annolux-Credits-Remaining`
- a server-generated `X-Request-ID`
- `fetched_at` on every result

Only a successful 2xx Search commits one credit. Invalid, forbidden,
rate-limited, timed-out and server-error requests release the reservation.
Insufficient balance returns 402; there is no metered overage.

## Accounts and plans

Sign in with Google or GitHub. Identities are keyed by Google `sub` and the
numeric GitHub user ID; matching email addresses are never merged
automatically. A second identity can only be linked from an authenticated
account.

| Plan | Price | Credits |
|---|---:|---:|
| Free | $0 | 1,000 once, no expiry |
| Pro | $29/month | 20,000 per billing period |
| Scale | $99/month | 100,000 per billing period |

Paid credits expire at the end of their billing period and do not roll over.
All plans use 5 RPS and burst 10. Scale changes unit economics only; it does not
promise a different rate limit, priority support, or an SLA. Each account may
have at most three active API keys.

Paddle checkout is implemented for Sandbox. Live billing remains mechanically
frozen until the seller-support requirement and the paid-launch checklist are
cleared. Free signup and Search do not depend on Paddle Live.

## MCP

`annolux-mcp` exposes exactly one tool, `search_web`, with the same fields
and limits as standard Search.

```json
{
  "mcpServers": {
    "annolux": {
      "command": "annolux-mcp",
      "env": {
        "ANNOLUX_API_URL": "https://api.annolux.com",
        "ANNOLUX_API_KEY": "ann_live_REPLACE_ME"
      }
    }
  }
}
```

No general scraping, crawling, extraction, verification, or trust tool is
registered in the public MCP server.

The release OCI image is
`ghcr.io/eason4kim-rocket/annolux:mcp-<version>`. Its official Registry
metadata lives in `server.json`; publish that metadata only after the matching
immutable GHCR image exists. The image carries the Registry ownership label
`io.modelcontextprotocol.server.name=io.github.eason4kim-rocket/annolux`.

## Architecture

The public deployment is intentionally small:

```text
Cloudflare Tunnel
  ├── annolux.com     -> static nginx site
  └── api.annolux.com -> one Go API process
                            ├── /data/index.db
                            └── /data/accounts.db
```

- `index.db`: local bilingual FTS index and operator-only frontier.
- `accounts.db`: accounts, OAuth identities/challenges, sessions, API-key
  hashes, credit batches/reservations/ledger, checkout intents and Paddle
  event state.
- No PostgreSQL, Redis, GPU, public admin panel, password database, or
  background URL-discovery service is required.
- API keys are displayed once and stored as HMAC-SHA256 hashes.
- Credit writes are serialized SQLite transactions using
  reserve → commit/release semantics.
- Paddle webhook bodies are signature-verified before JSON processing; event
  and entity IDs make delivery idempotent. A daily API reconciliation repairs
  missed webhook deliveries.

The operator-only indexer keeps robots enforcement, per-domain fairness,
admission filters and the 82%-used / 25-GiB-free disk stop. Routine maintenance
uses `-refresh-existing`, which only revisits known URLs and cannot discover
or lease frontier rows.

## Search quality

The migrated baseline contains 251,368 pages: 215,485 English and 35,883
Chinese. The frozen v2 blind set has 40 queries. The immutable first release
gate and the latest no-tuning prelaunch rerun were:

| Metric | First gate | Latest rerun |
|---|---:|---:|
| Hit@1 | 72.5% | 72.5% |
| Hit@3 | 82.5% | 82.5% |
| Hit@10 | 85.0% | 85.0% |
| MRR@10 | 0.78125 | 0.78125 |
| p95 | 532 ms | 356 ms |

These are retrieval-evaluation results, not general quality claims. The blind
set must be rerun before deployment and must not be used to tune ranking.
Hosted acceptance requires no metric regression, p95 at most one second and
fewer than 1% 5xx responses under the launch load profile.

## Commands

| Binary | Purpose |
|---|---|
| `annolux-api` | public Search, account, OAuth and billing API |
| `annolux-index` | operator-only index build or known-page refresh |
| `annolux-mcp` | single-tool MCP client |
| `annolux-healthcheck` | container readiness probe |
| `annolux-admin` | local account, ledger, billing and index operations |

```sh
make build
make site
make verify
```

The API requires a search index, a separate accounts database path and at
least 32 random bytes encoded into `ANNOLUX_ACCOUNT_HMAC_SECRET`. See
`.env.example` for the complete configuration contract.

## Website and SEO

`website/content.json` is the sole localized content source.
`tools/sitegen` produces readable static HTML for English routes and matching
`/zh/` routes. Every indexable page has a self-canonical and reciprocal
`en`, `zh-CN`, and `x-default` hreflang. Account, Playground, login and
checkout pages are `noindex` and `no-store`.

The site includes Home, Docs, Pricing, Benchmarks, Annolux Lab Notes,
Changelog, About, Contact, Crawler, Security, Status, Privacy, Terms, Refund,
Acceptable Use, Login, Account, Playground, checkout success, 404 and OpenAPI.
Release generation refuses to run until verified seller identity and
jurisdiction values are supplied.

## Labs

Verification, receipts and evidence-state work may be operated as
invitation-only Annolux Labs. They are not part of Pro or Scale, are not
registered as public routes or MCP tools, and must not be presented as a paid
plan entitlement.

## Operations and release gates

- [deploy/README.md](deploy/README.md) describes the isolated deployment.
- [deploy/launch-gates.md](deploy/launch-gates.md) separates free and paid
  launch gates.
- [deploy/external-gates.md](deploy/external-gates.md) lists exact third-party
  account fields and callbacks without storing any IDs or secrets.
- [ops/README.md](ops/README.md) covers consistent backups, restore drills,
  reconciliation and known-page refresh.
- [SECURITY.md](SECURITY.md) defines private vulnerability reporting.

Production images remain private and are pulled by a dedicated, read-only GHCR
deploy identity, then addressed by immutable commit digests. The index is
copied into a new data directory and verified by size, SHA-256, SQLite
integrity, FTS counts and language counts. Existing data and containers remain
available for rollback until the new route is stable.

## License

Apache License 2.0. See [LICENSE](LICENSE).

## License

- The repository as a whole — the search engine, indexer, public API server,
  and evaluator — is licensed under **AGPL-3.0** (see `LICENSE`).
- The MCP client and the packages it links against — `cmd/annolux-mcp`,
  `models`, `evidence`, and `receipts` — are licensed under **MIT** (see the
  `LICENSE` file in each of those directories), so the `annolux-mcp` binary
  and its dependency closure carry no copyleft obligations for users.
