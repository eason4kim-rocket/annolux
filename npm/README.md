# annolux-mcp

MCP server for [Annolux Search](https://annolux.com) — curated English and
Chinese web search for AI agents. Every result carries its source URL and an
explicit `fetched_at` timestamp.

This package downloads the platform binary from the pinned GitHub release on
first run and verifies it against checksums shipped inside this package.

## Usage

```sh
claude mcp add annolux --env ANNOLUX_API_KEY=ann_live_YOUR_KEY -- npx -y annolux-mcp
```

Get a free API key (1,000 credits, no card) at https://annolux.com.
Docs: https://annolux.com/mcp · Source: https://github.com/eason4kim-rocket/annolux (MIT client)
