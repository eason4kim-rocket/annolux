# Operations

- `backup-accounts.sh` creates a consistent `VACUUM INTO` snapshot, verifies it,
  uploads it with restic and applies retention. It never copies an active WAL.
- `restore-drill.sh` restores the latest encrypted backup into an empty staging
  directory and runs ledger integrity checks without replacing production data.
- `migrate-index.sh` copies (never moves) a checkpointed index and requires
  matching size, SHA-256, SQLite integrity, FTS and language counts.
- `clean-index.sh` copies the served index and applies the crawler's current
  junk rule to the copy only: pages that today's rule rejects, their FTS rows
  and the frontier entries that would fetch them again are deleted from the
  candidate, which is then vacuumed and verified. The served index is read
  once and never written, so a candidate that fails the held-out gate is
  discarded rather than rolled back. `annolux-indexclean` is dry-run by
  default and takes every decision from `indexer.JunkURL`, never from a copy
  of that rule.
- `refresh-existing.sh` refreshes known URLs only; it cannot lease or discover
  frontier URLs and retains the 82% / 25 GiB disk stop.
- reconciliation and all recurring jobs have separate external heartbeat URLs.
- `smoke.sh` checks web/API readiness, canonical public routes, true 404s and,
  only with explicit confirmation, one real credit-charging Search.
- `run-v2-gate.sh` records a new immutable held-out artifact and checks it
  against the frozen ranking and quality gate; it never changes weights. A
  production host does not need the Go toolchain: set absolute executable
  paths in `ANNOLUX_ADMIN_BIN`, `ANNOLUX_EVALUATOR_BIN`, and
  `ANNOLUX_EVALGATE_BIN`, plus `ANNOLUX_EVAL_MANIFEST` when the frozen corpus
  is stored outside the source tree.
- The query log (`/data/querylog.db`, 0600) records per-request retrieval
  telemetry: query text, language, domain filters, status, returned document
  hashes with raw scores, candidate counts, and latency. It holds no keys,
  IPs, or user agents; the API redacts query text after thirty days on its
  own, and `annolux-admin querylog-expire PATH [DAYS]` /
  `annolux-admin querylog-stats PATH` cover manual runs and verification.
  Writes are asynchronous and droppable — a full queue or failing disk never
  affects Search. Operator traffic must send `X-Annolux-Ops: <label>` (the
  smoke script already does) so probes stay out of the real distribution;
  evaluation runs stay on the staging container, never production.
- `monitoring.md` defines external checks, heartbeat windows and PII-free beta
  metrics. `ssh-hardening.md` keeps password/root changes behind verified
  key-only and provider-console recovery.
- `indexnow-submit.sh` submits only URLs from the generated canonical sitemap;
  its verification key is public by protocol design.
- account closure is an explicit privacy operation: first cancel any active
  paid subscription, then run `annolux-admin close-account ACCOUNT_ID CONFIRM`.
  It revokes sessions and keys, removes live OAuth profile data, and keeps
  HMAC identity tombstones plus legally required ledger/payment records.

Account snapshots retain 14 daily, 8 weekly, and 12 monthly copies. Operational
request/security logs, when enabled by the host, retain no more than 30 days.
These values are part of the published Privacy Policy; changing them requires a
policy review as well as an operations change.

Install the service/timer pairs only after filling the ignored host `.env` and
testing each oneshot manually. R2 credentials, heartbeat URLs and server facts
must never enter Git.

Private GHCR authentication is deliberately separate from `.env`. Use
`deploy/pull-private-ghcr.sh` with a dedicated `read:packages` credential on
standard input, then start the verified local digest with Compose
`--pull never`. The helper does not persist registry authentication.
