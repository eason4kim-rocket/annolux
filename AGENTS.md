# Annolux Repository Instructions

## Product boundary

- This public repository is the standalone Annolux product.
- Annolux is an agent-first English/Chinese curated Search API. Do not describe it as a hosted edition, rebrand, migration, or companion to any other product.
- The only self-serve commercial capability is standard Search. Indexing is operator-only. Verify and receipts may exist only behind the invitation-only Labs boundary.
- Do not expose Scrape, Batch, Crawl, Map, Extract, Answer, Watch, trust reranking, or a general crawler as customer APIs or MCP tools.
- Keep the public MCP surface to the single `search_web` tool and keep it contract-compatible with standard Search.

## Public-repository safety

- Never commit credentials, production databases, index files, server addresses, SSH details, credential paths, seller identity drafts, or generated secrets.
- Never commit MP4/WAV files or an unapproved blob larger than 5 MiB.
- Public source, generated site, images, metadata, examples, CI, module paths, binaries, environment variables, storage keys, CSS tokens, and events must use Annolux naming only.
- Real operator facts belong only in ignored `OPS.local.md`. Read it before any remote operation; if it is absent, stop instead of guessing.

## Architecture

- Public HTTP wiring lives in `publicapi`; request/response internals remain in `models`; the hosted contract adapter lives in `hostedsearch`; local FTS/index code remains in `searchindex`.
- Account and billing data uses `/data/accounts.db`; the search corpus uses `/data/index.db`. Do not merge these stores.
- SQLite writes are serialized, migrations are forward-only, API keys are stored as hashes, and credit changes are append-only ledger operations.
- Ranking weights and the v2 blind set are frozen. Never tune against blind results.
- Website text comes from `website/content.json`; `tools/sitegen` produces static English and `/zh/` pages. Do not add a runtime translation system.

## Verification and commits

- Work on a `codex/` branch unless the user selects another branch.
- Keep each commit reviewable and scoped: `feat(search):`, `feat(account):`, `feat(site):`, `fix(...)`, `test(...)`, `docs(...)`, or `chore(...)`.
- Before code commits run `go test ./...`, `go vet ./...`, `git diff --check`, the repository policy scan, and the static-site checks.
- Do not push, merge, tag, deploy, or modify remote settings without explicit user authorization.
