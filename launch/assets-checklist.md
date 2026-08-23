# Launch assets and links

Do not add videos or large binary files to Git. Capture PNG or WebP screenshots
only after the production URL passes release checks; keep each approved asset
below 5 MiB.

## Required screenshots

- Desktop home hero plus all three independent price cards.
- Mobile home at 390 px with English as the default language.
- English and Chinese Search result examples showing `fetched_at`.
- Account page with IDs and raw keys redacted.
- Playground successful response with a non-sensitive query.
- Benchmarks page with the corpus limitations visible in-frame.
- OpenAPI operation and the one-tool MCP example.

For every screenshot, record viewport, capture date, deployed commit, image
digest, and whether any personal/account data was redacted. Do not fabricate a
dashboard state.

## Link checklist

- Website, Docs, Pricing, Benchmarks, Lab Notes, Changelog.
- GitHub source and release, GHCR package, OpenAPI JSON.
- Status, Contact, Security, Privacy, Terms, Refund, AUP.
- Google/Bing index verification and sitemap status are internal proof, not
  promotional links.

## UTM convention

Use only aggregate, link-level tags—no user identifiers:

`utm_source={show_hn|product_hunt|v2ex|juejin|zhihu|reddit}`
`utm_medium=community`
`utm_campaign=free_launch_2026`

The site must work identically after removing all UTM parameters. Do not use
tracking cookies, fingerprinting, hidden pixels, or email-open tracking.
