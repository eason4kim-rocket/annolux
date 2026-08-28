# AlternativeTo (alternativeto.net) 提交指南与完整填表蓝图

**目标平台**：[AlternativeTo](https://alternativeto.net)  
**平台权威度**：Domain Rating 82（Google 搜索 `[Product] alternative` 长期霸榜前 3 名）  
**提交入口**：[https://alternativeto.net/software/create/](https://alternativeto.net/software/create/)

---

## 📋 提交表单字段一览（复制即填）

### 1. Basic Information（基本信息）
- **Application Name**: `Annolux`
- **Website URL**: `https://annolux.com`
- **Icon / Logo URL**: `https://raw.githubusercontent.com/eason4kim-rocket/annolux/main/website/logo-400.png`
- **License / Pricing Model**: `Freemium` (Free 1,000 queries/month, Pro starts at $29/mo)
- **Platforms**:
  - [x] Web / Online
  - [x] API
  - [x] Self-Hosted
  - [x] Mac
  - [x] Windows
  - [x] Linux

---

### 2. Short Description（一句话简介）
```text
Curated English & Chinese Web Search API and Model Context Protocol (MCP) server for AI Agents and RAG with explicit snapshot timestamps.
```

---

### 3. Long Description（详细介绍）
```markdown
Annolux is an agent-first web search API and Model Context Protocol (MCP) server engineered for LLM agents, RAG systems, and AI coding assistants (such as Claude Code, Cursor, Cline, and Windsurf).

### Key Features:
- 🕒 **Explicit `fetched_at` Snapshot Timestamps**: Every returned snippet includes an immutable RFC3339 timestamp, allowing LLMs to ground knowledge temporally and prevent hallucinations of deprecated APIs or outdated facts.
- 🛡️ **Curated Bilingual Index**: High-signal English and Chinese technical web corpus stripped of SEO clickbait, content farms, and noisy scrapers.
- 🎯 **Fair Pay-on-Success Billing**: Exactly 1 credit is deducted only on successful HTTP 200 responses. Timeouts (504), errors (4xx/5xx), and empty results cost 0 credits.
- 🧩 **Zero-Friction MCP Integration**: Run `npx -y annolux-mcp` or install via standard MCP registries (Smithery, Glama, Cline) with a single lean `search_web` tool surface.
- 🆓 **1,000 Free Monthly Requests**: Permanent recurring free credit grant for developers with no credit card required.
```

---

### 4. Alternatives to Suggest（最核心的替代竞品关联）
在 **"Suggest as alternative to"** 搜索并勾选以下产品（这是拦截精准搜索流量的关键）：
1. **Tavily** *(核心竞品)*
2. **Exa (formerly Metaphor)** *(AI 语义搜索)*
3. **Google Custom Search Engine** *(传统自定义搜索)*
4. **SerpAPI** *(传统 SERP 抓取)*
5. **Bing Web Search API** *(搜索引擎 API)*
6. **Brave Search API** *(搜索 API)*

---

### 5. Tags / Keywords（标签）
`search-api`, `ai-agent`, `rag`, `model-context-protocol`, `mcp`, `developer-tools`, `large-language-models`, `web-search`, `bilingual-search`
