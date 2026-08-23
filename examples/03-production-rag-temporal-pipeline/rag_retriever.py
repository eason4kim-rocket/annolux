"""
Annolux Example 03: Production RAG Hybrid Search with Fallback Retrieval

Demonstrates how to integrate Annolux Search into a production RAG pipeline.
When internal vector search confidence is low, the pipeline falls back to
curated live web search with deterministic 0-cost guarantees on network errors.
"""

import os
import sys
from typing import List, Dict, Any
import requests

class AnnoluxRAGRetriever:
    def __init__(self, api_key: str = None, base_url: str = "https://api.annolux.com"):
        self.api_key = api_key or os.environ.get("ANNOLUX_API_KEY", "")
        self.endpoint = f"{base_url.rstrip('/')}/api/v1/search"
        if not self.api_key:
            print("⚠️ Note: Running in demo mode without API key. Set ANNOLUX_API_KEY for live queries.")

    def retrieve(self, query: str, top_k: int = 3, domains: list = None) -> List[Dict[str, Any]]:
        """Query the curated search API and format context documents for LLM injection."""
        if not self.api_key:
            return []

        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json"
        }
        payload = {
            "query": query,
            "limit": min(top_k, 10),
            "deduplicate": True,
            "timeout": 10
        }
        if domains:
            payload["domains"] = domains

        try:
            res = requests.post(self.endpoint, headers=headers, json=payload, timeout=12)
            if res.status_code == 200:
                data = res.json()
                docs = []
                for hit in data.get("results", []):
                    docs.append({
                        "page_content": f"{hit.get('title')}\n{hit.get('snippet')}",
                        "metadata": {
                            "source": hit.get("url"),
                            "fetched_at": hit.get("fetched_at"),
                            "rank": hit.get("rank")
                        }
                    })
                return docs
            elif res.status_code == 402:
                print("❌ Annolux Error: Insufficient credits. Top up or check balance.")
                return []
            else:
                # 4xx / 5xx errors incur 0 credits billed
                print(f"⚠️ Annolux returned HTTP {res.status_code} (0 credits billed).")
                return []
        except requests.RequestException as e:
            # Network timeouts incur 0 credits billed
            print(f"⚠️ Search request failed with network error: {e} (0 credits billed).")
            return []

if __name__ == "__main__":
    retriever = AnnoluxRAGRetriever()
    query = sys.argv[1] if len(sys.argv) > 1 else "DeepSeek MLA Multi-Head Latent Attention implementation"
    
    print(f"🔎 Executing RAG fallback search for: '{query}'...")
    results = retriever.retrieve(query, top_k=3)
    
    print(f"\n📦 Retrieved {len(results)} context chunks for LLM injection:")
    for idx, doc in enumerate(results, 1):
        print(f"\n--- Context Document [{idx}] ---")
        print(f"Source: {doc['metadata']['source']}")
        print(f"Snapshot Time: {doc['metadata']['fetched_at']}")
        print(f"Content: {doc['page_content']}")
