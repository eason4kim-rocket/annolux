# Round6 dev miss adjudication

This directory records the manual adjudication of the 18 `dev` misses from
Search generalization `round6`. It does not contain the production round
artifact or any credential.

## Frozen inputs

- Corpus: `search-generalization-v1`
- Corpus SHA-256: `f041b7ae002bb6362ec37663ec97589d7e9102bafe01bab90815b15eaff7fa23`
- Round: `round6`
- Round SHA-256: `fffa428f7107a6c8f24716968e96da218cc52253ec945aecce60d165da487250`
- Deployed Search commit: `59abfdf651f5f7415597afa23949c463db084744`
- Evaluation limit: 10

The v1 held-out rows were already revealed during the round6 audit. They are
excluded from this adjudication and from all tuning, but they can no longer be
described as a blind release set. A future v2 must contain a newly authored,
never-inspected held-out split.

## Method

1. Select only rows with `split=dev` and `verdict=miss` from the pinned round.
2. Confirm each declared target by exact URL in the VPS index using a read-only
   SQLite connection. All 18 targets were present.
3. For suspected aliases, compare the indexed title, cleaned-content hash, and
   stored body size. Four pairs were byte-equivalent after cleaning.
4. Inspect the two Android background-work pages as separate official pages;
   the returned `Background tasks overview` is a more direct answer, not a
   content-identical alias.
5. Run the same 18 dev queries once at `limit=20` as a diagnostic. This is not
   a new evaluation round and no held-out row was queried.

## Result

| Classification | Count |
|---|---:|
| `alias` | 4 |
| `better_answer` | 1 |
| `true_gap` | 0 |
| `ranking_error` | 13 |

`dev-targets-v2-overlay.jsonl` contains complete replacement `targets` arrays
for the five gold corrections. It is input for a future v2 build; frozen v1 is
never edited. `true-gaps.json` is intentionally empty, so this adjudication
does not authorize a targeted crawl. Ranking weights remain frozen until the
separate top-k stability card is resolved.
