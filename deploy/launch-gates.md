# Annolux launch gates

`PASS` means independently verified. `WAITING_USER` is an external identity or
account action. A failed gate cannot be waived by a successful deployment.

## Free public launch

- [x] Clean public Git repository exists under the intended owner.
- [x] Repository policy, tests, vet, vulnerability, secret and site checks pass.
- [ ] A dedicated GHCR credential with only `read:packages` can pull the
  private API/web images by immutable digest. `WAITING_USER`
- [ ] Verified seller legal name, country and governing law are supplied. `WAITING_USER`
- [ ] Google and GitHub OAuth applications and exact callbacks exist. `WAITING_USER`
- [ ] Four support aliases deliver to the verified mailbox. `WAITING_USER`
- [x] Index copy audit and v2 blind evaluation meet the frozen baseline.
- [ ] Account signup, one-time 1,000-credit grant, key lifecycle and Playground pass.
- [ ] API, web and tunnel containers are healthy by commit-digest images.
- [ ] Backup restore, image rollback and tunnel rollback drills pass.
- [ ] English/Chinese, no-JavaScript, 404, canonical, hreflang, Lighthouse and axe pass.
- [ ] External readiness monitoring and status page are live. `WAITING_USER`
- [ ] Search Console, Bing Webmaster and IndexNow ownership are complete. `WAITING_USER`

## Paid launch

- [ ] Everything in the free launch gate passes.
- [ ] Pro and Scale sandbox products/prices exist. `WAITING_USER`
- [ ] Webhook, duplicate/out-of-order events, renewal, cancel, refund and chargeback pass.
- [ ] A complete sandbox checkout reaches credits through `transaction.completed`.
- [ ] Daily reconciliation and encrypted off-site backups have heartbeats.
- [ ] An empty-directory restore drill passes.
- [ ] Seller details and payment/legal text match the verified Paddle profile. `WAITING_USER`
- [ ] Paddle has explicitly accepted email-only buyer support, or a business phone is supplied. `WAITING_USER`
- [ ] Both live freeze flags are changed only after all gates above pass.

## Redirect matrix

Verify 301 once, then 200:

- `http://annolux.com/pricing` -> `https://annolux.com/pricing`
- `https://www.annolux.com/pricing` -> `https://annolux.com/pricing`
- `https://annolux.com/pricing.html` -> `https://annolux.com/pricing`
- `https://annolux.com/pricing?lang=zh` -> `https://annolux.com/zh/pricing`
- `https://annolux.com/pricing.html?lang=zh` -> `https://annolux.com/zh/pricing`
- `https://annolux.com/docs/index.html` -> `https://annolux.com/docs`
- `https://annolux.com/index.html` -> `https://annolux.com/`

Verify true 404 with no redirect for a random nonexistent path.
