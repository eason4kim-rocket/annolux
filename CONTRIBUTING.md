# Contributing

Open an issue before a large change. Keep the public product boundary to
standard Search and the single `search_web` MCP tool.

Run before submitting:

```sh
make verify
```

Do not submit production data, credentials, server details, media archives or
files larger than 5 MiB. Do not change frozen ranking weights using the v2 blind
set. Security reports belong at `security@annolux.com`, not in public issues.
