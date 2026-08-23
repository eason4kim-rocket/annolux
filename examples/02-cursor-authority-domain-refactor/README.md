# Example 02: Cursor & Windsurf Official Domain Refactoring

This example shows how to configure Cursor / Windsurf with Annolux MCP to eliminate deprecated API hallucinations during codebase refactoring.

---

## 🏃 Setup

1. Copy `.cursor/mcp.json` into your project's `.cursor/` directory.
2. Replace `ann_live_REPLACE_WITH_YOUR_KEY` with your actual API key from [annolux.com](https://annolux.com).
3. Open Cursor → Settings → MCP to verify `annolux` shows **Connected (1 Tool: search_web)**.

---

## 🎯 How to Use

In Cursor Composer / Agent mode (`Ctrl-I` / `Cmd-I`), try prompts like:

> *"Refactor our data-fetching layer to React 19 Server Actions. Use annolux search with domains restricted to react.dev to confirm current conventions."*

Cursor will invoke `search_web` with `"domains": ["react.dev"]`, receiving clean documentation snippets without third-party blog noise.
