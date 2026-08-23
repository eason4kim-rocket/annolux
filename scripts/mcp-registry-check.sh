#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

jq -e '
  .["$schema"] == "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json" and
  .name == "io.github.eason4kim-rocket/annolux" and
  .version == "0.1.2" and
  .repository == {
    "url": "https://github.com/eason4kim-rocket/annolux",
    "source": "github"
  } and
  (.packages | length) == 1 and
  .packages[0].registryType == "oci" and
  .packages[0].identifier == ("ghcr.io/eason4kim-rocket/annolux:mcp-" + .version) and
  .packages[0].transport == {"type": "stdio"} and
  ([.packages[0].environmentVariables[].name] | sort) == ["ANNOLUX_API_KEY", "ANNOLUX_API_URL"] and
  ([.packages[0].environmentVariables[] | select(.name == "ANNOLUX_API_KEY" and .isRequired == true and .isSecret == true)] | length) == 1
' server.json >/dev/null

grep -Fq 'io.modelcontextprotocol.server.name="io.github.eason4kim-rocket/annolux"' Dockerfile

echo 'MCP Registry metadata: pass'
