# 索引历史数据清理:执行结果

对应任务书 `TASK-index-cleanup.md`。全程 **候选副本方式**:线上索引只被读取,
从未被写入;若门槛不通过,做法是丢弃候选库,而不是回滚线上库。

## 一、工具

新增 `cmd/annolux-indexclean`(`make build` 产出 `annolux-indexclean`),
配套 `ops/clean-index.sh`。

- **判定唯一来源是现有 `junkURL()`**。`indexer/junkcleanup.go` 只导出
  `JunkURL`(直接转调 `junkURL`)和 `JunkReason`(只对已被判定为垃圾的 URL
  归因,用于报表);`indexer/junk.go` 一个字符未改。
- 默认 dry-run,必须显式 `-apply` 才删除。
- 清理范围:`pages` + 对应语言的 `pages_fts_en` / `pages_fts_zh` 行(FTS5
  contentless_delete)+ `frontier` 同类 URL + 空正文记录,最后 `VACUUM`。
- `-watch-urls` 逐条报告指定 URL 的命运,用于在删除**之前**验证评测目标不会丢。
- 删除后校验:`PRAGMA integrity_check`、两张 FTS 表的 `integrity-check`、
  语言/FTS/frontier 计数与删除计划逐项对账、WAL 必须为空、记录 SHA-256。

## 二、清理规模(实测全库,非抽样外推)

清理前:251,368 页(215,485 en / 35,883 zh),2,941,472,768 字节,
frontier 438,269 行(pending 140,093 / done 278,095 / failed 20,081)。

| 类别 | 页数 |
|---|---|
| 当前规则判定为垃圾 | 15,215(en 8,708 / zh 6,507) |
| ├ 路径陷阱 `trapped_path` | 8,424 |
| ├ 外语主机 `foreign_locale_host` | 4,065 |
| ├ 外语路径 `foreign_locale_path` | 1,847 |
| └ 资源扩展名 `asset_extension` | 879 |
| 空正文(其中 9,059 条不属于垃圾) | 9,071(en 337 / zh 8,734) |
| **合计删除** | **24,274(en 9,036 / zh 15,238)** |
| frontier 同类 URL | 33,665 |

垃圾占比 6.05%,与任务书抽样外推的 5.8% 一致。合计删除占全库 9.66%。

删除量 Top 主机:

| 主机 | 删除 | 垃圾 | 空正文 |
|---|---|---|---|
| news.cnblogs.com | 7,306 | 0 | 7,306 |
| big5.www.gov.cn | 6,492 | 6,491 | 1 |
| developer.mozilla.org | 1,521 | 1,520 | 1 |
| edu.cnblogs.com | 1,366 | 0 | 1,366 |
| ab.wikipedia.org | 1,356 | 1,356 | 0 |
| smalltalk.gnu.org | 1,184 | 1,184 | 0 |
| es.tldp.org | 1,118 | 1,118 | 5 |
| jigsaw.w3.org | 839 | 825 | 14 |
| doc.rust-lang.org | 575 | 575 | 0 |
| kubernetes.io | 328 | 328 | 0 |

**任务书没有预料到的一条**:空正文的绝对主力是 cnblogs
(news 7,306 + edu 1,366 = 8,672,全部中文),它们抓到了页面但正文抽取为空。
因此中文侧删除 15,238 条 = 中文索引的 **42.5%**,英文侧只删 4.2%。
这个不对称在任务书里没有出现,验收门槛因此以中文用例是否掉分为准。

## 三、安全性:评测目标零丢失(删除前验证)

把 v2 语料 100 个用例的全部 **116 个去重目标 URL** 交给 `-watch-urls`,
在删除发生之前逐条判定:

```
116 条全部在库,其中会被删除的:0
```

任务书只核了 `junkURL()` 一路;这里把**空正文那一路也一并核了**,结论不变:
一个评测目标都不会丢。

## 四、候选库校验

清理后:227,094 页(en 206,449 / zh 20,645),2,573,348,864 字节(回收 368 MB),
frontier 404,604 行(删掉的 33,665 行全是 done/failed;pending 140,093 未变 ——
在线抓取时 `PrunePending` 已经在拦 pending 队列了)。

```
PRAGMA integrity_check           ok
pages_fts_en / pages_fts_zh      integrity-check ok
FTS 行数 == 各语言页数           206,449 / 20,645  逐项相等
删除数与计划对账                 pages -24,274,frontier -33,665  完全一致
WAL                              0 字节
第二次 --apply(幂等性)          junk 0,空正文 0,SHA-256 不变
候选库 SHA-256                   0e0fbea9e7ab8f82f8ce8d7fd781267a75b45646c6d90548925665233708e503
```

## 五、验收结果:通过,指标零变化

同一份 API 镜像、同一个备用端口(8083),只换索引文件,跑两次 v2 held-out:

| 指标 | 清理前 | 清理后 | 变化 |
|---|---|---|---|
| Hit@1 | 0.725 | 0.725 | 0 |
| Hit@3 | 0.825 | 0.825 | 0 |
| Hit@10 | 0.85 | 0.85 | 0 |
| MRR@10 | 0.78125 | 0.78125 | 0 |
| mean nDCG | 0.79886 | 0.79886 | 0 |
| errors | 0 | 0 | 0 |
| client p95 延迟 | 328 ms | 299 ms | −29 ms |

清理前那一跑与冻结基线 `gate-result.json` 逐位相同,说明这套对比是干净的。
**40 个用例的 verdict 和 reciprocal_rank 逐条比对完全一致**,中文用例也没有一条掉档
(尽管中文库删掉了 42.5%)。门槛是"任一指标下降超过 1 个点就不切换",实际下降 0。

德语页占位情况:

| 用例 | 清理前 | 清理后 |
|---|---|---|
| kubernetes-horizontal-pod-autoscaling | rank 3 `kubernetes.io/de/...` | 已清除 |
| css-flex-layout-mixed | rank 2、6 `developer.mozilla.org/de/...` | 已清除 |
| php-generators | rank 2、3 `br2.php.net/manual/de/...` | **仍在**(见下) |

`React useState hook` 已无 `developer.mozilla.org/de/` 页面 —— 但这条查询暴露了
**覆盖缺口而不是排序问题**:库里 MDN 的 React 学习页只有德语版,没有英语版,
德语页清掉后前十全是无关内容。同一批英语 MDN CSS 页仍在且排在正确位置
(`css flex container` 前四条全是 `developer.mozilla.org/en-US/...`),
说明这是抓取覆盖问题,不是清理伤到了英文内容。

### 当前规则的一个漏网实例

任务书说"除非你能拿出当前规则漏网的实例",这里有一个:

```
http://br2.php.net/manual/de/class.generator.php
http://br2.php.net/manual/de/class.appenditerator.php
```

`foreignLocaleHost` 看的是首标签(`br2`,三字母且不在表里),
`foreignLocalePath` 看的是首个路径段(`manual`),所以 `/manual/de/` 这种
**第二段才是语言码**的形状两条规则都漏。**本轮没有改 `junk.go`** ——
按任务书要求排序与规则冻结,这条留作下一轮独立评审。

## 六、切换状态:已切换(2026-08-17,用户授权)

- 先删备用容器,再 stop `annolux-api` → 换文件 → start,停机约十秒。
- 线上 `/opt/annolux/data/index.db` 的 SHA-256 与通过门槛的候选库逐位相同
  (`0e0fbea9…708e503`);`/api/v1/ready`(search-aware)ready,容器 healthy、
  零重启,`api.annolux.com` 公网 200。
- 切换后用同镜像挂线上索引文件(只读)实测:`React useState hook` 无任何
  `/de/` 页;`kubernetes horizontal pod autoscaling` rank 1 正确。
- 旧库原封不动保留为 `index.db.pre-cleanup-20260817`(含 -shm/-wal),
  回滚 = stop → 两个文件名换回 → start。

## 七、遗留

- `content_hash` 去重是下一个独立任务。本轮删掉了 24,274 页,其中陷阱页与空正文
  都是重复大户,**去重任务开工前必须重新统计重复率**,不要沿用 25.1%。
- cnblogs 类"抓得到页、抽不出正文"的站点,属于抓取侧问题(可能需要渲染),
  不在本轮范围内。
