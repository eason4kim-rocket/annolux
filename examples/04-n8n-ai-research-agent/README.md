# Example 04: n8n AI Research Agent with Temporal Grounding

Build a production-grade AI Research Assistant in **n8n** powered by Annolux curated search.

---

## 🚀 Quick Setup (1 Minute)

### 1. Import Workflow into n8n
1. Open your n8n instance.
2. Click **Add workflow** > **Import from File...** (or copy contents of [`workflow.json`](workflow.json) and paste directly on the canvas).

### 2. Configure Credentials
1. **Annolux API**: Add your Annolux API Key from [annolux.com/account](https://annolux.com/account).
2. **OpenAI / Anthropic**: Add your preferred LLM provider credentials.

### 3. Test the Agent
Click **Chat** at the bottom of the canvas and ask:
> *"What are the latest performance benchmarks for Go 1.26 vs Rust in 2026?"*

The agent will query Annolux, extract verified snapshot dates, and return an accurate response with temporal citations.

---

## 🛠️ Architecture

```
[User Chat] ──> [AI Research Agent] ──> [Annolux Search Tool] (api.annolux.com)
                       │                       │
                       ▼                       ▼
              [OpenAI/Claude LLM]     [Curated Web Corpus + fetched_at]
```
