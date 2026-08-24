# Dify Builtin Tool Pull Request Submission Draft

**Target Repository**: `langgenius/dify`  
**Target Branch**: `main`  
**PR Title**: `feat(tools): add Annolux curated bilingual search provider`

---

## Description
This PR adds **Annolux** as a built-in search tool provider in Dify.

Annolux is a curated English and Chinese technical search engine designed for LLM Agents and RAG pipelines, returning clean snippets with explicit `fetched_at` snapshot timestamps to prevent hallucinations.

### Directory in Dify
`api/core/tools/provider/builtin/annolux/`:
- `annolux.yaml`: Provider config and API key credential setup
- `_assets/icon.svg`: Annolux brand icon
- `tools/annolux_search.yaml`: Search tool parameter schemas (`query`, `limit`, `domains`)
- `tools/annolux_search.py`: Tool implementation returning structured results

---

## PR Description Body

```markdown
## Summary
Adds `annolux` as a built-in search tool provider under `api/core/tools/provider/builtin/annolux/`.

Annolux provides high-signal bilingual (EN/ZH) technical web search with explicit `fetched_at` timestamps for agent grounding.

## Verification
- Tested locally in Dify workflow / agent tool invocation with mock and live API keys.
- Confirmed error handling for invalid/missing keys and timeouts.
```
