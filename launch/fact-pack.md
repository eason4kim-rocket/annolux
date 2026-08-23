# Annolux fact pack

Checked against the launch source on 2026-08-16. Recheck all dynamic values on
launch day.

## One sentence

Annolux is a focused English/Chinese Search API for agents and RAG systems,
backed by a transparent local index and a deliberately small request contract.

## Product facts

- Standard self-serve Search is `POST /api/v1/search`.
- Results contain title, URL, snippet, optional score, and `fetched_at`.
- The hosted index is periodically refreshed; it is not realtime or whole-web.
- `freshness` is rejected as `UNSUPPORTED_OPTION`, never silently ignored.
- A successful 2xx Search costs one credit. Errors and timeouts cost zero.
- Free: 1,000 permanent signup credits; no card and no overage.
- Pro: $29 per billing period for 20,000 expiring credits.
- Scale: $99 per billing period for 100,000 expiring credits.
- Every plan has 5 RPS, burst 10, and at most three active API keys.
- Scale changes unit economics only; it has no higher burst, priority support,
  or SLA.
- Google and GitHub are the only sign-in methods. Equal email addresses are not
  automatically merged.
- `annolux-mcp` exposes one tool, `search_web`, matching standard Search.
- Verify and receipts are invitation-only Labs, not paid-plan entitlements.
- Paddle Sandbox is implemented. Paddle Live is frozen pending seller support
  requirements and the paid-release gate.

## Search evidence

- Current migration target: approximately 251,368 pages, including 215,485
  English and 35,883 Chinese pages. Recount the copied index before publishing.
- Frozen v2 corpus: 100 queries, split into 60 dev and 40 held-out.
- Immutable first held-out gate: Hit@1 72.5%, Hit@3 82.5%, Hit@10 85.0%,
  MRR@10 0.78125, client p95 532 ms, zero request errors.
- Latest no-tuning prelaunch rerun: identical retrieval metrics, client p95
  356 ms, zero request errors.
- These are target-retrieval measurements for one finite corpus, not universal
  relevance or whole-web recall claims.

## Claims not allowed

- “Realtime,” “complete web,” “best search,” or “verified facts.”
- Any uptime or SLA claim.
- Any customer count, logo, quote, or case study without written permission.
- Any paid-plan feature outside standard Search.
- Any claim that Paddle Live checkout is available while the freeze is active.
