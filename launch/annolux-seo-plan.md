# Annolux SEO 总方案：三块真数据的打法

配套阅读：`gefei-seo-annolux.md`（意图页与 30 天节奏，本文不取代它，是它的数据侧展开）。
核对日：2026-08-19。约束不变：不写 realtime / complete web / best search / verified facts / SLA；对外声明以 `fact-pack.md` 为准。

## 状态日志（最新在上）

- **2026-08-20：批次 1 已上线。** 提交 `6d2928e`，8 个 URL 全部 200：
  `/mcp`、`/mcp/claude-code`、`/mcp/claude-desktop`、`/mcp/cursor` 及 `/zh/` 对等页。
  质量门实测：字数 415/320/311/333（≥300）；任两页独特内容 77–95%（≥35%）；
  响应示例与 OpenAPI 一字不差；`go install` 路径实测可构建。
  IndexNow 已推送全 sitemap。用户已在 GSC 请求收录 `/mcp`（配额有限只提交 hub）。
- **2026-08-20：GSC 验证完成**（DNS TXT），sitemap 已提交；Bing 经 GSC 导入。
- **2026-08-19：favicon 修复上线**（提交 `5061664`）：位图 ico/png 替代纯 SVG，
  nginx 假 `/favicon.ico` 映射已删。等 Google 重抓后 SERP 图标恢复。
- **待办**：`/docs` 加一条指向 `/mcp` 的内链（动现有文案，需用户过目）；
  批次 2 等 GSC 出现 `/mcp` 组曝光信号（预计 2–4 周）。

---

## 0. 一句话决策

**两块现在做，一块等数据。**

| 数据源 | 决策 | 理由 |
|---|---|---|
| ① MCP 客户端矩阵 | **现在做**（第一批 4 页） | 数据已存在（各客户端配置真实不同），词区新、KD 低，gefei 文档已判为第一优先 |
| ② 框架集成矩阵 | **现在准备，批次 3 上线** | 数据已存在（每框架一段可运行代码），但要等批次 1 验证模板与收录 |
| ③ 查询日志驱动页 | **等数据**（≥500 条去重真实查询） | querylog 已在收集；没有真实查询前做页面是在猜需求 |

程序化 SEO 的公式：`价值 = 真实数据行数 × 每行独特信息量`。
Annolux 的真实数据只有几十行，所以目标是 **30–60 页精确意图页**，不是 Zapier 式几千页。
2025 年 Google 对 scaled content abuse 大规模手动处罚后，模板换词页是负资产；对信任型 API 品牌，一次处罚是灾难。

---

## 1. 前置条件（不满足则 SEO 全部白做）

- [x] **GSC + Bing 验证、sitemap 提交**（2026-08-20 完成：GSC DNS TXT 验证 + sitemap 提交，Bing 经 GSC 导入）
- [x] 公网已是新版产品站（favicon 位图已修，等 Google 重抓）
- [x] robots 策略保持：放行 Googlebot/Bingbot/OAI-SearchBot，拦 GPTBot/Google-Extended/CCBot
- [x] `ops/indexnow-submit.sh` 已就绪，新页上线即提交
- [ ] 每个新意图页中英成对上线（hreflang 不缺半边）

---

## 2. 数据源 ①：MCP 客户端矩阵（= 我们的 "X integrations"）

**为什么是真数据**：每个 MCP 客户端的配置文件格式、路径、添加命令、坑都真实不同。页面差异来自数据本身，不是换词。

### 页面清单（按词区优先级）

| URL | 目标查询（en） | 批次 |
|---|---|---|
| `/mcp` | mcp web search, search mcp server | 1（hub） |
| `/mcp/claude-code` | claude code mcp web search | 1 |
| `/mcp/claude-desktop` | claude desktop mcp search | 1 |
| `/mcp/cursor` | cursor mcp web search | 1 |
| `/mcp/windsurf` | windsurf mcp search | 2 |
| `/mcp/cline` | cline web search mcp | 2 |
| `/mcp/continue` | continue dev mcp search | 2 |
| `/mcp/zed` | zed mcp web search | 2 |
| `/mcp/vscode` | vscode copilot mcp search | 2 |

中文对等：`/zh/mcp/*`，目标词「Claude MCP 联网搜索」「Cursor MCP 搜索」等。
批次 1 共 **8 页**（4 × en/zh）；批次 2 再加 ~10 页。

### 每页必备的独特内容（这是质量门，不是建议）

1. 该客户端**可复制**的配置 JSON（含 mac/win 路径差异）
2. 添加命令或操作步骤（该客户端特有）
3. 一段**真实** `search_web` 响应，含 `fetched_at`
4. 该客户端特有的坑（如配置文件热加载行为、env 传递方式）
5. 边界声明：精选中英索引、非实时全网
6. 内链：hub ↔ 客户端页 ↔ `/docs`、`/pricing`、语言对页

---

## 3. 数据源 ②：框架集成矩阵

**为什么是真数据**：每个框架的 tool/retriever 接口真实不同，每页核心是一段调 Annolux API 的**可运行代码**。

### 页面清单

| URL | 框架 | 目标查询 | 批次 |
|---|---|---|---|
| `/integrations/langchain` | LangChain | langchain web search tool | 3 |
| `/integrations/llamaindex` | LlamaIndex | llamaindex search retriever | 3 |
| `/integrations/openai-agents` | OpenAI Agents SDK | openai agents web search | 3 |
| `/integrations/vercel-ai-sdk` | Vercel AI SDK | ai sdk search tool | 3 |
| `/integrations/crewai` | CrewAI | crewai search tool | 4 |
| `/integrations/autogen` | AutoGen | autogen web search | 4 |
| `/integrations/haystack` | Haystack | haystack websearch | 4 |
| `/integrations/curl` | 纯 HTTP | search api curl example | 4 |

中文对等成对上线。批次 3+4 共 ~30 页。

**护城河动作**：把每页的示例代码放进 CI 真跑（调真 API、校验响应结构）。页面上的代码**永远可运行**——这是任何竞品页农场做不到的差异，也是最强的 E-E-A-T 信号。

---

## 4. 数据源 ③：查询日志驱动页（等数据）

**触发条件**（与索引扩容纪律同源，全部满足才开工）：

- ≥ 500 条去重真实查询（querylog 已上线在收集）
- 人工归类出重复出现的意图簇（每簇 ≥ 10 条查询）
- 该意图现有页面覆盖不了

**产出形态**：不是博客，是意图页。真实查询告诉你用户用什么词、要什么结果形态——那就是页面的 H1 和内容结构。这是最正统的 pSEO 数据源，急不来。

---

## 5. 实现：sitegen 加一个数据文件

```
website/integrations.json    数据源（每行一个客户端/框架）：
  slug, name, kind(mcp|framework), config_json, paths{mac,win},
  steps[], caveats[], verify_cmd, en{title,desc,intro}, zh{...}

tools/sitegen                新模板 integration.tmpl：
  - 自引用 canonical + hreflang 成对
  - BreadcrumbList JSON-LD（从 URL 层级生成）
  - HowTo/FAQPage schema（步骤真实存在才输出）
  - sitemap lastmod = integrations.json 中该行的 updated 字段
  - hub↔spoke 互链 + 相邻 3–5 条横链（同 kind）
```

### 质量门（写进生成器，硬性拦截）

| 规则 | 阈值 | 违反动作 |
|---|---|---|
| 每页正文 | ≥ 300 词 | 拒绝生成 |
| 模板共享文案占比 | < 60% | 拒绝生成 |
| 任两页独特内容 | ≥ 35% | 拒绝生成 |
| 配置/代码真实性 | CI 实测通过 | 拒绝发布 |
| 单批发布量 | ≤ 20 页 | 分批 |
| 单独存在价值测试 | 人工过一遍 | 每批 100% 审（量小审得起） |

---

## 6. 批次与节奏（贴 gefei 30 天计划，不另起炉灶）

| 批次 | 内容 | 页数 | 触发条件 |
|---|---|---|---|
| 1 ✅ | `/mcp` hub + claude-code + claude-desktop + cursor（×zh） | 8 | **已上线 2026-08-20（`6d2928e`）** |
| 2 | 其余 MCP 客户端（×zh） | ~10 | 批次 1 有页面进 GSC 曝光报告（约 2–4 周） |
| 3 | 框架集成前 4 个（×zh） | ~8 | 批次 2 收录率 ≥ 80% |
| 4 | 其余框架（×zh） | ~8 | 批次 3 有曝光 |
| 5 | querylog 驱动页 | 按需 | ≥500 条去重查询 + 意图簇成立 |

每批之间看数据再动，**永远不要一次发 50+ 页**。

---

## 7. 度量与决策规则（GSC 为准）

| 信号 | 判读 | 动作 |
|---|---|---|
| 有曝光无点击 | Title/Description 不勾人 | 改 TD，别改内容 |
| 有点击高跳出 | 页面没接住意图 | 首屏加可复制配置/代码 |
| 收录率 < 60%（4 周后） | 内容被判 thin | 停止扩批，先做内容加厚 |
| 某客户端页起量 | 词区验证成功 | 优先补该客户端的中文页和横链 |
| 品牌词曝光上升 | 外链/社区动作生效 | 保持，别加速 |

转化目标沿用 gefei 文档：**注册 + 第一次 Search**（Paddle 未开，不定付费目标）。

---

## 8. 不做清单（红线）

- ❌ 用索引内容生成页面（转发他人内容 + 暴露索引构成）
- ❌ 城市/行业换词页、上千 glossary、干砌 "X alternative" 页群
- ❌ 第一批做 vs Tavily 大词对比页（gefei 文档已判暂缓）
- ❌ AI 批量生成不经人审的内容
- ❌ 宣传能搜到围墙平台内容（中立地位不可自毁）
- ❌ 一次发布 50+ 页

---

## 9. 分工

| 事项 | 谁做 |
|---|---|
| GSC / Bing 验证 | **站主**（唯一阻塞项） |
| integrations.json + 模板 + 质量门 + 批次 1 页面 | 工程侧，可立即开工 |
| 每批人工审读 | 站主（每批 ≤ 20 页，半小时） |
| 外链（Show HN / V2EX / awesome-mcp PR） | 站主，按 gefei 文档第 8 节 |
| querylog 意图归类 | 有 500 条后一起做 |
