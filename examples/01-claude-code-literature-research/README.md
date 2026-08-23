# Example 01: Claude Code / CLI Literature Research Agent

This example demonstrates how an AI research agent can query the Annolux curated index to gather evidence-backed literature reviews with explicit `fetched_at` snapshot dates.

---

## 🏃 Quick Start

```bash
# 1. Install dependencies
pip install -r requirements.txt

# 2. Set your Annolux API key
export ANNOLUX_API_KEY="ann_live_your_actual_key"

# 3. Run the research agent
python research_agent.py "Go 1.24 synctest testing package"
```

---

## 💡 Why This Matters

Standard search engines often return un-dated or incorrectly dated blog posts, leading LLMs to synthesize obsolete advice. 

Annolux returns exact `fetched_at` timestamps on every result, allowing your agent to:
1. Sort citations by freshness.
2. Discard articles ingested before a critical library release.
3. Generate verifiable footnotes with exact provenance.
