# 📁 Annolux Production Recipes & Examples

Ready-to-run examples demonstrating how to integrate Annolux Curated Bilingual Search into AI agents, developer IDEs, and RAG pipelines.

---

## 📑 Example Index

| Example | Tech Stack | Target Use Case |
| :--- | :--- | :--- |
| [**`01-claude-code-literature-research`**](01-claude-code-literature-research/) | Python, Claude Code, Requests | Automated technical survey agent that relies on `fetched_at` timestamps to filter out outdated articles. |
| [**`02-cursor-authority-domain-refactor`**](02-cursor-authority-domain-refactor/) | Cursor, Windsurf, MCP, JSON | Restrict web search strictly to authoritative official documentation (`react.dev`, `go.dev`) for zero-hallucination refactoring. |
| [**`03-production-rag-temporal-pipeline`**](03-production-rag-temporal-pipeline/) | Python, LangChain / LlamaIndex / Custom RAG | Hybrid RAG search fallback: when local vector embeddings return low similarity, query Annolux with zero-cost guarantee on timeouts. |

---

## 🔑 Getting an API Key

All examples require an Annolux API Key (starts with `ann_live_`).
1. Go to [annolux.com](https://annolux.com) and sign in with GitHub or Google.
2. Generate an API Key in the dashboard (new accounts automatically get **1,000 free credits**).
3. Export the key in your environment:
   ```bash
   export ANNOLUX_API_KEY="ann_live_your_actual_key"
   ```
