<div align="center">

# ⚡ Annolux

**Curated English & Chinese Search API and MCP for AI Agents & RAG Systems**

*Search that can show its work. Every result carries an explicit `fetched_at` timestamp and provenance.*

[![Go Version](https://img.shields.io/github/go-mod/go-version/eason4kim-rocket/annolux?style=flat-square&logo=go)](https://golang.org)
[![NPM Version](https://img.shields.io/npm/v/annolux-mcp?style=flat-square&logo=npm&color=CB3837)](https://www.npmjs.com/package/annolux-mcp)
[![Smithery Badge](https://smithery.ai/badge/@eason4kim/annolux)](https://smithery.ai/server/@eason4kim/annolux)
[![Glama MCP](https://glama.ai/mcp/servers/eason4kim-rocket/annolux/badge)](https://glama.ai/mcp/servers/eason4kim-rocket/annolux)
[![Add to Cursor](https://img.shields.io/badge/Add%20to-Cursor-000000?style=flat-square&logo=cursor&logoColor=white)](cursor://anysphere.cursor-deeplink/mcp/install?name=annolux&config=eyJjb21tYW5kIjoibnB4IiwiYXJncyI6WyIteSIsImFubm9sdXgtbWNwIl19)
[![MCP Protocol](https://img.shields.io/badge/MCP-2025--12--11-000000?style=flat-square&logo=anthropic)](https://modelcontextprotocol.io/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg?style=flat-square)](LICENSE)
[![Free Tier](https://img.shields.io/badge/Free_Tier-1%2C000_Credits-brightgreen?style=flat-square)](https://annolux.com)

[🌐 Website](https://annolux.com) • [📖 API Docs](https://annolux.com/docs) • [⚡ MCP Quickstart](#-mcp-integration) • [📊 Frozen Benchmarks](#-search-quality--frozen-benchmarks) • [📁 Examples](examples/) • [🇨🇳 中文文档](README_zh.md)

</div>

---

## 💡 Why Annolux?

Current web search APIs for AI agents suffer from three fatal flaws:
1. **Garbage in, garbage out**: Commercial search engines index millions of SEO farms, scraped spam, and auto-generated noise that pollute LLM context windows.
2. **Missing time-provenance**: LLMs hallucinate current state because search APIs omit the exact snapshot timestamp (`fetched_at`).
3. **Predatory billing**: Paying full price for failed requests, empty outputs, or rate-limited retries.

**Annolux solves this with an agent-first curated approach:**
- 🛡️ **Curated Bilingual Technical Index**: High-signal English & Chinese corpus (Rust, Go, Python, AI/ML, Official Docs, RFCs, GitHub, arXiv).
- 🕒 **Explicit `fetched_at` Timestamp**: Every ranked hit reveals the exact second it was ingested—enabling grounded citations and temporal reasoning.
- 🎯 **Predictable Ledger Billing**: Exactly **1 credit per successful 2xx response**. Errors, timeouts (504), rate limits (429), and bad requests cost **0 credits**.
- 🧩 **Native Model Context Protocol (MCP)**: Zero setup across Claude Code, Cursor, Windsurf, Cline, Zed, and Claude Desktop.
- 🚀 **1,000 Free Credits Every Month**: Sign in with GitHub or Google at [annolux.com](https://annolux.com) and start querying in 30 seconds.

---

## 🥊 Comparison: Annolux vs. Generic Search APIs

| Feature / Metric | **Annolux** | **Exa (Metaphor)** | **Tavily** | **Serper / Google** |
| :--- | :--- | :--- | :--- | :--- |
| **Index Quality** | **Curated Tech & Knowledge (EN/ZH)** | Web-wide neural | Web-wide aggregator | Entire Web (noisy SEO) |
| **Chinese (ZH) Tech Corpus** | **First-class native bilingual FTS** | Moderate | Weak / Translated | Mixed with content farms |
| **Explicit Snapshot Timestamp** | **✅ `fetched_at` on every result** | ❌ Inconsistent | ❌ Omitted | ❌ Snippet approximate only |
| **Billing Guarantee** | **✅ 1 credit only on 2xx success** | Request-based | Request-based | Request-based |
| **Failed / Timeout Queries** | **🆓 0 Credits charged** | ❌ Billed | ❌ Billed | ❌ Billed |
| **MCP Tool Surface** | **Single lean `search_web` (Minimal token waste)** | Multiple bulky tools | Multi-step tools | Needs custom bridge |
| **Domain Restriction** | **✅ Exact hostname filtering (`domains`)** | ✅ Supported | ✅ Supported | Limited `site:` query |
| **Free Starter Tier** | **1,000 credits / month** | Limited trial | 1,000 / mo | 2,500 one-time |

---

## 📦 Quick Installation

### Option 1: NPX (Fastest for MCP & CLI)
```bash
# Run instantly via Node.js (zero installation)
npx -y annolux-mcp -key ann_live_YOUR_API_KEY
```

### Option 2: Go CLI & Server
```bash
go install github.com/eason4kim-rocket/annolux/cmd/annolux-mcp@latest
```

### Option 3: Pre-built Multi-Platform Binaries
Download standalone binaries from [GitHub Releases](https://github.com/eason4kim-rocket/annolux/releases):
- `linux-amd64` / `linux-arm64`
- `darwin-amd64` (Intel Mac) / `darwin-arm64` (Apple Silicon M-series)

---

## 🔌 MCP Integration

Annolux implements the official [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) specification with a single, high-efficiency tool: `search_web`.

### ⚡ 1-Click Installation (Cursor & Smithery)

- **Cursor**: Click [![Add to Cursor](https://img.shields.io/badge/Add%20to-Cursor-000000?style=flat-square&logo=cursor&logoColor=white)](cursor://anysphere.cursor-deeplink/mcp/install?name=annolux&config=eyJjb21tYW5kIjoibnB4IiwiYXJncyI6WyIteSIsImFubm9sdXgtbWNwIl19) to install natively via deep link.
- **Smithery CLI**:
  ```bash
  npx -y @smithery/cli install annolux-mcp --client claude
  npx -y @smithery/cli install annolux-mcp --client cursor
  ```
- **Glama Online Playground**: Test queries instantly without local setup on [Glama.ai](https://glama.ai/mcp/servers/eason4kim-rocket/annolux).

### 1. Claude Code
```bash
claude mcp add annolux npx -y annolux-mcp -- -key ann_live_YOUR_API_KEY
```

### 2. Cursor / Windsurf
Add to your project `.cursor/mcp.json` or global configuration:
```json
{
  "mcpServers": {
    "annolux": {
      "command": "npx",
      "args": ["-y", "annolux-mcp", "-key", "ann_live_YOUR_API_KEY"]
    }
  }
}
```

### 3. Claude Desktop
Add to `claude_desktop_config.json`:
```json
{
  "mcpServers": {
    "annolux": {
      "command": "annolux-mcp",
      "env": {
        "ANNOLUX_API_URL": "https://api.annolux.com",
        "ANNOLUX_API_KEY": "ann_live_YOUR_API_KEY"
      }
    }
  }
}
```

---

## 🚀 HTTP API Quickstart

### Standard Search Endpoint
```http
POST https://api.annolux.com/api/v1/search
Authorization: Bearer ann_live_YOUR_API_KEY
Content-Type: application/json
```

```json
{
  "query": "tokio async runtime memory model",
  "domains": ["tokio.rs", "docs.rs", "github.com"],
  "deduplicate": true,
  "limit": 5,
  "timeout": 10,
  "ranking": "default"
}
```

### Python
```python
import os
import requests

response = requests.post(
    "https://api.annolux.com/api/v1/search",
    headers={"Authorization": f"Bearer {os.environ.get('ANNOLUX_API_KEY')}"},
    json={
        "query": "DeepSeek R1 architecture reinforcement learning",
        "limit": 5,
        "deduplicate": True
    },
    timeout=15
)

data = response.json()
for result in data.get("results", []):
    print(f"[{result['fetched_at']}] {result['title']} -> {result['url']}")
```

### TypeScript / Node.js
```typescript
const res = await fetch("https://api.annolux.com/api/v1/search", {
  method: "POST",
  headers: {
    "Authorization": `Bearer ${process.env.ANNOLUX_API_KEY}`,
    "Content-Type": "application/json"
  },
  body: JSON.stringify({
    query: "vLLM PagedAttention implementation details",
    limit: 5,
    deduplicate: true
  })
});

const data = await res.json();
console.log(`Credits Remaining: ${res.headers.get("X-Annolux-Credits-Remaining")}`);
console.log(data.results);
```

### cURL
```bash
curl -s -X POST https://api.annolux.com/api/v1/search \
  -H "Authorization: Bearer ann_live_YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Go sync.Pool benchmark best practices",
    "limit": 3
  }' | jq .
```

---

## 🏛️ Architecture & Mechanics

```
┌─────────────────────────────────────────────────────────────┐
│                 AI Agent / RAG Application                  │
│       (Claude Code / Cursor / LangChain / Custom LLM)       │
└──────────────────────────────┬──────────────────────────────┘
                               │
               Stdio MCP / HTTPS REST Request
                               │
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                  Annolux Gateway API Engine                 │
│  ┌─────────────────────────┐     ┌───────────────────────┐  │
│  │ 1. Account & Rate Limit │ ──► │ Reserve 1 Credit      │  │
│  │    (5 RPS, Burst 10)    │     │ in /data/accounts.db  │  │
│  └─────────────────────────┘     └───────────────────────┘  │
│                                              │              │
│                                              ▼              │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 2. Bilingual FTS Ranker (/data/index.db)              │  │
│  │    • Curated English & Chinese Corpus                 │  │
│  │    • SimHash Content-Deduplication Engine             │  │
│  │    • Domain Filter & Exact Substring Match            │  │
│  └───────────────────────────────────────────────────────┘  │
│                                              │              │
│                                              ▼              │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 3. Atomic Response & Ledger Settlement                │  │
│  │    • 2xx Success ──► Commit 1 Credit & Attach Timing  │  │
│  │    • 4xx/5xx Err ──► Release Reservation (0 Cost)     │  │
│  └───────────────────────────────────────────────────────┘  │
└──────────────────────────────┬──────────────────────────────┘
                               │
          JSON with exact `fetched_at` & verified URL
                               │
                               ▼
                     [ Grounded LLM Response ]
```

---

## 📊 Search Quality & Frozen Benchmarks

Annolux evaluates search retrieval performance against an immutable, frozen blind set of 40 complex bilingual queries. The ranking weights are never tuned on the test set.

| Metric | First Gate Baseline | Prelaunch Verification Gate |
| :--- | :---:| :---:|
| **Hit@1** | `72.5%` | **`72.5%`** |
| **Hit@3** | `82.5%` | **`82.5%`** |
| **Hit@10** | `85.0%` | **`85.0%`** |
| **MRR@10** | `0.78125` | **`0.78125`** |
| **P95 Latency** | `532 ms` | **`356 ms`** |
| **5xx Error Rate** | `0.00%` | **`0.00%`** |

*All benchmarks are evaluated client-side under full concurrency load.*

---

## 💳 Transparent Pricing

| Plan | Price | Credits | Rate Limits | Billing Rules |
| :--- | :--- | :--- | :--- | :--- |
| **Free** | **$0** | **1,000 / month** | 5 RPS / Burst 10 | Free forever, no credit card required |
| **Pro** | **$29 / mo** | **20,000 / mo** | 5 RPS / Burst 10 | 1 success = 1 credit, no rollover |
| **Scale** | **$99 / mo** | **100,000 / mo** | 5 RPS / Burst 10 | 1 success = 1 credit, no rollover |

- No overage charges.
- Errors, rate-limits, and timeouts are 100% free (0 credit charged).
- Up to 3 active API keys per account.

---

## 📁 Examples & Recipes

Check the [`examples/`](examples/) directory for production-ready starters:
- [`01-claude-code-literature-research`](examples/01-claude-code-literature-research/): Automated technical survey agent with timestamped citations.
- [`02-cursor-authority-domain-refactor`](examples/02-cursor-authority-domain-refactor/): Restrict search to official doc domains (`react.dev`, `go.dev`) for zero-hallucination refactoring.
- [`03-production-rag-temporal-pipeline`](examples/03-production-rag-temporal-pipeline/): Production RAG hybrid search pipeline with fallback retrieval.
- [`04-n8n-ai-research-agent`](examples/04-n8n-ai-research-agent/): Ready-to-import n8n AI Agent workflow with community node (`n8n-nodes-annolux`) and temporal citations.

---

## 🤝 Community & Support

- File bug reports or feature requests on [GitHub Issues](https://github.com/eason4kim-rocket/annolux/issues).
- Review [SECURITY.md](SECURITY.md) for private vulnerability reporting.
- Public OpenAPI specification: [annolux.com/openapi.json](https://annolux.com/openapi.json).

---

## 📄 License

Annolux is open-source software licensed under the [Apache License, Version 2.0](LICENSE).
