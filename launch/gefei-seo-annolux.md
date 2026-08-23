# 按哥飞方法做 Annolux SEO

配套阅读：`gefei-seo-playbook.md`。  
产品依据：`README.md`、`launch/fact-pack.md`、`LAUNCH-STATUS.md`、本地 `website/dist/`。  
核对日：2026-08-16。

本文只给建议，不授权改文案、发帖、买外链或改 robots。对外声明仍以 `fact-pack.md` 为准。

---

## 1. 产品先说清楚

Annolux 不是通用搜索引擎，也不是 Firecrawl / Tavily 那种「全网实时搜索 + 抓页」。

它是：

- 面向 **Agent / RAG** 的中英双语 **Search API**
- 一个商业接口：`POST /api/v1/search`
- 一个 MCP 工具：`search_web`
- 自有精选索引（约 25.1 万页：英 21.5 万 / 中 3.6 万），定期刷新，**不声称实时、不全网**
- 每条结果带 title、URL、snippet、`fetched_at`
- 成功 2xx 扣 1 点，错误和超时不扣
- Free 1,000 点永久；Pro $29 / 2 万点；Scale $99 / 10 万点
- 所有套餐 5 RPS，Scale 只改单价，没有更高限流、没有 SLA
- Verify / receipts 是 Labs，不是付费权益

变现路径是 **用户订阅**，不是 Adsense。哥飞口径里，工具站付费往往是广告的 3–5 倍，这条路和他教的一致。

约束（做 SEO 时不能踩）：

- 不能写 realtime / complete web / best search / verified facts / SLA
- Paddle Live 还冻着，付费结账不能当已上线卖
- 线上 [annolux.com](https://annolux.com) **还没切到这版产品站**。现在公网仍是旧叙事（Fetch / Verify / receipts / 去旧产品仓库要 beta key）。本地新站已就绪，客户流量未切。

---

## 2. 用哥飞三句话诊断

| 哥飞原话 | Annolux 现状 | 判断 |
|---|---|---|
| 分门别类罗列 | 本地站页面不少（docs / pricing / benchmarks / 4 篇笔记 / 法律页），但按**用户搜索意图**分组几乎没做 | 结构按产品信息架构搭的，不是按词表搭的 |
| 举全站之力 | 品牌域 `annolux.com` 是新词，整站本应合力打 1–2 个能做的需求 | 首页在讲哲学，内页在讲契约，没有一个词被全站围殴 |
| 一个关键词一个页面 | docs 想同时覆盖 auth / 请求 / 错误 / credits / MCP | 几个不同意图挤在一页；MCP 只是 blog，不是产品页 |

时间分配更刺眼：工程和发布门禁极完整，增长侧还停在「等 10–20 个 beta」。这就是他批评的 **拍脑袋写代码 80%，挖需求和推广不够**。按他的 4:2:4，下一阶段主业不是再加功能，是挖词、做意图页、发外链。

---

## 3. 先不要打的词

这些 SERP 前十基本是竞品**首页**，新站不要当第一批目标：

- `search api` / `web search api`
- `tavily` / `serper` / `brave search api`
- `web search for ai agents`（综述文 + 大厂）
- `best search api`（违规宣称，且全是榜单站）

品牌词 `annolux` 是新词：收录后几乎稳排第一，但没人搜。只当「被搜到能找到官网」的保底，不当流量来源。

---

## 4. 建议词表（先收，再量化）

下面是按公开搜索行为和产品契约收的**候选组**，还没跑 Ahrefs / Ads / intitle。上站前应用 `gefei-seo-playbook.md` 第 4.2 节补搜索量、KD、KGR。只做 KGR 低、SERP 里多为内页或弱页、且你能诚实满足的组。

| 组 | 用户在找什么 | 建议 URL | 页面类型 | 第一批？ | 备注 |
|---|---|---|---|---|---|
| 品牌 | 已经知道 Annolux | `/` | 首页 | 是 | 解释总需求，往下导，不要当诗 |
| Agent Search API | 给 agent 一个可调用的搜索接口 | `/` 兼导，或 `/search-api` | 首页 / 产品页 | 是 | 主需求。Title 必须含这个意图，不能只写品牌金句 |
| MCP `search_web` | Claude / Cursor 要一个 search MCP | `/mcp` | 工具页 | **第一优先** | 需求新、页面少、你有真功能。现有 blog 不能代替 |
| Claude MCP web search | 具体客户端怎么接 | `/mcp/claude` | 工具页 | 是 | 打开就能复制配置；不登录可看 |
| Cursor / Codex MCP search | 同上，换客户端 | `/mcp/cursor` | 工具页 | 第二批 | 和 Claude 需求不同，可拆页 |
| bilingual / Chinese English search API | 要中英同一个索引 | `/bilingual-search` | 产品页 | 是 | 真差异。竞品很少认真做中文索引 |
| search api `fetched_at` / fetch time | 要知道结果是什么时候抓的 | `/docs` 一节不够，独立讲 | 产品页 | 可选 | 长尾、低量、极贴契约 |
| RAG search api | 给 RAG 找带 URL 的片段 | 并入 Agent Search API 页 | 同页覆盖 | 是 | 同义/相邻意图，不要拆薄页 |
| Annolux vs Tavily 等 | 选型 | `/compare/tavily` | 对比页 | 暂缓 | 必须诚实写「我们不是实时全网」。第一批不做，避免流量不匹配 |
| self-hosted search api | 要自己部署 | 不做 | — | 不做 | 公开产品是托管 Search，不要骗这个意图 |
| scrape / crawl / firecrawl | 要抽正文、整站爬 | `/crawler` 已有边界说明 | 边界页 | 保持 | 用来拒绝错误意图，不要去抢 Firecrawl 的词 |

中文组（`/zh/` 已有对等路由，缺的是意图页）：

- Agent 搜索 API
- MCP 联网搜索 / Claude MCP 搜索
- 中英双语 Search API
- RAG 搜索 API

同义词放同一页（search api for agents / agent search api / search api for rag）。MCP 和「双语索引」是不同操作需求，拆开。

---

## 5. 建议网站结构（词表 → URL，不是再画导航）

按哥飞：首页解释总需求和引导；栏目页分门别类；工具页满足一个明确需求；文章只补说明。

```text
/                         总需求：Agent / RAG 用的中英 Search API
/mcp                      工具：装上就能 search_web（复制 JSON）
/mcp/claude               工具：Claude Desktop / Claude Code 一步到位
/docs                     契约：auth、字段、错误、扣点
/docs/mcp                 可并入 /mcp，不要第三份薄页
/pricing                  价格意图（已有，TD 合格）
/benchmarks               评测方法 + 数字（已有）
/bilingual-search         中英同一个索引是怎么回事
/playground               试用（继续 noindex，登录后用）
/blog/*                   笔记，链回工具页，不承担主流量
/zh/...                   对等语言，服务端页面，已有 hreflang
```

现有法律页、about、status、security、crawler 保留。它们服务信任和合规，不承担获客词。

第一批只做 **5–8 个获客页**：首页改 TDH + `/mcp` + `/mcp/claude` + docs 收紧 + 中文对等。不要为了覆盖词再生成一堆相似 URL。

---

## 6. 对现有页面的具体改法

### 6.1 首页：现在在浪费主词

本地新站：

- Title：`Annolux Search — Focused bilingual search for agents`（还行）
- H1：`Search is how intelligence meets the world.`（哥飞会判不合格）

公网旧站更差：H1 同样是金句，主 CTA 指向旧产品仓库的 issues 页，文档链到 `./docs.html`。谷歌若先收旧版，会把品牌理解成另一个产品。

按他的 TDH：

- H1 写成用户搜的话，例如：`Bilingual Search API for AI agents`
- Title 把主词放前面：`Search API for AI agents — Annolux`
- Description 写清边界：精选中英索引、带 `fetched_at`、成功请求扣 1 点、不是实时全网
- 首屏就要能「试用」：curl 示例或不登录可看的请求/响应，不要先逼登录
- 金句可以留在下面，不能占 H1

### 6.2 `/mcp` 必须是工具页，不能只是 blog

`/blog/agent-search-mcp-quickstart` 已经有内容。按他的话：**用户要操作，就不要只给他一篇文章。**

`/mcp` 打开就应该有：

1. 一段话：这是什么、不是实时全网
2. 可复制的 MCP JSON（Claude / Cursor）
3. `search_web` 字段表（和 Search 契约同一份）
4. 一段真实响应，含 `fetched_at`
5. 链到 `/docs`、`/pricing`、中文页

这是最像他「单页工具站」的一页，也是最可能低 KD 起量的一页。

### 6.3 Docs

现在 Title / H1 都是 `Search API documentation`，description 在讲 auth 和错误。可以，但太泛。

建议 H1 贴近契约：`POST /api/v1/search`。用 H2 切开 Authentication / Request / Errors / Credits / MCP，每块用内链指回 `/mcp` 和定价。

### 6.4 技术 SEO：本地新站已经超过哥飞学员平均水平

已具备：静态 SSR、真 `<a>`、中英独立 URL、hreflang、canonical、sitemap、robots、JSON-LD、RSS、Lighthouse 接近满分。不要在这块再花大时间。

还缺的是运营动作，不是再写一套 schema：

1. **先切公网到本地这版站**。旧站还在，SEO 做了也是给错误产品做。
2. 用户侧完成 GSC / Bing / IndexNow 验证（`LAUNCH-STATUS` 里仍是 `WAITING_USER`）。
3. 提交 sitemap，用 `site:annolux.com` 盯收录。
4. Playground / account / login / checkout 继续 noindex，正确。
5. `robots.txt` 允许 `OAI-SearchBot`、拦住 `GPTBot` / `Google-Extended`：训练不喂、搜索型爬虫放行，和「会做 SEO 才能做 GEO」不冲突。切站后保持这个策略。

---

## 7. 第一个 30 天（听话照做）

目标不要定成「打赢 Tavily」。按他的第一个站：

1. 公网切到 Search-only 静态站，品牌词能搜到正确产品。
2. 选一个低难度组（大概率是 **MCP search_web / Claude MCP web search**），对应页进谷歌前 10。
3. GSC 能看到曝光，哪怕一天只有几十次。

一周节奏：

| 周 | 做什么 |
|---|---|
| 0 | 切站。GSC + Bing + IndexNow。首页改 H1/Title。上 `/mcp` 和 `/zh/mcp`。 |
| 1 | 不登录就能看懂并复制 MCP 配置。把 4 篇 Lab Notes 内链全部指向 `/mcp`、`/docs`、`/`。 |
| 2 | 发外链：Show HN、V2EX、MCP Registry（镜像公开之后）、GitHub README、相关仓库 topic。只发能被谷歌爬到、且指向意图页的链接。 |
| 3 | 看 GSC：哪些查询有曝光没点击 → 改 Title/Description；有点击没停留 → 页上补试用和内链。再加 `/mcp/claude` 或中英页，择一，不要两周加 10 页。 |

时间仍按 4:2:4：这 30 天不要优先加 Labs、不要优先扩索引、不要优先改定价。产品契约已经够小。

---

## 8. 外链：抄能抄的，不买脏的

哥飞会买外链。Annolux 是公开 API 产品，品牌和信任比游戏站值钱，**不要走自动发外链插件**。

优先抄 Agent Search / MCP 竞品常见的干净来源：

- 自己的 GitHub 仓库、Release、`server.json` / MCP Registry
- Show HN、V2EX（`launch-copy.md` 已有稿）
- MCP / Claude / Cursor 的 awesome 列表（人工提 PR）
- 评测文和「best web search MCP」综述：给可复现的查询和边界，让别人链到 `/mcp` 或 `/benchmarks`
- 中文：即刻、掘金、少数派开发频道，链到 `/zh/mcp`

不要：目录站群、Youtube Downloader 式注水词、刷 Similarweb。

---

## 9. 停留和转化（接住谷歌给的那点测试流量）

- Playground 对爬虫 noindex，但对人必须好用。从 `/mcp` 到第一次成功 Search 的路径要短。
- 文档页放可复制 curl，失败时明确错误码，不要静默忽略字段。
- 免费 1,000 点就是他的「不登录先试用」。付费结账未开，SEO 的转化目标先定成 **注册 + 第一次 Search**，不要定成付费。
- 中英互链已经做了。每个新意图页必须成对出现，否则 hreflang 会缺半边。

---

## 10. 不要做的事

- 不要再写「搜索如何遇见世界」这种 H1。
- 不要用博客农场覆盖 `best search api`。
- 不要为了 SEO 把 Labs / Verify 重新写回付费卖点。
- 不要在旧站上优化。切站是 SEO 第 0 步。
- 不要第一批做 vs Tavily 大词对比页。
- 不要把开发时间继续压过推广。本地门禁已经过了。

---

## 11. 和现有 launch 流程怎么接

`launch/README.md` 的顺序是：beta 10–20 人 → 公开仓库 → 发 HN / PH / V2EX。  
按哥飞，公开站和 `/mcp` 意图页应该 **与 beta 并行**，不要等「验证完再谈 SEO」。免费产品和 Playground 不依赖 Paddle Live。

建议把 SEO 验收加进切站后的清单：

- `site:annolux.com` 能看到新站，不是旧证据站
- GSC 已验证，sitemap 已提交
- 品牌词 Title/H1 与 fact-pack 一致
- `/mcp` 和 `/zh/mcp` 200、可索引、有独立 TDH
- 至少 3 条外部链接指向 `/` 或 `/mcp`（GitHub + 一篇社区帖即可起步）
