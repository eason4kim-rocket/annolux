# Private support playbook

Use `support@annolux.com` for product, account, billing, and refund questions;
`privacy@annolux.com` for rights requests; and `security@annolux.com` for
responsible disclosure. Never redirect these matters to a public issue.

## First response

Ask for account ID, server-generated request ID, UTC timestamp, endpoint, HTTP
status, and a redacted request shape. Explicitly tell the user not to send a raw
API key, OAuth token, password, full query containing confidential data, or card
information.

## Common outcomes

- **401:** confirm Bearer format, then rotate the key if exposure is possible.
- **402:** explain remaining balance and no-overage behavior; do not promise a
  manual grant.
- **429:** explain the account-wide 5 RPS/burst 10 limit; multiple keys do not
  multiply it.
- **Unsupported freshness:** explain the bounded periodic index and suggest a
  realtime source when that is the actual requirement.
- **Relevance issue:** request a non-secret query, observed URL, expected URL,
  and reason. Adjudicate before changing ranking or collecting.
- **Refund:** verify account and Paddle transaction reference, preserve the
  request date, apply mandatory rights first, and use Paddle for the refund.

## Incident levels

- `SEV-1`: credential exposure, account takeover, credit corruption, data loss,
  or materially incorrect payment fulfillment. Freeze billing/affected writes,
  preserve evidence, and post a status update.
- `SEV-2`: sustained Search outage, OAuth outage, or p95/5xx gate breach.
- `SEV-3`: isolated relevance, documentation, or UI defect.

Incident notes contain timestamps, affected scope, containment, recovery,
verification, and follow-up owners. Public notes exclude credentials, private
queries, IP addresses, and unconfirmed root causes.
