# Private beta program

## Participant profile

Invite 10–20 developers who already operate an Agent, RAG pipeline, coding
assistant, research workflow, or MCP client. Avoid broad mailing lists and
incentivized signups. Each invite should explain why that person's concrete
workflow is relevant.

## Private invite draft

> I am testing Annolux, a focused English/Chinese Search API for agents. It has
> a small, explicit contract and exposes when each result was fetched. The beta
> is free and I am looking for feedback from a real workflow—not a testimonial.
> If this matches something you already run, I can send onboarding details.

## Onboarding checklist

1. Confirm the participant accepts the Terms, Privacy Policy, and AUP.
2. Sign in with Google or GitHub and create a separately named API key.
3. Complete one REST or `search_web` MCP call without sharing the raw key.
4. Record whether the first useful result arrived within ten minutes of signup.
5. Ask for three real, non-secret queries from the participant's workflow.
6. Capture only consented feedback; never copy private prompts or customer data.
7. Revoke the beta key on request or at account closure.

## Feedback labels

- `contract`: request, response, errors, docs, or SDK/MCP friction.
- `relevance`: ranking issue where the needed page already exists in the index.
- `content-gap`: a needed page is demonstrably absent.
- `freshness`: the index is too old for the workflow; do not “fix” this by
  claiming realtime coverage.
- `operations`: latency, availability, account, key, or credit issue.

Only adjudicated `content-gap` evidence may produce a targeted collection list.
Do not tune frozen ranking against held-out or public-launch queries.

## Exit gate

Proceed to a public free launch only after at least 10 distinct participants
complete a first call, at least 3 show repeated use on separate days, there is
no unresolved P0 security/billing/data-loss issue, and the launch metrics gate
passes. Record counts, dates, and evidence; do not infer them from page views.
