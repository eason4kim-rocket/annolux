# Privacy-preserving launch metrics

No cookie analytics, fingerprinting, purchased lists, or hidden pixels. Use
aggregate web request counts plus account, key, and immutable credit-ledger
facts. Never store raw API keys or publish identifiable queries.

## Funnel

1. Unique beta invite acceptances (manual consent record).
2. Accounts created.
3. Accounts that create a first API key.
4. Accounts with a first successful Search.
5. Accounts with successful Search on at least two separate UTC days.
6. Accounts that exhaust or intentionally purchase credits after Live opens.

## Reliability

- `/ready` external availability.
- Search request count, 2xx/4xx/5xx distribution, p50/p95/p99.
- Credit reservations released by crash recovery.
- Paddle event failures/replays and reconciliation drift.
- Backup heartbeat age and last successful empty-directory restore drill.

## Decision gates

- Public free launch: 10 first-call accounts, 3 repeat users, v2 regression
  gate pass, p95 at most one second, 5xx below 1%, and no unresolved SEV-1.
- Product Hunt: seven stable days after free launch and support backlog under
  control.
- Paddle Live: every paid gate, Sandbox E2E, legal identity, seller support,
  encrypted backup, and restore drill independently pass.
