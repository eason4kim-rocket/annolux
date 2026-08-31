# annolux-mcp

Official [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server for **[Annolux](https://annolux.com)** Search API.

Bilingual (English & Chinese) curated web search for AI coding assistants and agents (Claude Code, Cursor, Windsurf, Claude Desktop).

## Quick Start

Requires Node.js 18+. Try the zero-config sandbox first—no account or API key is required:

### Claude Code

```bash
claude mcp add annolux -- npx -y annolux-mcp
```

### Cursor & Claude Desktop

Add to your MCP configuration (`claude_desktop_config.json` or Cursor Settings):

```json
{
  "mcpServers": {
    "annolux": {
      "command": "npx",
      "args": ["-y", "annolux-mcp"]
    }
  }
}
```

This configuration runs in the limited sandbox. For the full 1,000 credits/month, create a free key at [annolux.com](https://annolux.com) and add it to the server's environment:

```json
"env": {
  "ANNOLUX_API_KEY": "ann_live_YOUR_API_KEY"
}
```

## Features

- **Bilingual Curated Index**: 337,000+ vetted technical pages.
- **Explicit Provenance**: Every result includes URL, rank, and an exact `fetched_at` timestamp.
- **Deterministic Billing**: Exactly 1 credit per successful 2xx search.
- **Free Tier**: 1,000 free credits every month, no credit card, at [annolux.com](https://annolux.com).
- **Zero-Config Sandbox**: Run `npx -y annolux-mcp` with no API key to try it instantly (limited results); add a key through `ANNOLUX_API_KEY` for the full 1,000 credits/month.
- **Direct Upgrade Recovery**: When monthly credits run out, `search_web` returns the checkout link. After Paddle confirms payment, retry with the same API key—no MCP restart or key replacement is needed.

## License

Apache-2.0
