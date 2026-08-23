# search-generalization-v3

冻结于 2026-08-17。这套语料存在的唯一目的:**裁决 Tier 1 发现层 + RRF 融合
能否开启**。v2 的 heldout 已在多次门槛中使用,不再适合裁决排序输出的变更。

## 构成

100 例,dev 60 / heldout 40,两个 split 同分布:

- **core-v3(55 例)**:目标 URL 全部取自已验证在产的页面(v2 目标清单 +
  会话内实测存在的 URL)。它们衡量融合**不许伤害**现有质量。
- **head-coverage(45 例)**:目标是 `ops/seeds-head-coverage.json` 头部
  文档站的经典页(react.dev、vuejs.org、typescriptlang.org、docs.stripe.com
  等)。冻结时这些页**尚不在索引中** —— 它们衡量发现层爬取 + 融合带来的
  增益。今天在 Tier 0-only 上跑,这 45 例应大面积 miss;这正是基线。

语言:en 82 / zh 14 / mixed 4。类别:technical 54 / reference 46。

## 覆盖义务(语料冻结,义务在爬取侧)

head-coverage 的目标 URL 按站点自身的 canonical 形态书写,但站点未爬前无法
逐条确证。规则:**B3 首批爬取合入候选后、跑任何 v3 评测前**,用
`annolux-indexclean -watch-urls`(全部 v3 目标)对候选出报告;若 head 目标
的入库率 < 85%,先扩爬到达标,再进评测。**不修改语料去迁就爬取。**

## 使用政策

- 融合调参只许用 dev split(v2-dev 可辅助);
- heldout 只在融合开启门槛跑一次(`release-gate.json`);
- 门槛失败:heldout 即曝光,生产维持 flag off,诊断用例转入未来 dev 池,
  开启前必须另著新盲测集;
- 本语料**不用于**数据变更回归(去重、清理、爬取合入)—— 那是 v2-heldout
  的职责,它对回归仍然有效,因为其目标页原封保留。

## 复现

`gen_v3.py`(同目录)确定性生成三个文件;cases_sha256 见 manifest。
