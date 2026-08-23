# Security policy

## Reporting

Report vulnerabilities privately to `security@annolux.com`. Do not include API
keys, personal data, exploit payloads, or account details in public GitHub
issues. Include the affected endpoint/version, reproduction steps and impact.

We will acknowledge a valid report after the support mailbox is operational.
No bug-bounty payment is promised. Please avoid privacy violations, service
disruption, social engineering, denial of service and accessing data that is
not your own.

## Supported versions

Until the first tagged release, only the commit digest currently deployed at
`api.annolux.com` is supported. After tagged releases begin, the latest minor
release will receive security fixes.

## Deployment boundary

- Public account administration is intentionally absent.
- API keys are shown once and stored as keyed hashes.
- OAuth uses state, nonce and PKCE; write requests require exact Origin and
  CSRF validation.
- Account and search databases are separate SQLite files.
- Billing Live remains disabled until all paid-launch gates pass.
