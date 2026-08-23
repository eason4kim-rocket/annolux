# 查询日志:实现与验收结果

对应任务书 `TASK-query-log.md`。发射前最后一块工程:先在 VPS 备用容器
(8083,镜像 `annolux-api-local:a15f46b`)通过全部五项验收,随后**已切换上线**
(见第四节)。

## 一、实现

新增 `querylog` 包(独立 `/data/querylog.db`,不碰 `accounts.db`):

- **采集点**:`publicapi` 两个 Search 入口(API key `/api/v1/search` 与
  Playground `/api/v1/playground/search`)共用的收尾函数。**到达引擎的请求**
  每条一行 —— 成功、零结果、引擎超时/失败、结算失败都记;鉴权/校验/限流
  拒绝不记(它们不反映检索行为,还会被刷量灌爆)。
- **原始特征来自 `hostedsearch` 边界**:`models.SearchResponse` 挂一个
  `json:"-"` 的 Observation(检索语言、候选数、top1-top2 原始分差、域名/去重
  剔除数、跨语言命中数、每条返回文档的原始 BM25 分)。公开响应结构体里该字段
  不可导出,序列化测试证明一个特征都漏不出去。
- **`searchindex/query.go` 一个字节没动**(ranking lock 哈希锁定,校验通过)。
  NEAR/AND/OR 匹配层级因此不在行内 —— 用 `query_text` + `ranker_version`
  (即部署 commit)对固定索引离线重放即可恢复,任务书要的「置信度可事后重算」
  由此保证。
- **异步有界写入**:1024 容量队列 + 单写协程 + 128 条/500ms 批量落盘。队列满
  或磁盘故障 → 丢日志计数,绝不阻塞请求;打不开日志库 → API 照常起,只是无日志。
- **隐私**:不记 key/Authorization/IP/UA;`query_text` 由 API 进程每小时自动
  脱敏(>30 天置空,行保留供聚合);主文件与 **WAL/SHM sidecar 均 0600**
  (modernc 驱动默认 0644,已修);`top_docs` 存 URL 的 SHA-256 前 16 位而非
  原文。
- **运维**:`annolux-admin querylog-stats PATH` / `querylog-expire PATH [DAYS]`;
  运维流量带 `X-Annolux-Ops` 头即不入日志(smoke.sh 已加);评测只打备用容器。

字段对照任务书:request_id/ts/account_id/query_text/lang/domains/http_status/
result_count/zero_result(独立列)/top_docs(hash+rank+score+lang)/
candidate_count/score_gap_top1_top2/signals(JSON:域名剔除、近重删除、跨语言
命中、dedup、partial、provider_ms、total_ms)/ranker_version/latency_ms,
另加 query_has_han/query_has_latin(文本脱敏后语言混合统计仍可算)。

## 二、验收(全部在 VPS 备用容器实测)

| # | 任务书要求 | 结果 |
|---|---|---|
| 1 | 打一批查询,字段完整、zero_result 正确、top_docs 与实际响应一致 | ✅ 英文/中文/domain 过滤/零结果各 1+;零结果行 `zero_result=1, candidate_count=0, gap NULL`;**top_docs 五条 hash 与真实响应逐位 MATCH** |
| 2 | healthcheck 与 v2 评测流量不入日志 | ✅ healthcheck 只打 `/ready`(构造上不可能入);`X-Annolux-Ops` 探针实测未入日志;评测按规程只打备用容器 |
| 3 | 日志写失败时 Search 正常返回 | ✅ 指向不可建路径 → `query log disabled` 后 API 正常服务;单测覆盖运行中写失败(丢弃计数,不 panic) |
| 4 | 0600、无 key/IP/UA | ✅ db+WAL+SHM 均 600;原始字节扫描无 key 前缀/Bearer/UA/IP 字面量 |
| 5 | 并发压测延迟无劣化 | ✅ 200 请求 ×8 并发 ×12 账号轮换:关日志 p50 115 / p95 196ms,开日志 p50 106 / p95 176ms —— 差异在噪声内,无劣化 |

## 三、过程中的一个重要发现:本地分支曾落后于线上

线上 e1d26e1 是 `origin/codex/launch-hardening` 分支尖(含 main 的 #15/#16/#17
与两个 hardening 提交,accounts schema 已到 v3),而本地 `codex/launch` 停在
a6168f7+cherry-pick。用本地代码建的镜像起不来(schema v3 > 本地 v2)。已把
`origin/codex/launch-hardening` **merge 进本地 `codex/launch`**(冲突仅
`scripts/site-check.sh`,取 hardening 侧),合并后全套测试、ranking lock、
race 全绿。**此后不要再从旧基线出补丁。**

## 四、切换状态:已切换(2026-08-17,用户授权)

线上 `annolux-api` 现为 `annolux-api-local:a15f46b`,healthy、零重启,
公网 `/health` `/ready` 返回 a15f46b,OAuth start 正常,`querylog.db`
已建(db/wal/shm 均 0600)。旧容器保留为 `annolux-api-pre-querylog`
(停止态),回滚 = rm 新容器 + rename 回 + start。

切换过程踩的两个坑,都已固化为流程:

1. **SSH 断连风险**:第一次尝试在 SSH 会话里跑切换序列,连接被服务端掐断
   (缩短版 ssh 命令递了多把 key 触发 MaxAuthTries)。此后破坏性序列一律
   写成服务器上的脚本 `ops/cutover-querylog.sh`,`setsid` 脱离执行,
   SSH 只做触发与看日志;脚本自带就绪轮询(40s)与自动回滚,回滚路径
   在第二次尝试中被真实触发并验证有效。
2. **Paddle env 重复键**:旧容器 env 里 `ANNOLUX_PADDLE_ENVIRONMENT`
   出现两次(live 在前、sandbox 在后)。捕获 env 时按 Go 的
   first-occurrence 语义取到 live,而 hardening 后的二进制对
   「live 未批准」正确地拒绝启动 → 自动回滚。修复:切换脚本显式写
   `ANNOLUX_PADDLE_ENVIRONMENT=sandbox`(Live 冻结的本意)。
   **给下次的提醒:该容器的 env 已在 `/opt/annolux/secrets/api-cutover.env`
   规范化(无重复键),以后以它为准。**

## 五、发射后用法(本轮不做)

- 语义触发门槛:`SELECT count(DISTINCT query_text)` ≥500 后人工审词汇错配比例。
- 定向补内容:`WHERE zero_result=1` 与低 candidate_count 分布按 lang 聚合。
- 查询驱动晋升:top_docs 的 hash 对回索引 URL hash 即得文档命中频次。
- 点击/反馈记录是独立后续任务,不阻塞发射。
