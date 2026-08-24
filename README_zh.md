<div align="center">

# ⚡ Annolux (中文文档)

**面向 AI Agent 与 RAG 系统的中英双语精选搜索 API 与 MCP 服务**

*每一次检索都有据可查。每条结果均携带明确的 `fetched_at` 抓取时间戳与来源归属。*

[![Go Version](https://img.shields.io/github/go-mod/go-version/eason4kim-rocket/annolux?style=flat-square&logo=go)](https://golang.org)
[![NPM Version](https://img.shields.io/npm/v/annolux-mcp?style=flat-square&logo=npm&color=CB3837)](https://www.npmjs.com/package/annolux-mcp)
[![Smithery Badge](https://smithery.ai/badge/@eason4kim/annolux)](https://smithery.ai/server/@eason4kim/annolux)
[![Glama MCP](https://glama.ai/mcp/servers/eason4kim-rocket/annolux/badge)](https://glama.ai/mcp/servers/eason4kim-rocket/annolux)
[![Add to Cursor](https://img.shields.io/badge/一键安装到-Cursor-000000?style=flat-square&logo=cursor&logoColor=white)](cursor://anysphere.cursor-deeplink/mcp/install?name=annolux&config=eyJjb21tYW5kIjoibnB4IiwiYXJncyI6WyIteSIsImFubm9sdXgtbWNwIl19)
[![MCP Protocol](https://img.shields.io/badge/MCP-2025--12--11-000000?style=flat-square&logo=anthropic)](https://modelcontextprotocol.io/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg?style=flat-square)](LICENSE)
[![Free Tier](https://img.shields.io/badge/免费额度-1%2C000_点数-brightgreen?style=flat-square)](https://annolux.com/zh/)

[🌐 官方网站](https://annolux.com/zh/) • [📖 API 文档](https://annolux.com/zh/docs) • [⚡ MCP 快速上手](#-mcp-客户端集成) • [📊 冻结基准测试](#-搜索质量与冻结盲测基准) • [📁 实战案例](examples/) • [English README](README.md)

</div>

---

## 💡 为什么选择 Annolux？

当前面向 AI Agent 的通用网页搜索 API 存在三大致命痛点：
1. **垃圾数据污染上下文**：商业搜索引擎索引了数以亿计的 SEO 内容农场、采集站和机翻垃圾，严重污染大模型的上下文窗口。
2. **缺失时效性证据**：传统 API 不提供或模糊处理快照抓取时间（`fetched_at`），导致大模型引用过时代码或产生时间幻觉。
3. **恶意与模糊计费**：请求失败、返回空结果或超时时依然被扣费，甚至产生高昂的超额账单。

**Annolux 专为 AI Agent 设计，提供全新的精选检索体验：**
- 🛡️ **33.7 万+ 中英双语技术精选索引**：高质量技术语料（Rust、Go、Python、AI/ML、官方文档、RFC、GitHub、arXiv），杜绝采集站。
- 🕒 **显式 `fetched_at` 抓取时间戳**：每条命中结果均明确标注具体抓取秒级时间，为大模型提供确定性的时间证据链。
- 🎯 **确定性账本计费**：**仅成功的 2xx 请求扣 1 点**。参数错误、频率超限 (429)、服务端超时 (504) **100% 扣 0 点**。
- 🧩 **原生 Model Context Protocol (MCP)**：无缝支持 Claude Code、Cursor、Windsurf、Cline、Zed 与 Claude Desktop。
- 🚀 **每月 1,000 次免费额度**：支持 GitHub / Google 快捷登录，30 秒跑通首次调用。

---

## 🥊 核心特性对比表 (Feature Matrix)

| 特性 / 维度 | **Annolux** | **Exa (Metaphor)** | **Tavily** | **Serper / Google** |
| :--- | :--- | :--- | :--- | :--- |
| **索引定位** | **中英双语技术与知识精选** | 全网神经检索 | 全网聚合搜索 | 全网大爬虫 (含大量 SEO 农场) |
| **中文技术语料** | **原生中文 FTS，针对中文文档优化** | 一般 | 较弱 / 依赖翻译 | 参杂大量采集站垃圾内容 |
| **快照抓取时间戳** | **✅ 每条结果必带 `fetched_at`** | ❌ 缺失或不固定 | ❌ 忽略 | ❌ 仅有粗略估算日期 |
| **计费确定性保证** | **✅ 仅 2xx 成功请求扣 1 点** | 请求即扣费 | 请求即扣费 | 请求即扣费 |
| **超时/报错请求** | **🆓 0 点数扣费，绝无超额账单** | ❌ 依然计费 | ❌ 依然计费 | ❌ 依然计费 |
| **MCP 工具面** | **单工具 `search_web`（最省 Token）** | 多工具较重 | 多工具较重 | 需额外写 Bridge |
| **域名限定检索** | **✅ 支持精确主机名过滤 (`domains`)** | ✅ 支持 | ✅ 支持 | 受限的 `site:` 语法 |
| **免费起步额度** | **每月 1,000 次额度（免绑信用卡）** | 试用限制 | 1,000次/月 | 2,500次一次性 |

---

## 📦 快速安装与运行

### 方式 1: NPX (极速免安装运行 MCP)
```bash
# 无需安装 Go 或克隆代码，Node.js 18+ 直接一行运行
npx -y annolux-mcp -key ann_live_你的APIKey
```

### 方式 2: Go 安装
```bash
go install github.com/eason4kim-rocket/annolux/cmd/annolux-mcp@latest
```

### 方式 3: 预编译跨平台二进制
前往 [GitHub Releases](https://github.com/eason4kim-rocket/annolux/releases) 下载对应平台的独立二进制文件：
- `linux-amd64` / `linux-arm64`
- `darwin-amd64` (Intel Mac) / `darwin-arm64` (Apple Silicon M 系列芯片)

---

## 🔌 MCP 客户端集成

Annolux 严格遵循 Anthropic [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) 规范，提供轻量高效的单工具：`search_web`。

### ⚡ 1 秒极速一键安装（Cursor / Smithery / Glama）

- **Cursor 原生安装**：点击 [![一键安装到 Cursor](https://img.shields.io/badge/一键安装到-Cursor-000000?style=flat-square&logo=cursor&logoColor=white)](cursor://anysphere.cursor-deeplink/mcp/install?name=annolux&config=eyJjb21tYW5kIjoibnB4IiwiYXJncyI6WyIteSIsImFubm9sdXgtbWNwIl19) 直接在 Cursor 客户端中完成配置。
- **Smithery CLI 自动安装**：
  ```bash
  npx -y @smithery/cli install annolux-mcp --client claude
  npx -y @smithery/cli install annolux-mcp --client cursor
  ```
- **Glama 免安装网页沙盒**：在 [Glama.ai 在线 Playground](https://glama.ai/mcp/servers/eason4kim-rocket/annolux) 体验免本地环境搜索。

### 1. Claude Code
```bash
claude mcp add annolux npx -y annolux-mcp -- -key ann_live_你的APIKey
```

### 2. Cursor / Windsurf
在项目 `.cursor/mcp.json` 或全局配置中添加：
```json
{
  "mcpServers": {
    "annolux": {
      "command": "npx",
      "args": ["-y", "annolux-mcp", "-key", "ann_live_你的APIKey"]
    }
  }
}
```

### 3. Claude Desktop
在 `claude_desktop_config.json` 中配置：
```json
{
  "mcpServers": {
    "annolux": {
      "command": "annolux-mcp",
      "env": {
        "ANNOLUX_API_URL": "https://api.annolux.com",
        "ANNOLUX_API_KEY": "ann_live_你的APIKey"
      }
    }
  }
}
```

---

## 🚀 HTTP REST API 快速调用

### 标准搜索接口
```http
POST https://api.annolux.com/api/v1/search
Authorization: Bearer ann_live_你的APIKey
Content-Type: application/json
```

```json
{
  "query": "tokio 异步运行时 内存模型",
  "domains": ["tokio.rs", "docs.rs", "github.com"],
  "deduplicate": true,
  "limit": 5,
  "timeout": 10,
  "ranking": "default"
}
```

### Python 代码调用
```python
import os
import requests

response = requests.post(
    "https://api.annolux.com/api/v1/search",
    headers={"Authorization": f"Bearer {os.environ.get('ANNOLUX_API_KEY')}"},
    json={
        "query": "DeepSeek R1 强化学习 架构",
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
    query: "vLLM PagedAttention 核心实现",
    limit: 5,
    deduplicate: true
  })
});

const data = await res.json();
console.log(`剩余点数: ${res.headers.get("X-Annolux-Credits-Remaining")}`);
console.log(data.results);
```

---

## 📊 搜索质量与冻结盲测基准

Annolux 检索性能基于一组不可变更的 **40 条复杂中英双语盲测查询集（Frozen Blind Set）** 进行评估。排序权重绝不对测试集进行微调：

| 评估指标 | 首发发布门禁 (First Gate) | 当前预发布验证 (Prelaunch) |
| :--- | :---:| :---:|
| **Hit@1** | `72.5%` | **`72.5%`** |
| **Hit@3** | `82.5%` | **`82.5%`** |
| **Hit@10** | `85.0%` | **`85.0%`** |
| **MRR@10** | `0.78125` | **`0.78125`** |
| **P95 延迟** | `532 ms` | **`356 ms`** |
| **5xx 服务端错误率** | `0.00%` | **`0.00%`** |

---

## 💳 透明价格体系

| 套餐方案 | 价格 | 每月点数 | 速率限制 | 计费规则 |
| :--- | :--- | :--- | :--- | :--- |
| **Free (免费版)** | **$0** | **1,000 / 月** | 5 RPS / 突发 10 | 永久免费，免绑信用卡 |
| **Pro (专业版)** | **$29 / 月** | **20,000 / 月** | 5 RPS / 突发 10 | 1次成功请求=1点，周期清零不结转 |
| **Scale (规模版)** | **$99 / 月** | **100,000 / 月** | 5 RPS / 突发 10 | 1次成功请求=1点，周期清零不结转 |

- 无任何超额隐形账单。
- 报错、超频、超时请求 **100% 免计费（扣 0 点）**。
- 每个账户最多可创建 3 个活跃 API Key。

---

## 📁 实战案例库 (Examples)

前往 [`examples/`](examples/) 目录查看开箱即用的实战源码：
- [`01-claude-code-literature-research`](examples/01-claude-code-literature-research/): 自动化技术文献调研 Agent，带显式时间戳与引用来源。
- [`02-cursor-authority-domain-refactor`](examples/02-cursor-authority-domain-refactor/): 限定官方权威文档域名（`react.dev`、`go.dev`）的零幻觉重构助手。
- [`03-production-rag-temporal-pipeline`](examples/03-production-rag-temporal-pipeline/): 生产级 RAG 混合检索管道，带时效性降级策略。
- [`04-n8n-ai-research-agent`](examples/04-n8n-ai-research-agent/): 开箱即用的 n8n 官方社区节点 (`n8n-nodes-annolux`) 与 AI Agent 深度研究工作流蓝图。

---

## 📄 开源许可证

Annolux 遵循 [Apache License 2.0](LICENSE) 开源许可证。
