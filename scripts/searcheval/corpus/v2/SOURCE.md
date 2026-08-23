# Search generalization corpus v2

V2 preserves the sixty-case v1 `dev` split and applies only the six accepted
dev adjudication overlays from rounds 6 and 13. The ranking implementation was
then frozen at `fec176963894186eb907bb4f6b67be6efebad937` before any v2
held-out query was sent to the Search API.

The forty `blind-v2` cases are entirely new: neither their normalized query
strings nor any of their target URLs occur anywhere in v1. They were authored
from URL, title, and language metadata in the frozen 251,367-page VPS index.
Corpus construction queried the SQLite inventory directly and did not call the
Search API or inspect result order. All forty target URLs were confirmed present
and unique before freeze.

The blind split is fixed at 28 English, 10 Chinese, and 2 mixed-language cases.
It contains 26 technical-documentation, 8 stable-reference, 4 news, and 2
release-intent cases. Combined with dev, the corpus totals are declared in the
manifest and mechanically validated by tests.

`release-gate.json` was frozen with the corpus, before the first held-out run.
The one-time gate requires 40 completed cases, zero request errors,
`hit_at_3 >= 0.65`, `hit_at_10 >= 0.80`, `mrr_at_10 >= 0.55`, and client p95
latency no greater than 1000 ms. The gate is conjunctive: every condition must
pass. No threshold may be changed after observing v2 held-out results.

Only held-out aggregates may be inspected for the initial release decision. If
the gate fails, the per-case artifact may be opened for diagnosis, but that act
makes the split exposed evidence. Ranking may then be tuned only after those
cases are moved into a later dev pool, and a new blind split is required for the
next release gate.

## First release gate

The one-time held-out run completed on 2026-08-15 and passed every frozen
condition: 40 cases, zero errors, `hit_at_3 = 0.825`, `hit_at_10 = 0.85`,
`mrr_at_10 = 0.78125`, and client p95 latency of 532 ms. The immutable VPS
artifact is `/root/search-eval/v2-round1-heldout.jsonl`, SHA-256
`d99840326f63dde34185c3410ace8e7e6ff7e728d451dc3bfe660a01fe765bd6`.
Only the run metadata and aggregate summary were inspected; the forty per-case
rows remain unopened. `gate-result.json` records the complete aggregate
evidence and its code, image, corpus, and index identities.
