# Cloudflare edge rules

Configure these rules in this order. They are account-side state and are not
created by the deployment script.

Create redirect rules from most specific to least specific. Every target must
set both `https` and the apex host so combinations never chain:

1. `lang=zh` plus an English `.html` path -> apex `/zh/` extensionless path.
2. `lang=zh` plus any other English path -> apex `/zh/` path; drop `lang`.
3. Any `.html` path -> apex extensionless path.
4. HTTP or `www` -> the same path on `https://annolux.com`.
5. Route `annolux.com` to the web origin and `api.annolux.com` to the API origin.

Rules 1–3 must also normalize `/index.html` and `/zh/index.html` to `/` and
`/zh/`. Do not implement these as four independent redirects whose targets
retain an invalid scheme or host; that produces two-hop chains.

Each legacy URL must reach its canonical URL in one redirect. Verify the matrix
in `deploy/launch-gates.md` before changing traffic.
