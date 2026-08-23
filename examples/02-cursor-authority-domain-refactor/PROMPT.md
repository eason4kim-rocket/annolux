# System Prompt & Cursor Rules: Zero-Hallucination Refactoring

Paste this into your Cursor `.cursorrules` or ask Cursor Agent directly:

---

```markdown
When refactoring codebase dependencies and updating API calls:

1. Always invoke `annolux` MCP tool (`search_web`) before writing new code.
2. Explicitly restrict the search domains to authoritative official documentation:
   - For React / Next.js: `"domains": ["react.dev", "nextjs.org"]`
   - For Go: `"domains": ["go.dev", "pkg.go.dev", "golang.org"]`
   - For Rust: `"domains": ["doc.rust-lang.org", "docs.rs"]`
   - For Python: `"domains": ["docs.python.org", "pypi.org"]`
3. Check the `fetched_at` timestamp on returned snippets to verify you are using current syntax, not deprecated legacy patterns.
4. Cite the official source URL in code comments where applicable.
```
