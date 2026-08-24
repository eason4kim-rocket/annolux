# LlamaIndex Pull Request Submission Draft

**Target Repository**: `run-llama/llama_index`  
**Target Branch**: `main`  
**PR Title**: `feat(tools): add Annolux search tool spec (llama-index-tools-annolux)`

---

## Description
This PR adds the `AnnoluxToolSpec` integration for LlamaIndex to query the Annolux curated bilingual search API.

### Features
- Native `BaseToolSpec` implementation exporting `annolux_search`.
- Returns native `Document` objects with metadata (`title`, `url`, `fetched_at`, `domain`, `score`).
- Allows LlamaIndex Agents to retrieve timestamped external knowledge with zero hallucination.

---

## Files to Copy into `llama_index`

1. `llama-index-integrations/tools/llama-index-tools-annolux/llama_index/tools/annolux/base.py` -> [`packages/llama-index-tools-annolux/llama_index/tools/annolux/base.py`](file:///Users/liulin/Documents/code/annolux/packages/llama-index-tools-annolux/llama_index/tools/annolux/base.py)
2. `llama-index-integrations/tools/llama-index-tools-annolux/llama_index/tools/annolux/__init__.py` -> [`packages/llama-index-tools-annolux/llama_index/tools/annolux/__init__.py`](file:///Users/liulin/Documents/code/annolux/packages/llama-index-tools-annolux/llama_index/tools/annolux/__init__.py)
3. `llama-index-integrations/tools/llama-index-tools-annolux/pyproject.toml` -> [`packages/llama-index-tools-annolux/pyproject.toml`](file:///Users/liulin/Documents/code/annolux/packages/llama-index-tools-annolux/pyproject.toml)

---

## PR Description Body

```markdown
## Summary
Adds `llama-index-tools-annolux` package providing `AnnoluxToolSpec` for Agent function calling and external web retrieval.

## Example
```python
from llama_index.tools.annolux import AnnoluxToolSpec

tool_spec = AnnoluxToolSpec()
docs = tool_spec.annolux_search("Rust tokio runtime best practices", limit=5)
```
```
