# Example 03: Production RAG Hybrid Search with Fallback Retrieval

This example demonstrates how to integrate Annolux into a production RAG system as an external knowledge fallback.

---

## 🏃 Quick Start

```bash
# 1. Install dependencies
pip install -r requirements.txt

# 2. Set your Annolux API key
export ANNOLUX_API_KEY="ann_live_your_actual_key"

# 3. Test RAG retriever
python rag_retriever.py "PagedAttention vLLM memory benchmark"
```

---

## 🛡️ Production Guarantees

1. **Zero-Cost on Timeouts**: If a network timeout or upstream server glitch occurs, Annolux commits 0 credits to the ledger.
2. **Clean Text Snippets**: Responses are pre-cleaned Markdown snippets without CSS or navigation noise, fitting tightly into LLM context budgets.
3. **Structured Provenance**: Every hit carries a verifiable `fetched_at` timestamp.
