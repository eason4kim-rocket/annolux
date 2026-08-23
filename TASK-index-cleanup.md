# 任务:索引历史数据清理(不是修爬虫)

**给 Codex。先读完再动手。这不是「修一个 bug」,是「清理 bug 修复之前留下的数据」。**

## 一、先纠正一个可能的误判:爬虫已经修好了

不要去改 `indexer/junk.go` 的检测逻辑。**它已经能拦住这些陷阱了。**

我从生产索引里取了 40 条超长 URL(>500 字符),用**当前代码里的 `junkURL()`**
逐条跑过:

```
当前 junkURL 规则:  40/40 被拦截 (100.0%)
```

`trappedPath()` 的注释里甚至已经点名了 `smalltalk.gnu.org/manual-base/manual-libs/...`
这个形状。**规则是对的,问题是这些页面在规则加进来之前就已经进库了。**

所以本任务的目标是:**把当前规则回溯应用到已有数据上。**

## 二、清理规模(实测,非估算)

从生产 `index.db` 随机抽 8,000 条 URL,用当前 `junkURL()` 判定:

```
样本 8,000 条 → 应被拒绝 464 条 (5.8%)
外推全库 251,369 页 ≈ 14,600 页
```

Top 主机(样本内计数):

```
big5.www.gov.cn          194   ← 自引用路径陷阱
developer.mozilla.org     48   ← 德语 /de/ 页(见下,这是个可见质量 bug)
ab.wikipedia.org          42   ← 阿布哈兹语维基
es.tldp.org               39   ← 西班牙语
smalltalk.gnu.org         35   ← 路径循环陷阱
jigsaw.w3.org             28
kubernetes.io             15   ← 同样是 /de/ 等外语路径
doc.rust-lang.org         11
```

**这一次清理同时解决三个已知问题:**

1. **gov.cn 路径陷阱** —— 最大的三个重复簇(5,921 + 4,917 + 1,508 = 12,346 页)
   全是同一个自引用循环:
   `big5.www.gov.cn/gate/big5/.../fuwu/gongmin/anju/fuwu/gongmin/anju/...`
2. **德语页面污染英文结果** —— 实测线上查询 `React useState hook`,
   返回的前四条**全部是 MDN 的德语页**(`developer.mozilla.org/de/docs/...`)。
   这是用户一眼可见的质量问题,而这些页按当前规则本来就该被拒。
3. **外语维基/文档站** —— ab.wikipedia、es.tldp 等,同样早该被拒。

## 三、安全性已核实:不会伤到盲测

我把 v2 held-out 涉及的全部 263 个 URL(targets + results)跑了一遍 `junkURL()`:

```
会被清掉的:3 条
  作为【评测目标】出现: 0    ← 关键:一个目标都不会丢
  作为【搜索结果】出现: 3
     kubernetes-horizontal-pod-autoscaling | verdict strong | rank 3 | .../de/...
     css-flex-layout-mixed                 | verdict strong | rank 2 | .../de/Glossary/Flex
     css-flex-layout-mixed                 | verdict strong | rank 6 | .../de/Glossary/Flex_Container
```

**三条全是德语污染页占着结果位,而且这两个用例本来就判 strong。**
删掉只会让结果列表更干净,不会让任何目标丢失。

**预期:清理后 held-out 指标不降,有可能微升。若下降,说明清理逻辑有问题,立刻回滚。**

## 四、要做的事

### 4.1 主任务:回溯清理 —— **候选副本方式,严禁直接改线上库**

**关键原则:验证必须发生在切换之前,不是切换之后靠回滚补救。**

按顺序执行:

1. **先在当前线上索引跑一次 v2 held-out,记录清理前基线。**
2. checkpoint(WAL 落盘)后,把 `index.db` **复制成独立候选库**。
   线上库从此刻起不再被本任务触碰。
3. **只在候选库上**执行清理:
   - 遍历 `pages`,对每条 URL 调用**现有的** `junkURL()`;命中的从 `pages` 删除,
     并从 `pages_fts_en` / `pages_fts_zh` 删除对应 rowid
     (FTS5 建表带 `contentless_delete=1`,支持删除)
   - **同样清理 `frontier` 表** —— 队列里同类 URL 一并剔除,否则下次采集会重新爬回来。
     `junk.go` 注释本来就说这个函数「guards both link admission and frontier pruning」
   - 删除**空正文**记录(`body_z is null or length(body_z)=0`,共 9,071 条,
     且全部是同一个 content_hash)
   - `VACUUM` 回收空间
4. **校验候选库**:SQLite `PRAGMA integrity_check`、中英文 FTS 条数与 `pages` 一致、
   frontier 计数合理、记录文件 hash 与大小。
5. **用备用 API 端口加载候选库**(不影响线上服务)。
6. 在备用端口上跑清理后 v2 held-out,以及线上查询 `React useState hook`。
7. **指标通过才切换**;旧库原封不动保留,作为即时回滚路径。

**工具要求:**

- **默认 `dry-run`,必须显式 `--apply` 才真删。** dry-run 输出完整清理报告
  (各类命中数、预计删除页数、预计回收空间)。
- **以现有 `junkURL()` 为唯一判定来源** —— 不许复制一套判断逻辑,不许改 `junk.go`。
- 输出报告:各类命中数、清理前后库大小与页数、按主机的 top 分布。

> 说明:这不是此前用户决定暂缓的 R2 离机备份,而是**本次破坏性操作必须有的本机回滚副本**。
> 两件事不要混为一谈。

### 4.2 验收门槛

对比清理前后的 **Hit@1 / Hit@3 / Hit@10 / MRR@10**,以及 `latency_ms` 分布:

- **任一检索指标下降超过 1 个点 → 不切换,保留线上库,说明原因。
  不要自行调参补救**(排序冻结中)。
- `React useState hook` 结果中不应再出现 `developer.mozilla.org/de/` 页面。
- 预期:指标不降,有可能微升(德语污染页此前占着 rank 2/3/6)。

### 4.3 明确不做

- **不要改 `junk.go` 的规则**(已验证有效)。除非你能拿出当前规则漏网的实例。
- **不要动排序权重**(排序冻结中)。
- **不要启动新的采集**。
- **不要做 content_hash 去重** —— 那是下一个独立任务,见第五节。
- **不要碰同机上的其他产品目录**(具体路径见 `OPS.local.md`)。

## 五、下一个任务(本轮不做,先记着)

`content_hash` 去重:全库 251,369 URL 只对应 188,359 份独立正文,
**25.1% 是重复内容**;中文侧尤其严重(35,883 URL → 12,064 独立正文,66% 重复)。

目标形态:同一正文只保留一份 `body_z` 和一份 FTS posting,
其余 URL 降级为轻量 alias 记录。

这涉及 schema 变更和检索路径改动,**必须单独设计、单独评审**,不要和本轮混做。
注意:上面这个 5.8% 清理会先干掉一部分重复(陷阱页本身就是重复大户),
所以**去重任务开工前要重新统计,不要沿用 25.1% 这个数**。

## 六、背景

完整的索引扩展方案讨论见 `INDEX-SCALING-RESEARCH.md`(含第二轮复核结论)。
本任务是那份文档里「优先级高于一切扩容」的那一条。

在这次清理完成并验收通过之前,**不要开始任何索引扩容工作** ——
带着这些垃圾去扩到几百万页,只会按比例放大。
