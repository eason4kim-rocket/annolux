# LangChain Community Pull Request Submission Draft

**Target Repository**: `langchain-ai/langchain`  
**Target Branch**: `master`  
**PR Title**: `feat(community): add Annolux curated bilingual search tool`

---

## Description
This PR adds the official community integration for **Annolux** — a curated English and Chinese technical search API and MCP built specifically for AI agents with explicit `fetched_at` snapshot timestamps.

### Key Capabilities
- **Curated Bilingual Corpus**: High-signal English & Chinese technical and knowledge index with zero SEO spam.
- **Explicit Provenance**: Every search snippet includes an immutable snapshot date (`fetched_at`) to eliminate temporal hallucinations in RAG and agent workflows.
- **Async & Sync Support**: Provides both `AnnoluxSearchRun` (clean text) and `AnnoluxSearchResults` (structured JSON) with full async `_arun` support.

---

## Files to Copy into `langchain-ai/langchain`

1. `libs/community/langchain_community/utilities/annolux_search.py` -> [`packages/langchain-annolux/langchain_annolux/utilities.py`](file:///Users/liulin/Documents/code/annolux/packages/langchain-annolux/langchain_annolux/utilities.py)
2. `libs/community/langchain_community/tools/annolux_search/tool.py` -> [`packages/langchain-annolux/langchain_annolux/tool.py`](file:///Users/liulin/Documents/code/annolux/packages/langchain-annolux/langchain_annolux/tool.py)
3. `libs/community/langchain_community/tools/annolux_search/__init__.py` -> [`packages/langchain-annolux/langchain_annolux/__init__.py`](file:///Users/liulin/Documents/code/annolux/packages/langchain-annolux/langchain_annolux/__init__.py)

---

## PR Description Body

```markdown
## Overview
Adds `AnnoluxSearchRun` and `AnnoluxSearchResults` tools to `langchain_community`.

Annolux is a curated web search API designed for AI Agents. Unlike general search engines, Annolux returns pre-cleaned snippets accompanied by explicit `fetched_at` snapshot dates.

## Usage Example
```python
from langchain_community.tools.annolux_search import AnnoluxSearchRun

search = AnnoluxSearchRun()
print(search.run("DeepSeek R1 reinforcement learning architecture"))
```

## Testing
Unit tests included with mocked responses covering both sync and async execution.
```
