# 任务:分层索引落地(去重 → Tier 1 词法草图 → RRF 融合 → 查询驱动晋升)

对应 `INDEX-SCALING-RESEARCH.md` 第二轮复核后的定案架构。本文档把架构翻译成
schema、代码边界、门槛与顺序。**原则:广度由 Tier 1 廉价草图承担;完整正文
和位置索引是页面靠真实使用挣来的,不是预先白给的。**

## 0. 清理后的重测数字(A0 已完成,2026-08-17,`annolux-dedup` dry-run 实测)

研究里的旧统计(251,368 URL / 63,010 重复 / 9,071 空正文 / 超长 URL 占 69%
字节)作废。清理后 227,094 页的实测:

| 指标 | 数值 |
|---|---|
| 唯一正文 hash | 181,441(en 169,391 / zh 12,050) |
| 可折叠重复 URL | **45,643(20.1%)**,22,276 组 |
| en 重复 | 37,049(17.9%) |
| zh 重复 | 8,594(**41.6%** —— 真实中文正文仅 12,050 份) |
| 超 500 字节 URL | 0(最长 414B;清理已根除超长 URL 问题) |
| 盲测目标在重复组 | 33 个受 `-watch-urls` 保护;9 组含多目标,整组跳过 |
| 重复大户 | big5.www.gov.cn 5,908、rfc-editor www/裸域双份 6,329、docs.oracle.com 3,685、go.dev 1,579 |

A1 apply 后预期:**181,451 页 + 45,643 条 alias**。报告存
`/opt/annolux/eval/rounds/dedup-stats-A0.json`。

## 1. 与排序冻结的关系(本设计唯一的硬约束)

`searchindex/query.go`、`segment.go` 被 ranking lock 哈希锁定,政策是
「任何 digest 变化都视为排序变更,必须用全新盲测集评过才能接受新锁」。因此:

- **两个锁定文件零改动。** Tier 0 检索链路(含其内部有界重排)原样保留。
- 融合作为**外层新代码**实现:Tier 0 走冻结的 `Store.Query()` 不动;Tier 1
  是新表、新查询函数(新文件);RRF 在两条已排序的通道**之外**按名次融合。
  研究图里"RRF → 现有有界重排"在冻结约束下取务实读法:有界重排只在 Tier 0
  通道内部发生(它本来就在冻结代码里),Tier 1 通道用 FTS5 列权重 bm25 排序,
  RRF 用名次吸收两边分数不可比的问题。这是与研究图的一处**明示偏差**,
  由 v3 盲测门槛裁决效果。
- **feature flag**:`ANNOLUX_TIER1_FUSION` 默认 off。off 时行为与今日逐位相同,
  代码可以先上线"暗置",开关由门槛决定。
- **v3 盲测集是并行工作流**:在任何融合调参之前著作并冻结(60 dev / 40
  heldout);调参只许用 v3-dev(v2-dev 也可);开启融合的门槛 = v3-heldout
  一次通过 + v2-heldout 回归不降(它的目标页都还在)。
- 纯数据变更(去重迁移、Tier 1 爬取)不触锁,用现有 v2-heldout 做回归门槛,
  与索引清理同一先例。

## 2. 第一步:精确去重 + URL alias(研究认定的当前第一收益)

schema(`schemaMigrationV3`,同一 index.db):

```sql
CREATE TABLE url_aliases (
    url        TEXT PRIMARY KEY,
    page_id    INTEGER NOT NULL,   -- 指向保留的 canonical 页
    fetched_at INTEGER NOT NULL,
    etag       TEXT NOT NULL DEFAULT '',
    last_mod   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX url_aliases_page ON url_aliases(page_id);
```

迁移(候选副本方式,复用清理任务的整套流程):按 `content_hash` 分组,
每组选一个 canonical 保留,其余降级为 alias:删 `pages` 行与对应语言 FTS 行
(contentless_delete),frontier 不动。canonical 判定为确定性打分:
HTTPS > HTTP;host 等于 root host 优先;路径短、无查询参数、无 `index.html`
尾巴优先;字典序兜底。**判定函数单元测试先行。**

检索面不需要读 alias 表(线上响应本来就做了同 hash 去重,去重后只是候选更干净)。
已知取舍:alias 的 URL 以后内容漂移不再被刷新——它本来也不再被返回,记录在案。

门槛:`PRAGMA integrity_check` + 两张 FTS `integrity-check` + 计数对账 +
v2-heldout 回归零下降 + `annolux-indexclean` 幂等复跑为零。

## 3. Tier 1:轻量发现层

同一 index.db 内新增(en/zh 各一张 FTS,分词复用冻结的 `indexText` 路径——
调用不是修改):

```sql
CREATE TABLE pages_lite (
    id           INTEGER PRIMARY KEY,
    url          TEXT NOT NULL UNIQUE,
    root         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',  -- meta description 或首个有效段落;snippet 来源
    lang         TEXT NOT NULL,
    fetched_at   INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    promoted     INTEGER NOT NULL DEFAULT 0
);
CREATE VIRTUAL TABLE lite_fts_en USING fts5(
    title, headings, salient_hi, salient_lo,
    content='', contentless_delete=1, detail=column, tokenize='unicode61');
CREATE VIRTUAL TABLE lite_fts_zh USING fts5(... 同上 ...);
```

- `detail=column`:保留列权重、不存词位;phrase/NEAR 本来就只属于 Tier 0。
- 列权重经 `bm25(lite_fts_en, w_title, w_headings, w_hi, w_lo)` 表达重要度。

**词法草图**(设计的关键巧思,不用模型):

- 语料 DF 表 `df_stats(lang, term, df)`,先用现有 22.7 万正文建底,后续增量。
- 每页:正文分词 → 唯一词 → 按 `tf·log(N/df)` 打显著度 → 剔除已在
  title/headings 里的词 → 取 top(min 32 / max 128)→ 前半进 `salient_hi`,
  其余进 `salient_lo`。确定性、可重算、可离线验证。
- `needs_render` 页只有 title+meta:照收进 lite(发现层允许薄),不设草图。

**先回填后爬新**:第一批 lite 行从现有正文离线生成。**B2 实测(2026-08-17,
去重后 181,451 页)**:DF 建表 592 秒,回填 1,208 秒,**1,926 B/页**(含
df_stats 固定成本与行开销;高于研究先验 1,033–1,264,按此推 5M lite ≈ 9.6GB、
10M ≈ 19GB,5–10M 目标仍成立但更紧)。去重回收 421MB > lite 新增 349MB,
候选库反而比线上小 72MB。抽样质量检查以 D2 调参的批量查询覆盖,未单独人工抽 20 页。

**广度爬取**:`ops/seeds-head-coverage.json`(~110 个头部文档根,en 为主
zh 少量)就是 Tier 1 的第一批种子。爬虫加 `-tier=lite` 写入路径:同一套
fetch/clean/junk/robots,存 lite 行与草图,不存 body_z。容量目标按研究修正:
**先 100 万页级验证,5–10M 是现实目标,30M 只是上界不做承诺。**
磁盘护栏沿用(82% / 25GiB 停机)。候选副本 + 门槛 + 切换,全套复用。

## 4. 查询融合(新代码,冻结圈外)

```
query → Tier0: 冻结 Query() 取窗口 40   ┐
      → Tier1: lite bm25 列权重 取窗口 40 ┴→ RRF(k=60) → 现有 hostedsearch
                                             归一/去重/截断(不动)
```

- Tier 1-only 命中的响应字段:snippet=summary,fetched_at=lite 抓取时间,
  公开契约不变。
- 观测:querylog 的 signals 增记 `tier1_hits` / `fused`(schema 已留 JSON 位)。
- 开关 off = 不进融合分支,逐位等于今日行为。

## 5. 查询驱动晋升 / 降级(发射后开启)

- 晋升信号:querylog `top_docs` 的 hash 命中频次(≥N 次/窗口)起步;
  将来加显式取正文请求。动作:后台抓全文 → 走现有 `UpsertMany` 进 Tier 0 →
  lite 行标 `promoted`。
- 降级:Tier 0 设容量上限(先 30 万):超限时按低使用×陈旧驱逐——删正文与
  FTS、保 lite 行。盲测语料目标页永不驱逐。
- 这就落实了"先给 URL、内容按需拿":发现层给地址,正文靠需求挣。

## 6. 顺序、工期、门槛

| # | 事项 | 工期 | 门槛 |
|---|---|---|---|
| 0 | 清理后库重测重复/字节统计 | 0.5 天 | 数字进本文档替换旧表 |
| 1 | 去重+alias 迁移(候选副本) | 2–3 天 | integrity + v2 回归零降 |
| 2 | DF 表 + 草图器 + lite schema + 22.7 万回填 | 4–5 天 | 实测 $/页、构建耗时公布 |
| 3 | v3 盲测语料著作(与 2 并行) | 2 天 | 冻结 manifest,先于一切调参 |
| 4 | 融合暗置 + v3-dev 调参 | 3–4 天 | v3-heldout 一次过 + v2 回归 |
| 5 | Tier 1 广度爬取(与 4 并行,先 100 万级) | 数天起,后台持续 | junk 拦截复核 + 候选门槛 |
| 6 | 开启融合、切换上线 | 0.5 天 | 复用已验证的 detached 切换 |
| 7 | 晋升循环 | 3 天 | 发射后按真实 querylog 喂 |

到第 6 步 ≈ **2.5–3.5 周**。**发射(对外推)排在第 6 步之后**:那时随手搜
不再翻车的原因是发现层真的有广度,而不是把全文库硬塞大。

## 7. 明确不做(维持研究否决)

SPLADE/Seismic(独立引擎、非商业许可、内存模型不符)、稠密 HNSW、生成式检索、
FM-index。二值语义 sidecar 维持原门槛:≥500 条去重真实查询、审出 ≥20%
词汇错配才立项——它在图里的位置就是"将来且仅必要时"。
