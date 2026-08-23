# annolux-mcp

Official [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server for **[Annolux](https://annolux.com)** Search API.

Bilingual (English & Chinese) curated web search for AI coding assistants and agents (Claude Code, Cursor, Windsurf, Claude Desktop).

## Quick Start

### Claude Code

```bash
claude mcp add annolux npx -y annolux-mcp -- -key YOUR_ANNOLUX_API_KEY
```

### Cursor & Claude Desktop

Add to your MCP configuration (`claude_desktop_config.json` or Cursor Settings):

```json
{
  "mcpServers": {
    "annolux": {
      "command": "npx",
      "args": ["-y", "annolux-mcp", "-key", "YOUR_ANNOLUX_API_KEY"]
    }
  }
}
```

## Features

- **Bilingual Curated Index**: 337,000+ vetted technical pages.
- **Explicit Provenance**: Every result includes URL, rank, and an exact `fetched_at` timestamp.
- **Deterministic Billing**: Exactly 1 credit per successful 2xx search.
- **Free Tier**: 1,000 free permanent credits upon signup at [annolux.com](https://annolux.com).

## License

Apache-2.0
