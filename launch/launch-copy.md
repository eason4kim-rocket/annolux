# User-operated launch drafts

Replace bracketed launch-day facts only with verified values. The user—not an
automation—submits posts and replies.

## Show HN

**Title:** Show HN: Annolux – a focused bilingual Search API for agents

**Body:**

I built Annolux because agent search integrations often hide important
boundaries: what corpus is searched, when a page was fetched, and what an error
costs.

Annolux exposes one standard Search endpoint and one MCP tool. The current
English/Chinese index contains [verified page count] pages. Every result exposes
`fetched_at`; unsupported freshness requests fail explicitly. A successful
Search costs one credit, while errors and timeouts cost zero. New accounts get
1,000 credits without a card.

I published the source, OpenAPI contract, frozen evaluation method, and the
limitations. The latest no-tuning prelaunch rerun on the 40-query held-out set
is Hit@10 85% with p95 356 ms. The immutable first gate recorded p95 532 ms;
both are finite target-retrieval benchmarks—not whole-web claims.

Links: [website] · [GitHub] · [OpenAPI] · [benchmarks] · [method note]

I would especially value reproducible misses from real Agent/RAG workflows.
Please do not post private prompts or API keys.

## Product Hunt

**Tagline:** Focused bilingual web search for AI agents

**Description:** Annolux gives agents a bounded English/Chinese Search API and
a single `search_web` MCP tool. Results include source URLs and fetch times;
the pricing and credit contract is explicit. Free signup includes 1,000 credits
without a card.

Prepare three first comments: why the index is intentionally bounded, how the
frozen benchmark works, and why Search results are not labeled as evidence.

## V2EX

**标题：** 做了一个面向 Agent 的中英双语精选 Search API，公开索引边界和评测方法

**正文要点：** 说明真实索引规模、`fetched_at`、1 次成功请求扣 1 点、注册送
1,000 点、REST/MCP 同一契约、不是实时全网搜索。附官网、OpenAPI、GitHub、
Benchmarks 和中文 Lab Notes。请大家提交可复现查询，不收集私密 prompt。

## Reply discipline

- Answer limitations before defending design choices.
- Link to a reproducible artifact where possible.
- Mark inferences as inferences.
- Do not promise a collection date, uptime, ranking fix, refund outcome, or
  Paddle Live date before the corresponding gate passes.
- Move account, billing, privacy, and security details to private email.
