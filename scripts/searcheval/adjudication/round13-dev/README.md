# Round13 dev follow-up adjudication

This directory records one dev-only label correction discovered after the
round6 adjudication. It does not contain held-out results.

The frozen v1 target for `python-exceptions-tutorial` is the unversioned Python
tutorial URL. Round13 returned the versioned `/3/` URL at rank 1. Both indexed
rows have the same title, cleaned-content SHA-256, and compressed-body size, so
the versioned URL is a canonical alias rather than a remaining ranking error.

The v1 corpus is immutable. `dev-targets-v2-overlay.jsonl` is a complete
replacement target array for this one dev case and is consumed only when the
v2 corpus is assembled.
