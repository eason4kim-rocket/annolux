# Search generalization corpus v1

This corpus was frozen before its first 100-query run against the ranking
implementation at `59abfdf`. The first ten `original-tuning` cases are the
known tuning set and therefore belong only to `dev`. The other ninety queries
were authored from indexed URL and title metadata without inspecting their
Search API result order.

The corpus measures target retrieval, not exhaustive relevance. Each case
names a small set of known relevant pages: grade 3 is the primary authority,
grade 2 is an acceptable alternate, and grade 1 is a weak alternate. Results
not listed in `targets` are unjudged and must not be described as irrelevant.

Strata are fixed at 60 dev and 40 held-out cases, with English, Chinese, mixed
language, technical documentation, stable references, news, release intent,
and the known short-token `go` risk represented. During ranking work, inspect
and tune against `dev` only. Held-out per-case output may be retained for audit,
but only held-out aggregates are used at a release gate.

Every completed run must be an exclusive JSONL artifact containing run
metadata, all evaluated case rows, and a final summary. The metadata binds the
result to both case and manifest SHA-256 values, deployed source commit,
evaluation-runner commit, deployed image digest, index page counts and bytes,
and query limit. Never overwrite a round and never edit this v1 corpus after a
completed result exists; create `v2` instead.

Authentication is operational metadata, not evaluation data. The runner reads
`ANNOLUX_API_KEY` from its process environment, sends it only as `X-API-Key`,
and never serializes the credential into a round artifact or error message.
The manifest also fixes a 250 ms interval between requests so the baseline
stays below the production API's default 5 QPS rate limit.
